package interruptible_websocket_proxy

import (
	"fmt"
	"github.com/google/uuid"
	"io"
	"log"
	"sync"
)

type PreEmptablePipe struct {
	// ID Unique identifier for this pipe
	ID uuid.UUID
	// ClientUUID Unique identifier for the client
	ClientID      uuid.UUID
	ErrorListener PipeErrorListener
	ClientConn    io.ReadWriteCloser
	BackendConn   io.ReadWriteCloser
	ClientErr     error
	BackendErr    error
	// streamMut useful to update streamOn state
	streamMut sync.Mutex
	// streamOn useful to quickly check if stream is on, used to avoid duplicate streams
	streamOn        bool
	backendBuffer   []byte
	bufferByteLimit int
}

// NewPreEmptablePipe Creates a new preempt-able websocket pipe
func NewPreEmptablePipe(clientID uuid.UUID, clientConn, backendConn io.ReadWriteCloser) *PreEmptablePipe {
	limit := 5 * 1024 * 1024
	return &PreEmptablePipe{
		ID:              uuid.New(),
		ClientID:        clientID,
		ClientConn:      clientConn,
		BackendConn:     backendConn,
		bufferByteLimit: limit,
		backendBuffer:   make([]byte, 0, limit),
	}
}

// Stream runs back and forth stream copy between clientConn and backendConn
// Also helps to restart stream when pre-empted
func (pep *PreEmptablePipe) Stream() error {
	pep.streamMut.Lock()
	defer pep.streamMut.Unlock()
	if pep.streamOn {
		return fmt.Errorf("stream already running")
	}
	errChan := make(chan error)
	if pep.ClientConn == nil || pep.BackendConn == nil {
		return fmt.Errorf("error streaming, either of the connections are nil, clientConn: %v, backendConn: %v", pep.ClientConn, pep.BackendConn)
	}
	go pep.copyBuffer(CopyToBackend, errChan)
	go pep.copyBuffer(CopyFromBacked, errChan)
	go pep.listenForErrors(errChan)
	pep.streamOn = true
	return nil
}

func (pep *PreEmptablePipe) listenForErrors(errChan chan error) {
	for {
		err := <-errChan
		log.Printf("error reported to listener: %s", err)
		pep.streamOn = false
		pep.ErrorListener(pep.ID, err)
	}
}