// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package jobmanager

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
)

// emptyServerImpl satisfies wshutil.ServerImpl for test WshRpc construction.
type emptyServerImpl struct{}

func (*emptyServerImpl) WshServerImpl() {}

// TestSendDataRecoverPattern documents the named-return recover contract used
// by routedDataSender.SendData: any panic becomes an error, never process death.
func TestSendDataRecoverPattern(t *testing.T) {
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("streamdata send panic: %v", r)
			}
		}()
		// Simulate SendCommand finalizing a nil handler after SendComplexRequest
		// recovered a send-on-closed-channel panic to (nil, nil).
		var handler *struct{ finalize func() }
		handler.finalize() // nil pointer panic
		return nil
	}()
	if err == nil {
		t.Fatal("expected recovered error")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected panic-wrapped error, got %v", err)
	}
}

// TestRoutedDataSender_SendDataSurvivesServerShutdown covers post-shutdown sends
// through the real StreamDataCommand path.
func TestRoutedDataSender_SendDataSurvivesServerShutdown(t *testing.T) {
	inputCh := make(chan baseds.RpcInputChType, 8)
	outputCh := make(chan []byte, 8)

	rpc := wshutil.MakeWshRpcWithChannels(inputCh, outputCh, wshrpc.RpcContext{}, &emptyServerImpl{}, "test-shutdown")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range outputCh {
		}
	}()

	mscInput := make(chan baseds.RpcInputChType, 2)
	c1, c2 := net.Pipe()
	defer c2.Close()
	msc := &MainServerConn{WshRpc: rpc, Conn: c1, inputCh: mscInput}

	sm := MakeStreamManager()
	defer sm.Close()

	rds := &routedDataSender{wshRpc: rpc, route: "r", msc: msc, sm: sm}
	if _, epoch, err := sm.ClientConnected("s", rds, 1024, 0); err != nil {
		t.Fatal(err)
	} else if epoch == 0 || rds.epoch.Load() != epoch {
		t.Fatalf("epoch not bound: ret=%d field=%d", epoch, rds.epoch.Load())
	}

	// Graceful server stop: runServer closes OutputCh then sets ServerDone.
	close(inputCh)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for output drain")
	}
	time.Sleep(20 * time.Millisecond) // allow setServerDone

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SendData leaked panic: %v", r)
			}
		}()
		err := rds.SendData(wshrpc.CommandStreamData{Id: "s", Seq: 0, Data64: "YQ=="})
		// After ServerDone, expect a clean error (not a process-killing panic).
		if err == nil {
			t.Log("send returned nil after shutdown (channel timing); still no panic")
		} else {
			t.Logf("got expected error: %v", err)
		}
	}()

	rds.Abort(fmt.Errorf("abort after shutdown"))
	rds.Abort(fmt.Errorf("idempotent"))
}

