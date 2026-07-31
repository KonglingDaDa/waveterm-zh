package streamclient

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

type mockRpcInterface struct {
	dataChan chan wshrpc.CommandStreamData
	ackChan  chan wshrpc.CommandStreamAckData
}

func (m *mockRpcInterface) StreamDataCommand(data wshrpc.CommandStreamData, opts *wshrpc.RpcOpts) error {
	m.dataChan <- data
	return nil
}

func (m *mockRpcInterface) StreamDataAckCommand(data wshrpc.CommandStreamAckData, opts *wshrpc.RpcOpts) error {
	m.ackChan <- data
	return nil
}

func setupBrokerPair() (*Broker, *Broker) {
	rpc1 := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 10),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 10),
	}
	rpc2 := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 10),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 10),
	}

	broker1 := NewBroker(rpc1)
	broker2 := NewBroker(rpc2)

	go func() {
		for data := range rpc1.dataChan {
			broker2.RecvData(data)
		}
	}()

	go func() {
		for ack := range rpc1.ackChan {
			broker2.RecvAck(ack)
		}
	}()

	go func() {
		for data := range rpc2.dataChan {
			broker1.RecvData(data)
		}
	}()

	go func() {
		for ack := range rpc2.ackChan {
			broker1.RecvAck(ack)
		}
	}()

	return broker1, broker2
}

func TestBrokerBasicReadWrite(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	reader, meta := broker1.CreateStreamReader("reader1", "writer1", 1024)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	testData := []byte("Hello, World!")
	n, err := writer.Write(testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Write returned %d, expected %d", n, len(testData))
	}

	buf := make([]byte, 1024)
	n, err = reader.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Read returned %d, expected %d", n, len(testData))
	}
	if !bytes.Equal(buf[:n], testData) {
		t.Fatalf("Read data %q doesn't match written data %q", buf[:n], testData)
	}

	writer.Close()
	_, err = reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("Expected EOF, got %v", err)
	}
}

func TestBrokerEOF(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	reader, meta := broker1.CreateStreamReader("reader1", "writer1", 1024)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	testData := []byte("Test data")
	writer.Write(testData)
	writer.Close()

	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("First read failed: %v", err)
	}
	if !bytes.Equal(buf[:n], testData) {
		t.Fatalf("Read data doesn't match")
	}

	_, err = reader.Read(buf)
	if err != io.EOF {
		t.Fatalf("Expected EOF, got %v", err)
	}
}

func TestBrokerFlowControl(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	smallWindow := int64(10)
	reader, meta := broker1.CreateStreamReader("reader1", "writer1", smallWindow)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	largeData := make([]byte, 100)
	for i := range largeData {
		largeData[i] = byte(i)
	}

	writeDone := make(chan error)
	go func() {
		_, err := writer.Write(largeData)
		writeDone <- err
	}()

	received := make([]byte, 0, 100)
	buf := make([]byte, 20)
	for len(received) < len(largeData) {
		n, err := reader.Read(buf)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		received = append(received, buf[:n]...)
	}

	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write didn't complete in time")
	}

	if !bytes.Equal(received, largeData) {
		t.Fatal("Received data doesn't match sent data")
	}

	writer.Close()
}

func TestBrokerError(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	reader, meta := broker1.CreateStreamReader("reader1", "writer1", 1024)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	testErr := io.ErrUnexpectedEOF
	writer.CloseWithError(testErr)

	buf := make([]byte, 1024)
	_, err = reader.Read(buf)
	if err == nil {
		t.Fatal("Expected error from read")
	}
	if err.Error() != "stream error: unexpected EOF" {
		t.Fatalf("Expected stream error, got: %v", err)
	}
}

func TestBrokerCancel(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	reader, meta := broker1.CreateStreamReader("reader1", "writer1", 1024)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	reader.Close()

	select {
	case <-writer.GetCanceledChan():
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Writer not notified of cancellation")
	}

	_, _, canceled := writer.GetAckState()
	if !canceled {
		t.Fatal("Writer should be in canceled state")
	}
}

