// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package jobmanager

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/wavejwt"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshrpc/wshclient"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
)

type MainServerConn struct {
	PeerAuthenticated atomic.Bool
	SelfAuthenticated atomic.Bool
	WshRpc            *wshutil.WshRpc
	Conn              net.Conn
	inputCh           chan baseds.RpcInputChType
	closeOnce         sync.Once

	// lifecycleMu protects boundEpoch/aborting/closed so a stale Abort from a
	// previous connection epoch cannot close a MainServerConn that has already
	// been rebound, and so ordinary Close cannot leave BindEpoch open.
	// Lock order: lifecycle state → closeOnce body (never reverse).
	lifecycleMu sync.Mutex
	boundEpoch  int64
	aborting    bool
	closed      bool
}

// BindEpoch records the StreamManager connection epoch for this transport.
// Fails if Abort has claimed this connection or Close has already run.
func (msc *MainServerConn) BindEpoch(epoch int64) error {
	msc.lifecycleMu.Lock()
	defer msc.lifecycleMu.Unlock()
	if msc.aborting || msc.closed {
		return fmt.Errorf("MainServerConn is closed/aborting; cannot bind epoch %d", epoch)
	}
	msc.boundEpoch = epoch
	return nil
}

// ClaimAbort attempts to claim the right to Abort this connection for epoch.
// Returns true only once per connection lifecycle when epoch matches boundEpoch.
func (msc *MainServerConn) ClaimAbort(epoch int64) bool {
	msc.lifecycleMu.Lock()
	defer msc.lifecycleMu.Unlock()
	if msc.aborting || msc.closed || epoch == 0 || msc.boundEpoch != epoch {
		return false
	}
	msc.aborting = true
	return true
}

func (*MainServerConn) WshServerImpl() {}

func (msc *MainServerConn) Close() {
	// Mark closed under lifecycleMu first so concurrent BindEpoch fails before
	// socket/channel teardown. closeOnce still serializes the actual close.
	msc.lifecycleMu.Lock()
	msc.closed = true
	msc.lifecycleMu.Unlock()

	msc.closeOnce.Do(func() {
		if msc.Conn != nil {
			_ = msc.Conn.Close()
		}
		// inputCh may already be closed by peer teardown (e.g. adapt loop exit).
		// Never let a double-close panic kill JobManager.
		if msc.inputCh != nil {
			func() {
				defer func() { _ = recover() }()
				close(msc.inputCh)
			}()
		}
	})
}

type routedDataSender struct {
	wshRpc *wshutil.WshRpc
	route  string
	msc    *MainServerConn
	// epoch is bound under StreamManager.ClientConnected lock before the
	// sender is published to the send loop (see bindConnectionEpoch).
	epoch atomic.Int64
	sm    *StreamManager
	// abortOnce ensures at most one Close per sender instance.
	abortOnce sync.Once
}

// bindConnectionEpoch is invoked under StreamManager.lock while publishing
// this sender so the epoch is immutable for the lifetime of the connection.
// It claims the MSC lifecycle first; on failure ClientConnected must not
// publish dataSender/connected.
func (rds *routedDataSender) bindConnectionEpoch(epoch int64) error {
	if rds.msc != nil {
		if err := rds.msc.BindEpoch(epoch); err != nil {
			return err
		}
	}
	rds.epoch.Store(epoch)
	return nil
}

func (rds *routedDataSender) SendData(dataPk wshrpc.CommandStreamData) (err error) {
	// Named return + recover: WshRpc.SendCommand can panic when OutputCh is
	// closed (runServer close races NoResponse send: SendComplexRequest
	// recovers to (nil,nil) then SendCommand finalizes a nil handler).
	// A panic here would kill the entire JobManager process.
	// Kept as defense in depth alongside StreamManager.sendDataSafely.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("streamdata send panic: %v", r)
			log.Printf("SendData: recovered panic sending stream data: %v\n", r)
		}
	}()
	err = wshclient.StreamDataCommand(rds.wshRpc, dataPk, &wshrpc.RpcOpts{NoResponse: true, Route: rds.route})
	if err != nil {
		log.Printf("SendData: error sending stream data: %v\n", err)
		return err
	}
	return nil
}

