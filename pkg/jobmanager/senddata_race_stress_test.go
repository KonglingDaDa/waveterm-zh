// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

//go:build !race

package jobmanager

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
)

// Concurrent OutputCh-close stress. Excluded under -race because WshRpc's
// own teardown races are pre-existing; this file proves process survival.
func TestRoutedDataSender_ConcurrentClosedChannelStress(t *testing.T) {
	inputCh := make(chan baseds.RpcInputChType, 64)
	outputCh := make(chan []byte, 1)

	rpc := wshutil.MakeWshRpcWithChannels(inputCh, outputCh, wshrpc.RpcContext{}, &emptyServerImpl{}, "stress")

	mscInput := make(chan baseds.RpcInputChType, 4)
	c1, c2 := net.Pipe()
	defer c2.Close()
	msc := &MainServerConn{WshRpc: rpc, Conn: c1, inputCh: mscInput}

	sm := MakeStreamManager()
	defer sm.Close()

	rds := &routedDataSender{wshRpc: rpc, route: "r", msc: msc, sm: sm}
	if _, _, err := sm.ClientConnected("s", rds, 1024, 0); err != nil {
		t.Fatal(err)
	}

	var panics atomic.Int32
	var errors atomic.Int32
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							panics.Add(1)
						}
					}()
					if err := rds.SendData(wshrpc.CommandStreamData{Id: "s", Seq: int64(i), Data64: "YQ=="}); err != nil {
						errors.Add(1)
					}
				}()
			}
		}(i)
	}

	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-outputCh:
				if !ok {
					return
				}
			}
		}
	}()

	time.Sleep(20 * time.Millisecond)
	close(inputCh)
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	if panics.Load() != 0 {
		t.Fatalf("leaked panics: %d", panics.Load())
	}
	t.Logf("errors=%d", errors.Load())
}
