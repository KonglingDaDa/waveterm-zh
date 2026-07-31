package streamclient

import (
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

type AckSender interface {
	SendAck(ackPk wshrpc.CommandStreamAckData)
}

type readerRetirer interface {
	retireReader(streamId string)
}

type Reader struct {
	lock         sync.Mutex
	cond         *sync.Cond
	id           string
	ackSender    AckSender
	readWindow   int64
	nextSeq      int64
	buffer       []byte
	eof          bool
	err          error
	closed       bool
	lastRwndSent int64
	oooPackets   []wshrpc.CommandStreamData // out-of-order packets awaiting delivery

	// ingressStopped: refuse new RecvData (no further ACKs for new bytes).
	// Existing buffer remains readable so WaveFS can drain ACKed data.
	ingressStopped bool
	// draining: after buffer is empty, Read returns io.EOF without Cancel.
	// Used by graceful quiesce before reconnect snapshot.
	draining bool
}

func NewReader(id string, readWindow int64, ackSender AckSender) *Reader {
	return NewReaderWithSeq(id, readWindow, 0, ackSender)
}

func NewReaderWithSeq(id string, readWindow int64, startSeq int64, ackSender AckSender) *Reader {
	r := &Reader{
		id:           id,
		readWindow:   readWindow,
		ackSender:    ackSender,
		nextSeq:      startSeq,
		lastRwndSent: readWindow,
	}
	r.cond = sync.NewCond(&r.lock)
	return r
}

func (r *Reader) RecvData(dataPk wshrpc.CommandStreamData) {
	r.lock.Lock()
	defer r.lock.Unlock()

	// During graceful quiesce we must not accept or ACK new ingress — those
	// bytes remain on the remote circular buffer for replay after reconnect.
	if r.closed || r.ingressStopped || r.eof || r.err != nil {
		return
	}

	if dataPk.Id != r.id {
		return
	}

	// error packets can be sent without a valid Seq, so check for errors before validating sequence
	if dataPk.Error != "" {
		r.err = fmt.Errorf("stream error: %s", dataPk.Error)
		r.cond.Broadcast()
		r.sendAckLocked(true, false, "")
		return
	}

	if dataPk.Seq < r.nextSeq {
		return
	}
	if dataPk.Seq > r.nextSeq {
		r.addOOOPacketLocked(dataPk)
		return
	}

	r.recvDataOrderedLocked(dataPk)
	r.processOOOPacketsLocked()
	r.cond.Broadcast()
	r.sendAckLocked(r.eof, false, "")
}

func (r *Reader) recvDataOrderedLocked(dataPk wshrpc.CommandStreamData) {
	if dataPk.Data64 != "" {
		data, err := base64.StdEncoding.DecodeString(dataPk.Data64)
		if err != nil {
			r.err = err
			r.sendAckLocked(false, true, "base64 decode error")
			return
		}
		r.buffer = append(r.buffer, data...)
		r.nextSeq += int64(len(data))
	}

	if dataPk.Eof {
		r.eof = true
	}
}

func (r *Reader) addOOOPacketLocked(dataPk wshrpc.CommandStreamData) {
	for _, pkt := range r.oooPackets {
		if pkt.Seq == dataPk.Seq {
			// this handles duplicates
			return
		}
	}
	r.oooPackets = append(r.oooPackets, dataPk)
}

func (r *Reader) processOOOPacketsLocked() {
	if len(r.oooPackets) == 0 {
		return
	}
	sort.Slice(r.oooPackets, func(i, j int) bool {
		return r.oooPackets[i].Seq < r.oooPackets[j].Seq
	})
	consumed := 0
	for _, pkt := range r.oooPackets {
		if r.eof || r.err != nil {
			// we're done, so we can clear any pending ooo packets
			r.oooPackets = nil
			return
		}
		if pkt.Seq != r.nextSeq {
			break
		}
		r.recvDataOrderedLocked(pkt)
		consumed++
	}
	r.oooPackets = r.oooPackets[consumed:]
}

func (r *Reader) sendAckLocked(fin bool, cancel bool, errStr string) {
	rwnd := r.readWindow - int64(len(r.buffer))
	if rwnd < 0 {
		rwnd = 0
	}
	ack := wshrpc.CommandStreamAckData{
		Id:     r.id,
		Seq:    r.nextSeq,
		Fin:    fin,
		Cancel: cancel,
		RWnd:   rwnd,
		Error:  errStr,
	}
	r.ackSender.SendAck(ack)
	r.lastRwndSent = rwnd
}

// StopIngress refuses new RecvData (and therefore new ACKs) while leaving the
// already-ACKed memory buffer available for Read. Does not send Cancel.
func (r *Reader) StopIngress() {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.ingressStopped = true
	// Drop undelivered OOO so we do not later deliver un-ACKed packets from a
	// half-open state; remote will resend after reconnect.
	r.oooPackets = nil
	r.cond.Broadcast()
}

// BeginDrain stops ingress and causes Read to return io.EOF once the buffer is empty.
// Used by graceful quiesce so the output loop can flush WaveFS before snapshot.
func (r *Reader) BeginDrain() {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.ingressStopped = true
	r.draining = true
	r.oooPackets = nil
	r.cond.Broadcast()
}

// IsDraining reports whether graceful drain was requested.
func (r *Reader) IsDraining() bool {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.draining
}

// BufferedLen returns the number of bytes currently held in the memory buffer.
func (r *Reader) BufferedLen() int {
	r.lock.Lock()
	defer r.lock.Unlock()
	return len(r.buffer)
}

func (r *Reader) Read(p []byte) (int, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	for len(r.buffer) == 0 && !r.eof && r.err == nil && !r.closed && !r.draining {
		r.cond.Wait()
	}

	// Hard Close always wins over drain/buffer. BeginDrain alone may flush
	// ACKed bytes; once Close is called, subsequent Read returns ErrClosedPipe
	// immediately (file-copy / WshFS cancel contract).
	if r.closed {
		return 0, io.ErrClosedPipe
	}

	// Graceful drain or normal: deliver remaining buffered bytes.
	if len(r.buffer) > 0 {
		n := copy(p, r.buffer)
		r.buffer = r.buffer[n:]

		// During drain/ingress-stop, do not send further window ACKs — reconnect
		// will reset flow control. Active mode still updates RWnd as before.
		if !r.draining && !r.ingressStopped {
			currentRwnd := r.readWindow - int64(len(r.buffer))
			if currentRwnd < 0 {
				currentRwnd = 0
			}
			threshold := r.readWindow / 5
			rwndDiff := currentRwnd - r.lastRwndSent
			if len(r.buffer) == 0 || rwndDiff >= threshold {
				r.sendAckLocked(false, false, "")
			}
		}
		return n, nil
	}

	if r.err != nil {
		return 0, r.err
	}

	if r.draining {
		// Graceful drain complete: buffer empty, no more ingress.
		return 0, io.EOF
	}

	if r.eof {
		return 0, io.EOF
	}

	return 0, nil
}

func (r *Reader) UpdateNextSeq(newSeq int64) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.nextSeq = newSeq
}

// ReleaseWithoutCancel unregisters a reader locally after graceful drain or a
// protocol terminal event. It must not be used to interrupt an active stream;
// Close is the hard-cancel operation and sends a Cancel ACK.
func (r *Reader) ReleaseWithoutCancel() error {
	r.lock.Lock()
	if !r.draining && !r.eof && r.err == nil && !r.closed {
		streamId := r.id
		r.lock.Unlock()
		return fmt.Errorf("cannot release active stream reader %s without cancel", streamId)
	}
	streamId := r.id
	retirer, canRetire := r.ackSender.(readerRetirer)
	r.lock.Unlock()

	if canRetire {
		retirer.retireReader(streamId)
	}
	return nil
}

func (r *Reader) Close() error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.closed {
		if r.err != nil {
			return r.err
		}
		return io.ErrClosedPipe
	}

	r.closed = true
	if r.err == nil {
		r.err = io.ErrClosedPipe
	}
	r.cond.Broadcast()
	r.sendAckLocked(false, true, "")

	return r.err
}
