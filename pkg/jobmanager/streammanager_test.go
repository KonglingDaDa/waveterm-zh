// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package jobmanager

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

type testWriter struct {
	mu      sync.Mutex
	packets []wshrpc.CommandStreamData
	aborts  int
	lastErr error
}

func (tw *testWriter) SendData(pkt wshrpc.CommandStreamData) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.packets = append(tw.packets, pkt)
	return nil
}

func (tw *testWriter) Abort(err error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.aborts++
	tw.lastErr = err
}

func (tw *testWriter) GetPackets() []wshrpc.CommandStreamData {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	result := make([]wshrpc.CommandStreamData, len(tw.packets))
	copy(result, tw.packets)
	return result
}

func (tw *testWriter) AbortCount() int {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.aborts
}

func (tw *testWriter) Clear() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.packets = nil
}

// controllableSender injects SendData failures by packet index (0-based among
// successful-attempted data/terminal packets) and records Abort calls.
type controllableSender struct {
	mu sync.Mutex

	// failOnIndices: packet attempt numbers (0-based) that should return error.
	failOnIndices map[int]bool
	// failTerminal: if true, fail the first terminal (EOF/error) packet.
	failTerminal bool

	attempt      int
	packets      []wshrpc.CommandStreamData
	abortCount   int
	abortCh      chan struct{}
	abortOnce    sync.Once
	lastAbortErr error
	// after each successful send, optional hook
	onSend func(pkt wshrpc.CommandStreamData)
}

func newControllableSender() *controllableSender {
	return &controllableSender{
		failOnIndices: make(map[int]bool),
		abortCh:       make(chan struct{}),
	}
}

func (cs *controllableSender) SendData(pkt wshrpc.CommandStreamData) error {
	cs.mu.Lock()
	idx := cs.attempt
	cs.attempt++
	isTerminal := pkt.Eof || pkt.Error != ""
	shouldFail := cs.failOnIndices[idx] || (isTerminal && cs.failTerminal && idx >= 0)
	// failTerminal only once
	if isTerminal && cs.failTerminal {
		cs.failTerminal = false
		shouldFail = true
	}
	if !shouldFail {
		cs.packets = append(cs.packets, pkt)
	}
	hook := cs.onSend
	cs.mu.Unlock()

	if hook != nil && !shouldFail {
		hook(pkt)
	}
	if shouldFail {
		return fmt.Errorf("injected send failure at packet %d", idx)
	}
	return nil
}

func (cs *controllableSender) Abort(err error) {
	cs.mu.Lock()
	cs.abortCount++
	cs.lastAbortErr = err
	cs.mu.Unlock()
	cs.abortOnce.Do(func() {
		close(cs.abortCh)
	})
}

func (cs *controllableSender) waitAbort(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-cs.abortCh:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for Abort")
	}
}

func (cs *controllableSender) getAbortCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.abortCount
}

func (cs *controllableSender) getPackets() []wshrpc.CommandStreamData {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]wshrpc.CommandStreamData, len(cs.packets))
	copy(out, cs.packets)
	return out
}

func (cs *controllableSender) packetCount() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return len(cs.packets)
}

func decodeData(data64 string) string {
	decoded, _ := base64.StdEncoding.DecodeString(data64)
	return string(decoded)
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting: %s", msg)
}

func TestBasicDisconnectedMode(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("hello world")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) > 0 {
		t.Errorf("Expected no packets in DISCONNECTED mode without client, got %d", len(packets))
	}

	sm.Close()
}

func TestConnectedModeBasicFlow(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("hello")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, _, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected packets after ClientConnected")
	}

	// Verify we got the data
	allData := ""
	for _, pkt := range packets {
		if pkt.Data64 != "" {
			allData += decodeData(pkt.Data64)
		}
	}

	if allData != "hello" {
		t.Errorf("Expected 'hello', got '%s'", allData)
	}

	// Send ACK
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "1", Seq: 5, RWnd: CwndSize})

	time.Sleep(50 * time.Millisecond)

	// Check for EOF packet
	packets = tw.GetPackets()
	hasEof := false
	for _, pkt := range packets {
		if pkt.Eof {
			hasEof = true
		}
	}

	if !hasEof {
		t.Error("Expected EOF packet after ACKing all data")
	}

	sm.Close()
}

