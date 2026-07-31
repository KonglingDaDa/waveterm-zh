// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package jobmanager

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

const (
	CwndSize      = 64 * 1024       // 64 KB window for connected mode
	CirBufSize    = 2 * 1024 * 1024 // 2 MB max buffer size
	DisconnReadSz = 4 * 1024        // 4 KB read chunks when disconnected
	MaxPacketSize = 4 * 1024        // 4 KB max data per packet

	// StreamAckStallTimeout is how long outstanding data/terminal events may wait
	// for ACK progress before the transport is aborted and the substream rebuilt.
	// Chosen as 3x the default WSH RPC enqueue timeout (5s); full recovery SLA is 30s.
	StreamAckStallTimeout = 15 * time.Second
	// StreamAckCheckInterval is how often the watchdog evaluates ACK progress.
	StreamAckCheckInterval = 1 * time.Second
)

// DataSender delivers durable stream packets to the client transport.
// Send failures and Abort must not terminate the JobManager or PTY; they only
// tear down the current job-domain connection so the client can reconnect and
// resume from the persistent circular buffer.
type DataSender interface {
	SendData(dataPk wshrpc.CommandStreamData) error
	// Abort is idempotent; it closes only the current client transport.
	Abort(err error)
}

type streamTerminalEvent struct {
	isEof bool
	err   string
}

// StreamManager handles PTY output buffering with ACK-based flow control
type StreamManager struct {
	lock      sync.Mutex
	drainCond *sync.Cond

	streamId string

	// this is the data read from the attached reader
	buf           *CirBuf
	terminalEvent *streamTerminalEvent
	eofPos        int64 // fixed position when EOF/error occurs (-1 if not yet)

	reader io.Reader

	cwndSize int
	rwndSize int
	// invariant: if connected is true, dataSender is non-nil
	connected  bool
	dataSender DataSender

	// unacked state (reset on disconnect)
	sentNotAcked      int64
	terminalEventSent bool

	// track max acked to handle out-of-order ACKs (reset on disconnect)
	maxAckedSeq  int64
	maxAckedRwnd int64

	// terminal state - once true, stream is complete
	terminalEventAcked bool
	closed             bool

	// connectionEpoch increments on each ClientConnected so late failures from
	// a previous transport cannot abort a freshly established connection.
	connectionEpoch int64
	// lastAckProgressAt is the last time a legitimate ACK advanced seq or Fin.
	// Zero means no outstanding data/terminal is currently waiting for ACK.
	lastAckProgressAt time.Time
	// transportFailurePending ensures one Abort per failed epoch.
	transportFailurePending bool
	// lastSentSeq is the highest data seq scheduled for send (for logs/tests).
	lastSentSeq int64
	// nowFunc is injectable for deterministic stall tests; nil uses time.Now.
	nowFunc func() time.Time
	// stallTimeout overrides StreamAckStallTimeout when non-zero (tests).
	stallTimeout time.Duration
}

func MakeStreamManager() *StreamManager {
	return MakeStreamManagerWithSizes(CwndSize, CirBufSize)
}

func MakeStreamManagerWithSizes(cwndSize, cirbufSize int) *StreamManager {
	sm := &StreamManager{
		buf:      MakeCirBuf(cirbufSize, true),
		eofPos:   -1,
		cwndSize: cwndSize,
		rwndSize: cwndSize,
	}
	sm.drainCond = sync.NewCond(&sm.lock)
	go sm.senderLoop()
	go sm.watchdogLoop()
	return sm
}

func (sm *StreamManager) now() time.Time {
	if sm.nowFunc != nil {
		return sm.nowFunc()
	}
	return time.Now()
}

func (sm *StreamManager) getStallTimeout() time.Duration {
	if sm.stallTimeout > 0 {
		return sm.stallTimeout
	}
	return StreamAckStallTimeout
}

// SetClockForTest injects a clock for deterministic ACK-stall tests.
// Must be called before outstanding data is produced (typically right after construction).
func (sm *StreamManager) SetClockForTest(nowFunc func() time.Time, stallTimeout time.Duration) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	sm.nowFunc = nowFunc
	sm.stallTimeout = stallTimeout
}