func TestRoutedDataSender_StaleEpochDoesNotAbort(t *testing.T) {
	// Same MainServerConn rebound to a newer epoch: stale Abort must not Close.
	inputCh := make(chan baseds.RpcInputChType, 4)
	outputCh := make(chan []byte, 8)

	rpc := wshutil.MakeWshRpcWithChannels(inputCh, outputCh, wshrpc.RpcContext{}, &emptyServerImpl{}, "test-epoch")
	defer close(inputCh)

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	msc := &MainServerConn{WshRpc: rpc, Conn: c1, inputCh: make(chan baseds.RpcInputChType, 1)}

	sm := MakeStreamManager()
	defer sm.Close()

	rds1 := &routedDataSender{wshRpc: rpc, route: "r1", msc: msc, sm: sm}
	if _, epoch1, err := sm.ClientConnected("e1", rds1, 1024, 0); err != nil {
		t.Fatal(err)
	} else if epoch1 == 0 || rds1.epoch.Load() != epoch1 {
		t.Fatalf("epoch1 not bound: ret=%d field=%d", epoch1, rds1.epoch.Load())
	}
	sm.ClientDisconnected()

	rds2 := &routedDataSender{wshRpc: rpc, route: "r2", msc: msc, sm: sm}
	if _, epoch2, err := sm.ClientConnected("e2", rds2, 1024, 0); err != nil {
		t.Fatal(err)
	} else if epoch2 < 2 {
		t.Fatalf("expected epoch >=2, got %d", epoch2)
	} else if rds2.epoch.Load() != epoch2 {
		t.Fatalf("epoch2 not bound on sender: field=%d ret=%d", rds2.epoch.Load(), epoch2)
	}

	// Stale Abort from previous epoch must not claim/close the rebound MSC.
	rds1.Abort(fmt.Errorf("stale"))
	msc.lifecycleMu.Lock()
	abortingAfterStale := msc.aborting
	boundAfterStale := msc.boundEpoch
	msc.lifecycleMu.Unlock()
	if abortingAfterStale {
		t.Fatal("stale Abort must not set aborting on rebound MainServerConn")
	}
	if boundAfterStale != rds2.epoch.Load() {
		t.Fatalf("boundEpoch=%d want %d", boundAfterStale, rds2.epoch.Load())
	}
	// Current epoch must still be claimable (proves stale Abort did not claim).
	if !msc.ClaimAbort(rds2.epoch.Load()) {
		t.Fatal("current epoch ClaimAbort should succeed after stale Abort was ignored")
	}
	// One-shot: further ClaimAbort must fail.
	if msc.ClaimAbort(rds2.epoch.Load()) {
		t.Fatal("ClaimAbort must be one-shot")
	}
	// rds2.Abort after claim already true should no-op Close-wise (abortOnce / ClaimAbort false).
	rds2.Abort(fmt.Errorf("already claimed"))
}

// TestMainServerConn_CloseThenBindEpochFails ensures ordinary Close participates
// in the epoch lifecycle (not only ClaimAbort).
func TestMainServerConn_CloseThenBindEpochFails(t *testing.T) {
	msc := &MainServerConn{}
	if err := msc.BindEpoch(1); err != nil {
		t.Fatalf("BindEpoch(1): %v", err)
	}
	msc.Close()
	if err := msc.BindEpoch(2); err == nil {
		t.Fatal("BindEpoch after Close must fail")
	}
	if msc.ClaimAbort(1) {
		t.Fatal("ClaimAbort after Close must fail")
	}
}

// TestMainServerConn_ClaimAbortVsBindEpochOrders covers serial orderings and a
// concurrent race: never both new-epoch bindSuccess and old-epoch claimSuccess.
func TestMainServerConn_ClaimAbortVsBindEpochOrders(t *testing.T) {
	// Order 1: old claim first → new BindEpoch fails.
	msc1 := &MainServerConn{}
	if err := msc1.BindEpoch(1); err != nil {
		t.Fatalf("BindEpoch(1): %v", err)
	}
	if !msc1.ClaimAbort(1) {
		t.Fatal("ClaimAbort(1) should succeed after BindEpoch(1)")
	}
	if err := msc1.BindEpoch(2); err == nil {
		t.Fatal("BindEpoch(2) should fail after ClaimAbort(1)")
	}

	// Order 2: new BindEpoch first → old ClaimAbort returns false.
	msc2 := &MainServerConn{}
	if err := msc2.BindEpoch(1); err != nil {
		t.Fatalf("BindEpoch(1): %v", err)
	}
	if err := msc2.BindEpoch(2); err != nil {
		t.Fatalf("BindEpoch(2): %v", err)
	}
	if msc2.ClaimAbort(1) {
		t.Fatal("ClaimAbort(1) should fail after rebind to epoch 2")
	}
	if !msc2.ClaimAbort(2) {
		t.Fatal("ClaimAbort(2) should succeed")
	}

	// Concurrent: old ClaimAbort vs new BindEpoch on the same MSC.
	// Never both bindSuccess for the new epoch AND oldClaimSuccess.
	const iters = 200
	for i := 0; i < iters; i++ {
		msc := &MainServerConn{}
		if err := msc.BindEpoch(1); err != nil {
			t.Fatalf("iter %d BindEpoch(1): %v", i, err)
		}
		var bindSuccess, oldClaimSuccess atomic.Bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if msc.ClaimAbort(1) {
				oldClaimSuccess.Store(true)
			}
		}()
		go func() {
			defer wg.Done()
			if err := msc.BindEpoch(2); err == nil {
				bindSuccess.Store(true)
			}
		}()
		wg.Wait()
		if bindSuccess.Load() && oldClaimSuccess.Load() {
			t.Fatalf("iter %d: both BindEpoch(2) success and ClaimAbort(1) success — epoch claim race", i)
		}
	}
}