func TestDisconnectedToConnectedTransition(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("test data")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, _, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected cirbuf drain after connect")
	}

	allData := ""
	for _, pkt := range packets {
		if pkt.Data64 != "" {
			allData += decodeData(pkt.Data64)
		}
	}

	if allData != "test data" {
		t.Errorf("Expected 'test data', got '%s'", allData)
	}

	sm.Close()
}

func TestConnectedToDisconnectedTransition(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := &slowReader{data: []byte("slow data"), delay: 50 * time.Millisecond}
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, _, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	sm.ClientDisconnected()

	time.Sleep(100 * time.Millisecond)

	sm.Close()
}

func TestFlowControl(t *testing.T) {
	cwndSize := 1024
	tw := &testWriter{}
	sm := MakeStreamManagerWithSizes(cwndSize, 8*1024)

	largeData := strings.Repeat("x", cwndSize+500)
	reader := strings.NewReader(largeData)

	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, _, err = sm.ClientConnected("1", tw, cwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	totalData := 0
	for _, pkt := range packets {
		if pkt.Data64 != "" {
			decoded, _ := base64.StdEncoding.DecodeString(pkt.Data64)
			totalData += len(decoded)
		}
	}

	if totalData > cwndSize {
		t.Errorf("Sent %d bytes without ACK, exceeds cwnd size %d", totalData, cwndSize)
	}

	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "1", Seq: int64(totalData), RWnd: int64(cwndSize)})

	time.Sleep(100 * time.Millisecond)

	sm.Close()
}

func TestSequenceNumbering(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("abcdefghij")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, _, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected packets")
	}

	expectedSeq := int64(0)
	for _, pkt := range packets {
		if pkt.Data64 == "" {
			continue
		}

		if pkt.Seq != expectedSeq {
			t.Errorf("Expected seq %d, got %d", expectedSeq, pkt.Seq)
		}

		decoded, _ := base64.StdEncoding.DecodeString(pkt.Data64)
		expectedSeq += int64(len(decoded))
	}

	sm.Close()
}

func TestTerminalEventOrdering(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("data")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, _, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected data packets")
	}

	hasData := false
	hasEof := false
	eofSeq := int64(-1)

	for _, pkt := range packets {
		if pkt.Data64 != "" {
			hasData = true
		}
		if pkt.Eof {
			hasEof = true
			eofSeq = pkt.Seq
		}
	}

	if !hasData {
		t.Error("Expected data packet")
	}

	if hasEof {
		t.Error("Should not have EOF before ACK")
	}

	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "1", Seq: 4, RWnd: CwndSize})

	time.Sleep(50 * time.Millisecond)

	packets = tw.GetPackets()
	hasEof = false
	for _, pkt := range packets {
		if pkt.Eof {
			hasEof = true
			eofSeq = pkt.Seq
		}
	}

	if !hasEof {
		t.Error("Expected EOF after ACKing all data")
	}

	if eofSeq != 4 {
		t.Errorf("Expected EOF at seq 4, got %d", eofSeq)
	}

	sm.Close()
}

// --- #2985 recovery tests ---

