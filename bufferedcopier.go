package interruptible_websocket_proxy

import (
	"fmt"
	"io"
	"log"
	"time"
)

type writeErr struct {
	error
	CopyDirection
}

type readErr struct {
	error
	CopyDirection
}

type CopyDirection int

const (
	CopyToBackend CopyDirection = iota + 1
	CopyFromBacked
)

// copyBuffer is the actual implementation of Copy and CopyBuffer.
// if buf is nil, one is allocated.
// TODO: Should mutex writes to backend error
func (pep *PreEmptablePipe) copyBuffer(cd CopyDirection, errChan chan error) {
	var src func() io.Reader
	var dst func() io.Writer
	var written int64
	var err error

	if cd == CopyToBackend {
		src = func() io.Reader { return pep.ClientConn }
		dst = func() io.Writer {
			return pep.BackendConn
		}
	} else {
		src = func() io.Reader {
			return pep.BackendConn
		}
		dst = func() io.Writer {
			return pep.ClientConn
		}
	}

	size := 32 * 1024
	if l, ok := src().(*io.LimitedReader); ok && int64(size) > l.N {
		if l.N < 1 {
			size = 1
		} else {
			size = int(l.N)
		}
	}
	buf := make([]byte, size)
	for {
		if cd == CopyFromBacked && pep.BackendErr != nil {
			if len(pep.backendBuffer) > pep.bufferByteLimit {
				err = writeErr{error: fmt.Errorf("backend buffer reached max limit, exiting")}
				log.Println(err)
				break
			}
			time.Sleep(2 * time.Second)
			continue
		}
		nr, srcReadErr := src().Read(buf)
		if nr > 0 {
			if cd == CopyToBackend && pep.BackendErr != nil {
				if len(pep.backendBuffer)+len(buf[0:nr]) > pep.bufferByteLimit {
					err = writeErr{error: fmt.Errorf("backend buffer reached max limit, exiting")}
					log.Println(err)
					break
				}
				pep.backendBuffer = append(pep.backendBuffer, buf[0:nr]...)
				continue
			} else if cd == CopyToBackend && pep.BackendErr == nil && len(pep.backendBuffer) > 0 {
				// TODO: this one is quick hack, can implement to write chunks if writes are failing with large buffer size
				buf = append(pep.backendBuffer, buf[0:nr]...)
				nr = len(pep.backendBuffer)
			}
			nw, ew := dst().Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = fmt.Errorf("invalid write error")
				}
			}
			written += int64(nw)
			if ew != nil {
				err = writeErr{error: ew, CopyDirection: cd}
				if cd == CopyToBackend {
					//pep.BackendErr = err
					pep.backendBuffer = append(pep.backendBuffer, buf[0:nr]...)
					//errChan <- err
					continue
				}
				//errChan <- err
				break
			}
			if nr != nw {
				invalidWriteErr := fmt.Errorf("invalid write error: %s", io.ErrShortWrite)
				err = writeErr{error: invalidWriteErr, CopyDirection: cd}
				log.Println(err)
				if cd == CopyToBackend {
					//pep.BackendErr = err
					pep.backendBuffer = append(pep.backendBuffer, buf[0:nr]...)
					//errChan <- err
					continue
				}
				//errChan <- err
				break
			} else if len(pep.backendBuffer) > 0 {
				pep.backendBuffer = pep.backendBuffer[:0]
			}
		}
		// In case of reading from client connection is an error, stop
		if cd == CopyToBackend && srcReadErr != nil {
			log.Printf("WARN: read from client connection failed with err: %s", srcReadErr)
			if srcReadErr != io.EOF {
				err = readErr{error: srcReadErr, CopyDirection: cd}
			}
			break
		} else if cd == CopyFromBacked && srcReadErr != nil {
			log.Printf("WARN: backend connection failed with err: %s", srcReadErr)
			if pep.BackendErr == nil {
				pep.BackendErr = readErr{error: srcReadErr, CopyDirection: cd}
				errChan <- pep.BackendErr
			}
			if len(pep.backendBuffer) > pep.bufferByteLimit {
				err = writeErr{error: fmt.Errorf("backend buffer reached max limit, exiting")}
				break
			}
		}
	}
}
