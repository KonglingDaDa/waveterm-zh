// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package streamclient

import (
	"encoding/base64"
	"io"
	"sync"
	"testing"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

type recordingAckSender struct {
	mu   sync.Mutex
	acks []wshrpc.CommandStreamAckData
}

func (s *recordingAckSender) SendAck(ack wshrpc.CommandStreamAckData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks = append(s.acks, ack)
}

func (s *recordingAckSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.acks)
}

func (s *recordingAckSender) lastSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.acks) == 0 {
		return -1
	}
	return s.acks[len(s.acks)-1].Seq
}

func TestReaderGracefulDrainPreservesAckedBuffer(t *testing.T) {
	acks := &recordingAckSender{}
	r := NewReader("s1", 64*1024, acks)

	payload := []byte("hello-world-data")
	r.RecvData(wshrpc.CommandStreamData{
		Id:     "s1",
		Seq:    0,
		Data64: base64.StdEncoding.EncodeToString(payload),
	})
	if acks.count() == 0 {
		t.Fatal("expected ACK after RecvData")
	}
	if acks.lastSeq() != int64(len(payload)) {
		t.Fatalf("expected ACK seq %d, got %d", len(payload), acks.lastSeq())
	}
	acksBeforeDrain := acks.count()

	// Graceful quiesce: stop ingress, drain buffer.
	r.BeginDrain()

	// New ingress must be ignored (no additional ACK).
	r.RecvData(wshrpc.CommandStreamData{
		Id:     "s1",
		Seq:    int64(len(payload)),
		Data64: base64.StdEncoding.EncodeToString([]byte("MORE")),
	})
	if acks.count() != acksBeforeDrain {
		t.Fatalf("expected no new ACKs after StopIngress, got %d vs %d", acks.count(), acksBeforeDrain)
	}

	var got []byte
	buf := make([]byte, 4)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
	}
	if string(got) != string(payload) {
		t.Fatalf("drained data mismatch: got %q want %q", got, payload)
	}
	if r.BufferedLen() != 0 {
		t.Fatalf("expected empty buffer after drain, got %d", r.BufferedLen())
	}
}

func TestReaderHardCloseReturnsErrClosedPipeImmediately(t *testing.T) {
	acks := &recordingAckSender{}
	r := NewReader("s2", 64*1024, acks)
	payload := []byte("persist-me")
	r.RecvData(wshrpc.CommandStreamData{
		Id:     "s2",
		Seq:    0,
		Data64: base64.StdEncoding.EncodeToString(payload),
	})

	// Hard Close without BeginDrain (timeout / supersede): drop buffer, fail immediately.
	_ = r.Close()

	buf := make([]byte, 64)
	n, err := r.Read(buf)
	if n != 0 || !errorsIsClosed(err) {
		t.Fatalf("expected n=0 ErrClosedPipe on hard Close, got n=%d err=%v", n, err)
	}
	// Buffer is intentionally not drained on hard Close.
	if r.BufferedLen() != len(payload) {
		t.Fatalf("expected buffer retained after hard Close for diagnostics, got %d", r.BufferedLen())
	}
}

func errorsIsClosed(err error) bool {
	return err == io.ErrClosedPipe
}

func TestReaderBeginDrainThenCloseIsHardCancel(t *testing.T) {
	acks := &recordingAckSender{}
	r := NewReader("s4", 1024, acks)
	payload := []byte("buffered")
	r.RecvData(wshrpc.CommandStreamData{
		Id:     "s4",
		Seq:    0,
		Data64: base64.StdEncoding.EncodeToString(payload),
	})
	r.BeginDrain()
	// Close after BeginDrain must still hard-cancel: no buffer emit.
	_ = r.Close()
	buf := make([]byte, 64)
	n, err := r.Read(buf)
	if n != 0 || !errorsIsClosed(err) {
		t.Fatalf("BeginDrain+Close must return ErrClosedPipe immediately, got n=%d err=%v", n, err)
	}
}

func TestReaderReleaseWithoutCancelRejectsActiveReader(t *testing.T) {
	acks := &recordingAckSender{}
	r := NewReader("active-release", 1024, acks)

	if err := r.ReleaseWithoutCancel(); err == nil {
		t.Fatal("active reader must not be released without Cancel")
	}
	if acks.count() != 0 {
		t.Fatalf("rejected release sent %d ACK(s)", acks.count())
	}
}

func TestReaderDrainThenEOFDoesNotSendCancelUntilClose(t *testing.T) {
	acks := &recordingAckSender{}
	r := NewReader("s3", 1024, acks)
	r.RecvData(wshrpc.CommandStreamData{
		Id:     "s3",
		Seq:    0,
		Data64: base64.StdEncoding.EncodeToString([]byte("x")),
	})
	r.BeginDrain()
	buf := make([]byte, 8)
	_, _ = r.Read(buf)
	_, err := r.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected EOF on drain complete, got %v", err)
	}
	// Cancel only on Close.
	for _, a := range acks.acks {
		if a.Cancel {
			t.Fatal("BeginDrain/Read must not send Cancel ACK")
		}
	}
	if err := r.ReleaseWithoutCancel(); err != nil {
		t.Fatalf("release drained reader: %v", err)
	}
	for _, a := range acks.acks {
		if a.Cancel {
			t.Fatal("ReleaseWithoutCancel must not send Cancel ACK")
		}
	}
	_ = r.Close()
	foundCancel := false
	for _, a := range acks.acks {
		if a.Cancel {
			foundCancel = true
		}
	}
	if !foundCancel {
		t.Fatal("expected Cancel on Close")
	}
}