func TestBrokerReleaseReaderWithoutCancel(t *testing.T) {
	rpc := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 1),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 1),
	}
	broker := NewBroker(rpc)
	defer broker.Close()

	reader, meta := broker.CreateStreamReader("reader", "writer", 1024)
	reader.BeginDrain()
	if err := reader.ReleaseWithoutCancel(); err != nil {
		t.Fatalf("release drained reader: %v", err)
	}

	broker.lock.Lock()
	_, readerExists := broker.readers[meta.Id]
	broker.lock.Unlock()
	if readerExists {
		t.Fatal("released reader remained registered in broker")
	}

	// A packet may already be queued when graceful drain completes. It belongs
	// to the retired stream and must be dropped rather than converted into the
	// missing-reader Cancel that aborts the job transport.
	broker.processRecvData(wshrpc.CommandStreamData{Id: meta.Id, Seq: 0})
	broker.sendQueue.Close(false)
	broker.sendQueue.Wait()
	select {
	case ack := <-rpc.ackChan:
		t.Fatalf("late retired-stream packet unexpectedly sent ACK: %+v", ack)
	default:
	}
}

func TestBrokerFinRetiresReaderAndDropsLateData(t *testing.T) {
	rpc := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 1),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 2),
	}
	broker := NewBroker(rpc)
	defer broker.Close()

	_, meta := broker.CreateStreamReader("reader", "writer", 1024)
	broker.processSendAck(wshrpc.CommandStreamAckData{Id: meta.Id, Fin: true})

	select {
	case ack := <-rpc.ackChan:
		if !ack.Fin || ack.Cancel {
			t.Fatalf("unexpected terminal ACK: %+v", ack)
		}
	default:
		t.Fatal("Fin ACK was not sent to the writer")
	}

	broker.lock.Lock()
	_, readerExists := broker.readers[meta.Id]
	_, retiredExists := broker.retiredReaders[meta.Id]
	broker.lock.Unlock()
	if readerExists || !retiredExists {
		t.Fatalf("Fin must retire reader: reader=%v retired=%v", readerExists, retiredExists)
	}

	broker.processRecvData(wshrpc.CommandStreamData{Id: meta.Id, Seq: 0})
	broker.sendQueue.Close(false)
	broker.sendQueue.Wait()
	select {
	case ack := <-rpc.ackChan:
		t.Fatalf("late data after Fin unexpectedly sent ACK: %+v", ack)
	default:
	}
}

func TestBrokerPrunesExpiredRetiredReaderOnCreate(t *testing.T) {
	rpc := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 1),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 1),
	}
	broker := NewBroker(rpc)
	defer broker.Close()

	reader, oldMeta := broker.CreateStreamReader("reader", "writer", 1024)
	reader.BeginDrain()
	if err := reader.ReleaseWithoutCancel(); err != nil {
		t.Fatalf("release drained reader: %v", err)
	}
	broker.lock.Lock()
	broker.retiredReaders[oldMeta.Id] = time.Now().Add(-retiredReaderTTL - time.Second)
	broker.lock.Unlock()

	_, _ = broker.CreateStreamReader("reader", "writer", 1024)
	broker.lock.Lock()
	_, retiredExists := broker.retiredReaders[oldMeta.Id]
	_, routeExists := broker.writerRoutes[oldMeta.Id]
	broker.lock.Unlock()
	if retiredExists || routeExists {
		t.Fatalf("expired retired reader was not pruned: retired=%v writerRoute=%v", retiredExists, routeExists)
	}
}