// Abort closes the current main-server client transport only. It does not
// terminate the JobManager or PTY; the client is expected to reconnect and
// resume from the durable circular buffer.
// Epoch ownership is claimed on the MainServerConn (ClaimAbort); a stale
// epoch from a previous bind must not close a rebound connection.
func (rds *routedDataSender) Abort(err error) {
	if rds.msc == nil {
		return
	}
	epoch := rds.epoch.Load()
	if !rds.msc.ClaimAbort(epoch) {
		log.Printf("durable stream transport aborted: ignoring stale Abort epoch=%d err=%v\n", epoch, err)
		return
	}
	rds.abortOnce.Do(func() {
		log.Printf("durable stream transport aborted: closing MainServerConn epoch=%d (err=%v)\n", epoch, err)
		rds.msc.Close()
	})
}

func (msc *MainServerConn) authenticateSelfToServer(jobAuthToken string) error {
	jobId, _ := WshCmdJobManager.GetJobAuthInfo()
	authData := wshrpc.CommandAuthenticateJobManagerData{
		JobId:        jobId,
		JobAuthToken: jobAuthToken,
	}
	err := wshclient.AuthenticateJobManagerCommand(msc.WshRpc, authData, &wshrpc.RpcOpts{Route: wshutil.ControlRoute})
	if err != nil {
		log.Printf("authenticateSelfToServer: failed to authenticate to server: %v\n", err)
		return fmt.Errorf("failed to authenticate to server: %w", err)
	}
	msc.SelfAuthenticated.Store(true)
	log.Printf("authenticateSelfToServer: successfully authenticated to server\n")
	return nil
}

func (msc *MainServerConn) AuthenticateToJobManagerCommand(ctx context.Context, data wshrpc.CommandAuthenticateToJobData) error {
	jobId, jobAuthToken := WshCmdJobManager.GetJobAuthInfo()

	claims, err := wavejwt.ValidateAndExtract(data.JobAccessToken)
	if err != nil {
		log.Printf("AuthenticateToJobManager: failed to validate token: %v\n", err)
		return fmt.Errorf("failed to validate token: %w", err)
	}
	if !claims.MainServer {
		log.Printf("AuthenticateToJobManager: MainServer claim not set\n")
		return fmt.Errorf("MainServer claim not set")
	}
	if claims.JobId != jobId {
		log.Printf("AuthenticateToJobManager: JobId mismatch: expected %s, got %s\n", jobId, claims.JobId)
		return fmt.Errorf("JobId mismatch")
	}
	msc.PeerAuthenticated.Store(true)
	log.Printf("AuthenticateToJobManager: authentication successful for JobId=%s\n", claims.JobId)

	err = msc.authenticateSelfToServer(jobAuthToken)
	if err != nil {
		msc.PeerAuthenticated.Store(false)
		return err
	}

	WshCmdJobManager.SetAttachedClient(msc)
	return nil
}

func (msc *MainServerConn) StartJobCommand(ctx context.Context, data wshrpc.CommandStartJobData) (*wshrpc.CommandStartJobRtnData, error) {
	log.Printf("StartJobCommand: received command=%s args=%v", data.Cmd, data.Args)
	if !msc.PeerAuthenticated.Load() {
		log.Printf("StartJobCommand: not authenticated")
		return nil, fmt.Errorf("not authenticated")
	}
	return WshCmdJobManager.StartJob(msc, data)
}

func (msc *MainServerConn) JobPrepareConnectCommand(ctx context.Context, data wshrpc.CommandJobPrepareConnectData) (*wshrpc.CommandJobConnectRtnData, error) {
	if !msc.PeerAuthenticated.Load() {
		return nil, fmt.Errorf("peer not authenticated")
	}
	if !msc.SelfAuthenticated.Load() {
		return nil, fmt.Errorf("not authenticated to server")
	}
	return WshCmdJobManager.PrepareConnect(msc, data)
}

func (msc *MainServerConn) JobStartStreamCommand(ctx context.Context, data wshrpc.CommandJobStartStreamData) error {
	if !msc.PeerAuthenticated.Load() {
		return fmt.Errorf("not authenticated")
	}
	return WshCmdJobManager.StartStream(msc)
}

func (msc *MainServerConn) JobInputCommand(ctx context.Context, data wshrpc.CommandJobInputData) error {
	if !msc.PeerAuthenticated.Load() {
		return fmt.Errorf("not authenticated")
	}
	if !WshCmdJobManager.IsJobStarted() {
		return fmt.Errorf("job not started")
	}

	WshCmdJobManager.InputQueue.QueueItem(data.InputSessionId, data.SeqNum, data)
	return nil
}