// AttachReader starts reading from the given reader
func (sm *StreamManager) AttachReader(r io.Reader) error {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	if sm.reader != nil {
		return fmt.Errorf("reader already attached")
	}

	sm.reader = r
	go sm.readLoop()

	return nil
}

// connectionEpochBinder is implemented by production senders so the epoch is
// published atomically with dataSender under sm.lock (no post-return race).
// A non-nil error must leave the StreamManager disconnected (not published).
type connectionEpochBinder interface {
	bindConnectionEpoch(epoch int64) error
}

// ClientConnected transitions to CONNECTED mode.
// Returns (startSeq, connectionEpoch, error). The epoch is bound onto the
// sender (when it implements connectionEpochBinder) before the send loop is
// allowed to observe the new connection. If binding fails, dataSender is not
// published; connectionEpoch may still have been incremented.
func (sm *StreamManager) ClientConnected(streamId string, dataSender DataSender, rwndSize int, clientSeq int64) (int64, int64, error) {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	if sm.closed || sm.terminalEventAcked {
		return 0, 0, fmt.Errorf("stream is closed")
	}

	if sm.connected {
		return 0, 0, fmt.Errorf("client already connected")
	}

	if dataSender == nil {
		return 0, 0, fmt.Errorf("dataSender cannot be nil")
	}

	headPos := sm.buf.HeadPos()
	if clientSeq > headPos {
		bytesToConsume := int(clientSeq - headPos)
		available := sm.buf.Size()
		if bytesToConsume > available {
			return 0, 0, fmt.Errorf("client seq %d is beyond our stream end (head=%d, size=%d)", clientSeq, headPos, available)
		}
		if bytesToConsume > 0 {
			if err := sm.buf.Consume(bytesToConsume); err != nil {
				return 0, 0, fmt.Errorf("failed to consume buffer: %w", err)
			}
			headPos = sm.buf.HeadPos()
		}
	}

	sm.connectionEpoch++
	epoch := sm.connectionEpoch
	// Bind epoch before publishing dataSender so concurrent Abort/Send cannot
	// observe epoch=0 or a stale post-return assignment.
	if binder, ok := dataSender.(connectionEpochBinder); ok {
		if err := binder.bindConnectionEpoch(epoch); err != nil {
			// Do not publish dataSender/connected. Epoch stays incremented.
			return 0, epoch, err
		}
	}

	sm.streamId = streamId
	sm.dataSender = dataSender
	sm.connected = true
	sm.rwndSize = rwndSize
	sm.sentNotAcked = 0
	sm.maxAckedSeq = headPos
	sm.maxAckedRwnd = 0
	sm.lastAckProgressAt = time.Time{}
	sm.transportFailurePending = false
	sm.lastSentSeq = headPos
	effectiveWindow := sm.cwndSize
	if sm.rwndSize < effectiveWindow {
		effectiveWindow = sm.rwndSize
	}
	sm.buf.SetEffectiveWindow(true, effectiveWindow)
	sm.drainCond.Signal()

	startSeq := headPos
	if clientSeq > startSeq {
		startSeq = clientSeq
	}

	return startSeq, epoch, nil
}

// AllowTransportAbort reports whether Abort for the given epoch may close the
// transport. Only the current connection epoch may abort; a superseded epoch
// must not close a recycled MainServerConn.
func (sm *StreamManager) AllowTransportAbort(epoch int64) bool {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return epoch != 0 && sm.connectionEpoch == epoch
}

// GetStreamId returns the current stream ID (safe to call with lock held by caller)
func (sm *StreamManager) GetStreamId() string {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return sm.streamId
}

// GetConnectionEpoch returns the current connection epoch (for tests).
func (sm *StreamManager) GetConnectionEpoch() int64 {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return sm.connectionEpoch
}

// IsConnected reports whether a client transport is currently attached.
func (sm *StreamManager) IsConnected() bool {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return sm.connected
}