func TestSendDataFailureAbortsTransport(t *testing.T) {
	// First packet fails → Abort once; transport disconnected; no further sends on old epoch.
	cs := newControllableSender()
	cs.failOnIndices[0] = true

	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	// Large payload so multiple packets would be sent if send kept going.
	payload := strings.Repeat("A", 3000)
	if err := sm.AttachReader(strings.NewReader(payload)); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	if _, _, err := sm.ClientConnected("s1", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}

	cs.waitAbort(t, 2*time.Second)

	if cs.getAbortCount() != 1 {
		t.Fatalf("expected exactly 1 Abort, got %d", cs.getAbortCount())
	}
	if sm.IsConnected() {
		t.Fatal("expected disconnected after send failure")
	}
	if !sm.IsTransportFailurePending() {
		t.Fatal("expected transportFailurePending")
	}
	// No successful packets on the failed epoch.
	if cs.packetCount() != 0 {
		t.Fatalf("expected 0 successful packets after first-packet failure, got %d", cs.packetCount())
	}

	sm.Close()
}

func TestSendDataMidStreamFailureAbortsOnce(t *testing.T) {
	// Fail the second packet; first succeeds; Abort once; no more packets after Abort.
	// Window must fit ≥2 MaxPacketSize chunks so the second packet is attempted without ACK.
	cs := newControllableSender()
	cs.failOnIndices[1] = true

	sm := MakeStreamManagerWithSizes(16*1024, 64*1024)
	payload := strings.Repeat("B", 9000) // > 2 * MaxPacketSize (4KiB)
	if err := sm.AttachReader(strings.NewReader(payload)); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	if _, _, err := sm.ClientConnected("s2", cs, 16*1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}

	cs.waitAbort(t, 2*time.Second)
	time.Sleep(50 * time.Millisecond) // allow any stray sends

	if cs.getAbortCount() != 1 {
		t.Fatalf("expected 1 Abort, got %d", cs.getAbortCount())
	}
	// Exactly one successful packet before the failure.
	if cs.packetCount() != 1 {
		t.Fatalf("expected 1 successful packet before mid-stream failure, got %d", cs.packetCount())
	}
	if sm.IsConnected() {
		t.Fatal("expected disconnected")
	}

	sm.Close()
}

func TestAckStallTriggersAbort(t *testing.T) {
	var fakeNow atomic.Int64
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fakeNow.Store(base.UnixNano())

	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	sm.SetClockForTest(func() time.Time {
		return time.Unix(0, fakeNow.Load())
	}, 15*time.Second)

	// Blocking reader: keep producing so we get outstanding data without EOF.
	pr, pw := io.Pipe()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	go func() {
		_, _ = pw.Write([]byte(strings.Repeat("C", 100)))
		// leave pipe open so no EOF
	}()

	if _, _, err := sm.ClientConnected("stall", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return cs.packetCount() > 0
	}, "at least one packet sent")

	// No ACK progress — advance fake clock past stall timeout.
	fakeNow.Store(base.Add(16 * time.Second).UnixNano())
	sender, epoch, stalled, reason := sm.CheckAckStall(time.Unix(0, fakeNow.Load()))
	if !stalled {
		t.Fatalf("expected stall, reason=%q packets=%d", reason, cs.packetCount())
	}
	if sender == nil {
		t.Fatal("expected sender to Abort")
	}
	if epoch == 0 {
		t.Fatal("expected non-zero epoch")
	}
	sender.Abort(errors.New(reason))

	if cs.getAbortCount() != 1 {
		t.Fatalf("expected 1 Abort, got %d", cs.getAbortCount())
	}
	if sm.IsConnected() {
		t.Fatal("expected disconnected after stall")
	}

	// Second CheckAckStall must not Abort again for same epoch.
	_, _, stalled2, _ := sm.CheckAckStall(time.Unix(0, fakeNow.Load()))
	if stalled2 {
		t.Fatal("second stall check should not re-arm Abort")
	}
	if cs.getAbortCount() != 1 {
		t.Fatalf("Abort count changed after second check: %d", cs.getAbortCount())
	}

	_ = pw.Close()
	sm.Close()
}