func TestBrokerExpireRetiredReaderDoesNotDeleteNewerRetirement(t *testing.T) {
	rpc := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 1),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 1),
	}
	broker := NewBroker(rpc)
	defer broker.Close()

	reader, meta := broker.CreateStreamReader("reader", "writer", 1024)
	reader.BeginDrain()
	if err := reader.ReleaseWithoutCancel(); err != nil {
		t.Fatalf("release drained reader: %v", err)
	}
	broker.lock.Lock()
	firstRetirement := broker.retiredReaders[meta.Id]
	newerRetirement := firstRetirement.Add(time.Nanosecond)
	broker.retiredReaders[meta.Id] = newerRetirement
	broker.lock.Unlock()

	broker.expireRetiredReader(meta.Id, firstRetirement)
	broker.lock.Lock()
	gotRetirement, retiredExists := broker.retiredReaders[meta.Id]
	_, routeExists := broker.writerRoutes[meta.Id]
	broker.lock.Unlock()
	if !retiredExists || gotRetirement != newerRetirement || !routeExists {
		t.Fatalf("stale expiry deleted newer retirement: retired=%v timestamp=%v route=%v", retiredExists, gotRetirement, routeExists)
	}

	broker.expireRetiredReader(meta.Id, newerRetirement)
	broker.lock.Lock()
	_, retiredExists = broker.retiredReaders[meta.Id]
	_, routeExists = broker.writerRoutes[meta.Id]
	broker.lock.Unlock()
	if retiredExists || routeExists {
		t.Fatalf("matching expiry did not clean retirement: retired=%v route=%v", retiredExists, routeExists)
	}
}

func TestBrokerHardCloseAfterReleaseStillSendsCancel(t *testing.T) {
	rpc := &mockRpcInterface{
		dataChan: make(chan wshrpc.CommandStreamData, 1),
		ackChan:  make(chan wshrpc.CommandStreamAckData, 1),
	}
	broker := NewBroker(rpc)
	defer broker.Close()

	reader, _ := broker.CreateStreamReader("reader", "writer", 1024)
	reader.BeginDrain()
	if err := reader.ReleaseWithoutCancel(); err != nil {
		t.Fatalf("release drained reader: %v", err)
	}
	_ = reader.Close()
	broker.sendQueue.Close(false)
	broker.sendQueue.Wait()

	select {
	case ack := <-rpc.ackChan:
		if !ack.Cancel {
			t.Fatalf("hard Close sent non-Cancel ACK: %+v", ack)
		}
	default:
		t.Fatal("hard Close after local release did not reach RPC transport")
	}
}

func TestBrokerMultipleWrites(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	reader, meta := broker1.CreateStreamReader("reader1", "writer1", 1024)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	messages := []string{"First", "Second", "Third"}
	for _, msg := range messages {
		_, err := writer.Write([]byte(msg))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	expected := "FirstSecondThird"
	buf := make([]byte, len(expected))
	totalRead := 0
	for totalRead < len(expected) {
		n, err := reader.Read(buf[totalRead:])
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		totalRead += n
	}

	if string(buf) != expected {
		t.Fatalf("Expected %q, got %q", expected, string(buf))
	}

	writer.Close()
}

func TestBrokerCleanup(t *testing.T) {
	broker1, broker2 := setupBrokerPair()

	reader, meta := broker1.CreateStreamReader("reader1", "writer1", 1024)
	writer, err := broker2.CreateStreamWriter(meta)
	if err != nil {
		t.Fatalf("CreateStreamWriter failed: %v", err)
	}

	testData := []byte("cleanup test")
	writer.Write(testData)

	buf := make([]byte, 1024)
	reader.Read(buf)

	writer.Close()

	time.Sleep(100 * time.Millisecond)

	broker1.lock.Lock()
	_, readerExists := broker1.readers[meta.Id]
	broker1.lock.Unlock()

	if readerExists {
		t.Fatal("Reader should have been cleaned up")
	}

	broker2.lock.Lock()
	_, writerExists := broker2.writers[meta.Id]
	broker2.lock.Unlock()

	if writerExists {
		t.Fatal("Writer should have been cleaned up")
	}
}