// IsTransportFailurePending reports whether the current epoch already scheduled Abort.
func (sm *StreamManager) IsTransportFailurePending() bool {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return sm.transportFailurePending
}

// GetStreamDoneInfo returns whether the stream is done and the error if there was one.
// The error is only meaningful if done=true, as the error is delivered as part of the stream otherwise.
func (sm *StreamManager) GetStreamDoneInfo() (done bool, streamError string) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	if !sm.terminalEventAcked {
		return false, ""
	}
	if sm.terminalEvent != nil && !sm.terminalEvent.isEof {
		return true, sm.terminalEvent.err
	}
	return true, ""
}

// ClientDisconnected transitions to DISCONNECTED mode
func (sm *StreamManager) ClientDisconnected() {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	sm.resetConnectionStateLocked()
}

func (sm *StreamManager) resetConnectionStateLocked() {
	if !sm.connected && sm.dataSender == nil {
		// Still clear watchdog bookkeeping when already disconnected.
		sm.lastAckProgressAt = time.Time{}
		sm.transportFailurePending = false
		return
	}

	sm.connected = false
	sm.dataSender = nil
	sm.sentNotAcked = 0
	sm.maxAckedSeq = 0
	sm.maxAckedRwnd = 0
	sm.lastAckProgressAt = time.Time{}
	// Keep transportFailurePending as-is so a second Abort for the same epoch is skipped;
	// ClientConnected resets it for the next epoch.
	if !sm.terminalEventAcked {
		sm.terminalEventSent = false
	}
	sm.buf.SetEffectiveWindow(false, CirBufSize)
	sm.drainCond.Signal()
}

// RecvAck processes an ACK from the client
// must be connected, and streamid must match
func (sm *StreamManager) RecvAck(ackPk wshrpc.CommandStreamAckData) {
	// Cancel may need Abort outside the lock.
	var abortSender DataSender
	var abortEpoch int64
	var abortReason error

	func() {
		sm.lock.Lock()
		defer sm.lock.Unlock()

		if !sm.connected || ackPk.Id != sm.streamId {
			return
		}

		if ackPk.Cancel {
			// Client cancelled this stream (reader closed / superseded). Tear down
			// the transport so we do not wait the full ACK stall timeout.
			if sm.transportFailurePending {
				return
			}
			sm.transportFailurePending = true
			abortSender = sm.dataSender
			abortEpoch = sm.connectionEpoch
			abortReason = fmt.Errorf("stream cancelled by client: streamId=%s epoch=%d", sm.streamId, abortEpoch)
			log.Printf("durable stream send failed: streamId=%s epoch=%d cause=cancel", sm.streamId, abortEpoch)
			sm.connected = false
			sm.dataSender = nil
			sm.sentNotAcked = 0
			sm.maxAckedSeq = 0
			sm.maxAckedRwnd = 0
			sm.lastAckProgressAt = time.Time{}
			if !sm.terminalEventAcked {
				sm.terminalEventSent = false
			}
			sm.buf.SetEffectiveWindow(false, CirBufSize)
			sm.drainCond.Signal()
			return
		}

		if ackPk.Fin {
			sm.terminalEventAcked = true
			sm.lastAckProgressAt = time.Time{}
			sm.drainCond.Signal()
			return
		}

		seq := ackPk.Seq
		rwnd := ackPk.RWnd
		headPos := sm.buf.HeadPos()
		// Upper bound: data actually scheduled for send (head + outstanding).
		sendUpper := headPos + sm.sentNotAcked

		// Validate range before updating maxAcked* so out-of-range ACKs cannot poison state.
		if seq < headPos || seq > sendUpper {
			return
		}

		// Ignore stale ACKs using tuple comparison (seq, rwnd)
		if seq < sm.maxAckedSeq || (seq == sm.maxAckedSeq && rwnd <= sm.maxAckedRwnd) {
			return
		}

		// Update max acked tuple only after range validation.
		prevMaxSeq := sm.maxAckedSeq
		sm.maxAckedSeq = seq
		sm.maxAckedRwnd = rwnd

		ackedBytes := seq - headPos
		if ackedBytes > sm.sentNotAcked {
			return
		}

		if ackedBytes > 0 {
			if err := sm.buf.Consume(int(ackedBytes)); err != nil {
				return
			}
			sm.sentNotAcked -= ackedBytes
		}

		// Only seq advance (or Fin above) counts as ACK progress for the watchdog.
		// Pure RWnd updates must not mask a missing data ACK.
		if seq > prevMaxSeq {
			if sm.hasOutstandingLocked() {
				sm.lastAckProgressAt = sm.now()
			} else {
				sm.lastAckProgressAt = time.Time{}
			}
		}

		prevRwnd := sm.rwndSize
		sm.rwndSize = int(ackPk.RWnd)
		effectiveWindow := sm.cwndSize
		if sm.rwndSize < effectiveWindow {
			effectiveWindow = sm.rwndSize
		}
		sm.buf.SetEffectiveWindow(true, effectiveWindow)

		if sm.rwndSize > prevRwnd || ackedBytes > 0 {
			sm.drainCond.Signal()
		}
	}()

	if abortSender != nil {
		log.Printf("durable stream transport aborted: epoch=%d cause=cancel", abortEpoch)
		abortSender.Abort(abortReason)
	}
}