func TestAckProgressPreventsStall(t *testing.T) {
	var fakeNow atomic.Int64
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fakeNow.Store(base.UnixNano())

	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	sm.SetClockForTest(func() time.Time {
		return time.Unix(0, fakeNow.Load())
	}, 15*time.Second)

	pr, pw := io.Pipe()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	go func() {
		_, _ = pw.Write([]byte(strings.Repeat("D", 200)))
	}()

	if _, _, err := sm.ClientConnected("ok", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return cs.packetCount() > 0
	}, "packets")

	// Advance 10s and ACK all — progress should refresh timer.
	fakeNow.Store(base.Add(10 * time.Second).UnixNano())
	pkts := cs.getPackets()
	var endSeq int64
	for _, p := range pkts {
		if p.Data64 != "" {
			endSeq = p.Seq + int64(len(decodeData(p.Data64)))
		}
	}
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "ok", Seq: endSeq, RWnd: 1024})

	// Another 10s after ACK — still under 15s from last progress → no stall.
	fakeNow.Store(base.Add(20 * time.Second).UnixNano())
	// More data with ACK progress keeps us healthy; if no outstanding, also no stall.
	_, _, stalled, _ := sm.CheckAckStall(time.Unix(0, fakeNow.Load()))
	if stalled {
		t.Fatal("should not stall when ACKs advance / no outstanding")
	}
	if cs.getAbortCount() != 0 {
		t.Fatalf("unexpected Abort count %d", cs.getAbortCount())
	}

	_ = pw.Close()
	sm.Close()
}

func TestRwndOnlyAckDoesNotResetStallTimer(t *testing.T) {
	var fakeNow atomic.Int64
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fakeNow.Store(base.UnixNano())

	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	sm.SetClockForTest(func() time.Time {
		return time.Unix(0, fakeNow.Load())
	}, 15*time.Second)

	pr, pw := io.Pipe()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	go func() {
		_, _ = pw.Write([]byte(strings.Repeat("E", 100)))
	}()

	if _, _, err := sm.ClientConnected("rwnd", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return cs.packetCount() > 0 }, "packets")

	// Pure RWnd bump at seq=0 (no data acked) — if maxAcked starts 0, first ack with seq=0 rwnd change.
	// After data was sent, head is still 0 and sentNotAcked > 0. ACK seq=0 with higher rwnd is a window update only.
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "rwnd", Seq: 0, RWnd: 2048})

	fakeNow.Store(base.Add(16 * time.Second).UnixNano())
	sender, _, stalled, _ := sm.CheckAckStall(time.Unix(0, fakeNow.Load()))
	if !stalled {
		t.Fatal("RWnd-only ACK must not prevent stall when data remains unacked")
	}
	if sender != nil {
		sender.Abort(errors.New("stall"))
	}

	_ = pw.Close()
	sm.Close()
}

func TestRwndOnlyAckAtNonzeroHeadDoesNotResetStallTimer(t *testing.T) {
	var fakeNow atomic.Int64
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fakeNow.Store(base.UnixNano())

	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	defer sm.Close()
	sm.SetClockForTest(func() time.Time {
		return time.Unix(0, fakeNow.Load())
	}, 15*time.Second)

	// Model a reconnect after the first 100 bytes were already durably ACKed.
	if n, _ := sm.buf.WriteAvailable(make([]byte, 100)); n != 100 {
		t.Fatalf("seed write=%d want 100", n)
	}
	if err := sm.buf.Consume(100); err != nil {
		t.Fatalf("seed consume: %v", err)
	}

	pr, pw := io.Pipe()
	defer pw.Close()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	if startSeq, _, err := sm.ClientConnected("rwnd-head", cs, 1024, 100); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	} else if startSeq != 100 {
		t.Fatalf("startSeq=%d want 100", startSeq)
	}
	go func() {
		_, _ = pw.Write([]byte(strings.Repeat("H", 100)))
	}()
	waitFor(t, 2*time.Second, func() bool { return cs.packetCount() > 0 }, "packets")

	// This ACK only widens RWnd at the current head; it acknowledges no bytes.
	fakeNow.Store(base.Add(10 * time.Second).UnixNano())
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "rwnd-head", Seq: 100, RWnd: 2048})

	fakeNow.Store(base.Add(16 * time.Second).UnixNano())
	sender, _, stalled, _ := sm.CheckAckStall(time.Unix(0, fakeNow.Load()))
	if !stalled {
		t.Fatal("RWnd-only ACK at a nonzero head must not reset the stall timer")
	}
	if sender != nil {
		sender.Abort(errors.New("stall"))
	}
}