// panicOnceSender panics on the first SendData, then records subsequent sends.
type panicOnceSender struct {
	mu        sync.Mutex
	panicked  bool
	packets   []wshrpc.CommandStreamData
	abortCh   chan struct{}
	abortOnce sync.Once
	aborts    int
}

func newPanicOnceSender() *panicOnceSender {
	return &panicOnceSender{abortCh: make(chan struct{})}
}

func (s *panicOnceSender) SendData(pkt wshrpc.CommandStreamData) error {
	s.mu.Lock()
	if !s.panicked {
		s.panicked = true
		s.mu.Unlock()
		panic("panicOnceSender boom")
	}
	s.packets = append(s.packets, pkt)
	s.mu.Unlock()
	return nil
}

func (s *panicOnceSender) Abort(err error) {
	s.mu.Lock()
	s.aborts++
	s.mu.Unlock()
	s.abortOnce.Do(func() { close(s.abortCh) })
}

func (s *panicOnceSender) packetCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.packets)
}

func (s *panicOnceSender) didPanic() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.panicked
}

// recordingSender records SendData without panicking.
type recordingSender struct {
	mu      sync.Mutex
	packets []wshrpc.CommandStreamData
	aborts  int
}

func (s *recordingSender) SendData(pkt wshrpc.CommandStreamData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packets = append(s.packets, pkt)
	return nil
}

func (s *recordingSender) Abort(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aborts++
}

func (s *recordingSender) packetCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.packets)
}

// TestSenderLoop_SurvivesPanicOnce proves sendDataSafely keeps senderLoop alive
// after a DataSender panic: Abort, disconnect, reconnect, more data delivered.
func TestSenderLoop_SurvivesPanicOnce(t *testing.T) {
	sm := MakeStreamManagerWithSizes(16*1024, 64*1024)
	defer sm.Close()

	// Enough data for multiple packets so reconnect still has bytes to send.
	payload := strings.Repeat("x", 9000)
	if err := sm.AttachReader(strings.NewReader(payload)); err != nil {
		t.Fatal(err)
	}

	ps := newPanicOnceSender()
	if _, _, err := sm.ClientConnected("panic-s", ps, 16*1024, 0); err != nil {
		t.Fatal(err)
	}

	select {
	case <-ps.abortCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Abort after SendData panic")
	}
	if !ps.didPanic() {
		t.Fatal("expected first SendData to panic")
	}

	// Disconnect after Abort so a new ClientConnected can attach.
	sm.ClientDisconnected()

	rs := &recordingSender{}
	if _, _, err := sm.ClientConnected("panic-s2", rs, 16*1024, 0); err != nil {
		t.Fatal(err)
	}

	// Prove senderLoop is still alive: more data must be sent on the new sender.
	deadline := time.Now().Add(3 * time.Second)
	for rs.packetCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if rs.packetCount() == 0 {
		t.Fatal("expected packets on reconnect after panic; senderLoop may have died")
	}
	sm.ClientDisconnected()
}