func (sm *StreamManager) hasOutstandingLocked() bool {
	if sm.sentNotAcked > 0 {
		return true
	}
	return sm.terminalEventSent && !sm.terminalEventAcked
}

// markOutstandingLocked starts the ACK stall timer when the first unacked
// data or terminal packet is scheduled.
func (sm *StreamManager) markOutstandingLocked() {
	if sm.lastAckProgressAt.IsZero() {
		sm.lastAckProgressAt = sm.now()
	}
}

// CheckAckStall evaluates whether outstanding ACKs have stalled.
// Returns the sender to Abort (if any) and a reason string. Safe for tests.
// Abort is NOT invoked here; the caller must call Abort outside the StreamManager lock.
func (sm *StreamManager) CheckAckStall(now time.Time) (sender DataSender, epoch int64, stalled bool, reason string) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return sm.checkAckStallLocked(now)
}

func (sm *StreamManager) checkAckStallLocked(now time.Time) (sender DataSender, epoch int64, stalled bool, reason string) {
	if sm.closed || !sm.connected || sm.transportFailurePending {
		return nil, sm.connectionEpoch, false, ""
	}
	if !sm.hasOutstandingLocked() {
		return nil, sm.connectionEpoch, false, ""
	}
	if sm.lastAckProgressAt.IsZero() {
		return nil, sm.connectionEpoch, false, ""
	}
	timeout := sm.getStallTimeout()
	if now.Sub(sm.lastAckProgressAt) < timeout {
		return nil, sm.connectionEpoch, false, ""
	}
	// Arm failure under lock; Abort happens outside.
	sm.transportFailurePending = true
	sender = sm.dataSender
	epoch = sm.connectionEpoch
	outstanding := sm.sentNotAcked
	expectedAck := sm.buf.HeadPos() + sm.sentNotAcked
	duration := now.Sub(sm.lastAckProgressAt)
	reason = fmt.Sprintf("job/stream id=%s epoch=%d expectedAck=%d outstandingBytes=%d duration=%s",
		sm.streamId, epoch, expectedAck, outstanding, duration)
	// Freeze sending for this epoch immediately.
	sm.connected = false
	sm.dataSender = nil
	sm.sentNotAcked = 0
	sm.maxAckedSeq = 0
	sm.maxAckedRwnd = 0
	sm.lastAckProgressAt = time.Time{}
	if !sm.terminalEventAcked {
		sm.terminalEventSent = false
	}
	sm.buf.SetEffectiveWindow(false, CirBufSize)
	sm.drainCond.Signal()
	return sender, epoch, true, reason
}