func TestTerminalPacketFinAckLossStillStalls(t *testing.T) {
	// Data fully ACKed, EOF sent, Fin never arrives → stall must still fire
	// (cannot only watch sentNotAcked > 0).
	var fakeNow atomic.Int64
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fakeNow.Store(base.UnixNano())

	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	sm.SetClockForTest(func() time.Time {
		return time.Unix(0, fakeNow.Load())
	}, 15*time.Second)

	if err := sm.AttachReader(strings.NewReader("fin")); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	if _, _, err := sm.ClientConnected("fin", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return cs.packetCount() >= 1
	}, "data packet")

	// ACK all data so EOF can be sent.
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "fin", Seq: 3, RWnd: 1024})

	waitFor(t, 2*time.Second, func() bool {
		for _, p := range cs.getPackets() {
			if p.Eof {
				return true
			}
		}
		return false
	}, "EOF packet")

	// No Fin ACK — advance past stall timeout.
	fakeNow.Store(base.Add(20 * time.Second).UnixNano())
	sender, _, stalled, reason := sm.CheckAckStall(time.Unix(0, fakeNow.Load()))
	if !stalled {
		t.Fatalf("expected stall on missing Fin ACK, reason=%q", reason)
	}
	if sender != nil {
		sender.Abort(errors.New(reason))
	}
	if cs.getAbortCount() != 1 {
		t.Fatalf("expected Abort, got %d", cs.getAbortCount())
	}

	sm.Close()
}

func TestOutOfRangeAckDoesNotPoisonMaxAcked(t *testing.T) {
	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)

	pr, pw := io.Pipe()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	go func() {
		_, _ = pw.Write([]byte("hello"))
	}()

	if _, _, err := sm.ClientConnected("oor", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return cs.packetCount() > 0 }, "packets")

	// Far-future ACK must be ignored.
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "oor", Seq: 99999, RWnd: 1024})

	// Legitimate ACK must still work.
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "oor", Seq: 5, RWnd: 1024})

	// After full ACK, window should reopen — write more and expect more packets.
	go func() {
		_, _ = pw.Write([]byte("world"))
	}()

	waitFor(t, 2*time.Second, func() bool {
		var all string
		for _, p := range cs.getPackets() {
			if p.Data64 != "" {
				all += decodeData(p.Data64)
			}
		}
		return strings.Contains(all, "world")
	}, "legitimate ACK still accepted after out-of-range")

	_ = pw.Close()
	sm.Close()
}

func TestStaleEpochFailureDoesNotAbortNewConnection(t *testing.T) {
	cs1 := newControllableSender()
	// Hold the first send so we can reconnect before it fails.
	release := make(chan struct{})
	var firstSendStarted sync.WaitGroup
	firstSendStarted.Add(1)

	cs1.onSend = nil
	// Use a custom sender that blocks then fails.
	bs := &blockingFailSender{
		release:          release,
		firstSendStarted: &firstSendStarted,
	}

	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	pr, pw := io.Pipe()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	go func() { _, _ = pw.Write([]byte(strings.Repeat("Z", 50))) }()

	if _, _, err := sm.ClientConnected("old", bs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}

	firstSendStarted.Wait()
	// Disconnect and connect a new sender before the old send returns.
	sm.ClientDisconnected()
	cs2 := newControllableSender()
	if _, _, err := sm.ClientConnected("new", cs2, 1024, 0); err != nil {
		t.Fatalf("ClientConnected new: %v", err)
	}
	newEpoch := sm.GetConnectionEpoch()

	// Unblock old send failure.
	close(release)
	time.Sleep(100 * time.Millisecond)

	if cs2.getAbortCount() != 0 {
		t.Fatalf("new connection was aborted by stale failure: aborts=%d", cs2.getAbortCount())
	}
	if bs.abortCount.Load() != 0 {
		// Old sender may receive Abort only if handleSendFailure still matched epoch —
		// after disconnect+new connect, epoch changed so Abort must not be called on new path.
		// Old sender Abort is also wrong if epoch check works; expect 0.
		t.Fatalf("stale sender should not be Abort'd after epoch change, got %d", bs.abortCount.Load())
	}
	if !sm.IsConnected() {
		t.Fatal("new connection should remain connected")
	}
	if sm.GetConnectionEpoch() != newEpoch {
		t.Fatal("epoch should be stable for new connection")
	}

	// New connection can still send.
	waitFor(t, 2*time.Second, func() bool {
		return cs2.packetCount() > 0
	}, "new connection packets")

	_ = pw.Close()
	sm.Close()
}