// handleSendFailure records a send-path failure for the given epoch and returns
// the sender to Abort. If the epoch is stale or already failing, returns nil.
func (sm *StreamManager) handleSendFailure(epoch int64, err error) DataSender {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	if sm.closed || sm.connectionEpoch != epoch {
		// Stale epoch — do not touch the new connection.
		return nil
	}
	if sm.transportFailurePending {
		return nil
	}
	sm.transportFailurePending = true
	sender := sm.dataSender
	log.Printf("durable stream send failed: streamId=%s epoch=%d lastSentSeq=%d err=%v",
		sm.streamId, epoch, sm.lastSentSeq, err)
	// Stop sending on this epoch; recovery unit is the whole substream.
	sm.connected = false
	sm.dataSender = nil
	sm.sentNotAcked = 0
	sm.maxAckedSeq = 0
	sm.maxAckedRwnd = 0
	sm.lastAckProgressAt = time.Time{}
	if !sm.terminalEventAcked {
		sm.terminalEventSent = false
	}
	sm.buf.SetEffectiveWindow(false, CirBufSize)
	sm.drainCond.Signal()
	return sender
}

// SetRwndSize dynamically updates the receive window size
func (sm *StreamManager) SetRwndSize(rwndSize int) error {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	if rwndSize < 0 {
		return fmt.Errorf("rwndSize cannot be negative")
	}
	if !sm.connected {
		return fmt.Errorf("not connected")
	}
	sm.rwndSize = rwndSize
	effectiveWindow := sm.cwndSize
	if sm.rwndSize < effectiveWindow {
		effectiveWindow = sm.rwndSize
	}
	sm.buf.SetEffectiveWindow(true, effectiveWindow)
	sm.drainCond.Signal()
	return nil
}

// Close shuts down the sender loop. The reader loop will exit on its next iteration
// or when the underlying reader is closed.
func (sm *StreamManager) Close() {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	sm.closed = true
	sm.lastAckProgressAt = time.Time{}
	sm.drainCond.Signal()
}

// readLoop is the main read goroutine
func (sm *StreamManager) readLoop() {
	readBuf := make([]byte, MaxPacketSize)
	for {
		sm.lock.Lock()
		closed := sm.closed
		sm.lock.Unlock()

		if closed {
			return
		}

		n, err := sm.reader.Read(readBuf)

		if n > 0 {
			sm.handleReadData(readBuf[:n])
		}

		if err != nil {
			if err == io.EOF {
				sm.handleEOF()
			} else {
				sm.handleError(err)
			}
			return
		}
	}
}

func (sm *StreamManager) handleReadData(data []byte) {
	offset := 0
	for offset < len(data) {
		n, waitCh := sm.buf.WriteAvailable(data[offset:])
		offset += n

		if n > 0 {
			sm.lock.Lock()
			sm.drainCond.Signal()
			sm.lock.Unlock()
		}

		if waitCh != nil {
			<-waitCh
		}
	}
}

func (sm *StreamManager) handleEOF() {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	log.Printf("handleEOF: PTY reached EOF, totalSize=%d", sm.buf.TotalSize())
	sm.eofPos = sm.buf.TotalSize()
	sm.terminalEvent = &streamTerminalEvent{isEof: true}
	sm.drainCond.Signal()
}

func (sm *StreamManager) handleError(err error) {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	log.Printf("handleError: PTY error=%v, totalSize=%d", err, sm.buf.TotalSize())
	sm.eofPos = sm.buf.TotalSize()
	sm.terminalEvent = &streamTerminalEvent{err: err.Error()}
	sm.drainCond.Signal()
}

// sendDataSafely calls sender.SendData and converts panics into errors so the
// senderLoop can handleSendFailure/Abort and continue serving later reconnects.
func sendDataSafely(sender DataSender, pkt wshrpc.CommandStreamData) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("data sender panic: %v", r)
			log.Printf("durable stream sendDataSafely recovered panic: %v", r)
		}
	}()
	return sender.SendData(pkt)
}