// blockingFailSender blocks in SendData until release, then returns error.
type blockingFailSender struct {
	release          chan struct{}
	firstSendStarted *sync.WaitGroup
	startedOnce      sync.Once
	abortCount       atomic.Int32
}

func (b *blockingFailSender) SendData(pkt wshrpc.CommandStreamData) error {
	b.startedOnce.Do(func() {
		b.firstSendStarted.Done()
	})
	<-b.release
	return errors.New("stale send failure")
}

func (b *blockingFailSender) Abort(err error) {
	b.abortCount.Add(1)
}

func TestCancelAckAbortsTransport(t *testing.T) {
	cs := newControllableSender()
	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	pr, pw := io.Pipe()
	if err := sm.AttachReader(pr); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	go func() { _, _ = pw.Write([]byte(strings.Repeat("X", 100))) }()

	if _, _, err := sm.ClientConnected("cancel", cs, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return cs.packetCount() > 0 }, "packets")

	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "cancel", Cancel: true})

	waitFor(t, 2*time.Second, func() bool {
		return cs.getAbortCount() >= 1
	}, "Abort after Cancel")
	if sm.IsConnected() {
		t.Fatal("expected disconnected after Cancel ACK")
	}

	_ = pw.Close()
	sm.Close()
}

func TestReconnectAfterSendFailureResumes(t *testing.T) {
	// After Abort disconnect, a new ClientConnected must be able to resend
	// buffered data (recovery unit is the substream).
	cs1 := newControllableSender()
	cs1.failOnIndices[0] = true

	sm := MakeStreamManagerWithSizes(1024, 8*1024)
	payload := "resume-me"
	if err := sm.AttachReader(strings.NewReader(payload)); err != nil {
		t.Fatalf("AttachReader: %v", err)
	}
	if _, _, err := sm.ClientConnected("e1", cs1, 1024, 0); err != nil {
		t.Fatalf("ClientConnected: %v", err)
	}
	cs1.waitAbort(t, 2*time.Second)

	// Simulate client reconnect with a healthy sender at seq 0.
	cs2 := newControllableSender()
	// ClientDisconnected is effectively already done by handleSendFailure;
	// ClientConnected requires !connected.
	if sm.IsConnected() {
		sm.ClientDisconnected()
	}
	if _, _, err := sm.ClientConnected("e2", cs2, 1024, 0); err != nil {
		t.Fatalf("reconnect ClientConnected: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		var all string
		for _, p := range cs2.getPackets() {
			if p.Data64 != "" {
				all += decodeData(p.Data64)
			}
		}
		return all == payload
	}, "resumed payload on new stream")

	sm.Close()
}

type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (sr *slowReader) Read(p []byte) (n int, err error) {
	if sr.pos >= len(sr.data) {
		return 0, io.EOF
	}

	time.Sleep(sr.delay)

	n = copy(p, sr.data[sr.pos:])
	sr.pos += n

	return n, nil
}