func (sm *StreamManager) senderLoop() {
	// Per-iteration recover is in sendDataSafely so a panicking DataSender
	// becomes a send failure (Abort + continue) rather than killing the loop.
	for {
		done, pkt, sender, epoch := sm.prepareNextPacket()
		if done {
			return
		}
		if pkt == nil {
			continue
		}
		err := sendDataSafely(sender, *pkt)
		if err != nil {
			abortSender := sm.handleSendFailure(epoch, err)
			if abortSender != nil {
				log.Printf("durable stream transport aborted: streamId=%s epoch=%d cause=send-failed err=%v",
					pkt.Id, epoch, err)
				// Abort outside StreamManager lock (prepareNextPacket already released it;
				// handleSendFailure also released it).
				abortSender.Abort(err)
			}
		}
	}
}

func (sm *StreamManager) watchdogLoop() {
	ticker := time.NewTicker(StreamAckCheckInterval)
	defer ticker.Stop()
	for {
		<-ticker.C
		sm.lock.Lock()
		closed := sm.closed
		sm.lock.Unlock()
		if closed {
			return
		}
		// Use sm.now() so tests with injected clocks can drive stall detection
		// by advancing time and calling CheckAckStall directly; the ticker path
		// still works in production with wall clock.
		sender, epoch, stalled, reason := sm.CheckAckStall(sm.now())
		if stalled && sender != nil {
			log.Printf("durable stream ack stalled: %s", reason)
			log.Printf("durable stream transport aborted: epoch=%d cause=ack-stall", epoch)
			sender.Abort(fmt.Errorf("durable stream ack stalled: %s", reason))
		}
	}
}

func (sm *StreamManager) prepareNextPacket() (done bool, pkt *wshrpc.CommandStreamData, sender DataSender, epoch int64) {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	available := sm.buf.Size()

	if sm.closed || sm.terminalEventAcked {
		return true, nil, nil, sm.connectionEpoch
	}

	if !sm.connected || sm.transportFailurePending {
		sm.drainCond.Wait()
		return false, nil, nil, sm.connectionEpoch
	}

	if available == 0 {
		if sm.terminalEvent != nil && !sm.terminalEventSent {
			pkt := sm.prepareTerminalPacket()
			if pkt != nil {
				sm.markOutstandingLocked()
				return false, pkt, sm.dataSender, sm.connectionEpoch
			}
		}
		sm.drainCond.Wait()
		return false, nil, nil, sm.connectionEpoch
	}

	effectiveRwnd := sm.rwndSize
	if sm.cwndSize < effectiveRwnd {
		effectiveRwnd = sm.cwndSize
	}
	availableToSend := int64(effectiveRwnd) - sm.sentNotAcked

	if availableToSend <= 0 {
		sm.drainCond.Wait()
		return false, nil, nil, sm.connectionEpoch
	}

	peekSize := int(availableToSend)
	if peekSize > MaxPacketSize {
		peekSize = MaxPacketSize
	}
	if peekSize > available {
		peekSize = available
	}

	data := make([]byte, peekSize)
	n := sm.buf.PeekDataAt(int(sm.sentNotAcked), data)
	if n == 0 {
		sm.drainCond.Wait()
		return false, nil, nil, sm.connectionEpoch
	}
	data = data[:n]

	seq := sm.buf.HeadPos() + sm.sentNotAcked
	sm.sentNotAcked += int64(n)
	sm.lastSentSeq = seq + int64(n)
	sm.markOutstandingLocked()

	return false, &wshrpc.CommandStreamData{
		Id:     sm.streamId,
		Seq:    seq,
		Data64: base64.StdEncoding.EncodeToString(data),
	}, sm.dataSender, sm.connectionEpoch
}

func (sm *StreamManager) prepareTerminalPacket() *wshrpc.CommandStreamData {
	if sm.terminalEventSent || sm.terminalEvent == nil {
		return nil
	}

	pkt := &wshrpc.CommandStreamData{
		Id:  sm.streamId,
		Seq: sm.eofPos,
	}

	if sm.terminalEvent.isEof {
		pkt.Eof = true
	} else {
		pkt.Error = sm.terminalEvent.err
	}

	sm.terminalEventSent = true
	return pkt
}
