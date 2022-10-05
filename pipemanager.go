package interruptible_websocket_proxy

import (
	"fmt"
	"github.com/google/uuid"
	"io"
	"log"
	"sync"
	"time"
)

type PipeErrorListener func(pipeId uuid.UUID, err error)

type PipeManager struct {
	// Eventually this should be offloaded to another dedicated service/ redis
	clientPipesMap sync.Map

	backendPool *BackendWSConnPool
}

func NewPipeManager() *PipeManager {
	pool := NewBackendConnPool()
	return &PipeManager{
		clientPipesMap: sync.Map{},
		backendPool:    pool,
	}
}

func (pm *PipeManager) AddConnectionToPool(url string) {
	pm.backendPool.AddToPool(url)
}

func (pm *PipeManager) CreatePipe(clientId uuid.UUID, conn io.ReadWriteCloser) error {
	errChan := make(chan error)
	// Create and get backendConn
	backendConn, err := pm.backendPool.GetConn()
	if err != nil {
		return err
	}

	preEmptablePipe := NewPreEmptablePipe(clientId, conn, backendConn)
	preEmptablePipe.ErrorListener = func(pipeId uuid.UUID, err error) {
		log.Printf("error during stream: %s", err.Error())
		for {
			if preEmptablePipe.BackendErr != nil {
				log.Printf("recognised backedend error: %s, attempting another connection", preEmptablePipe.BackendErr)
				pm.backendPool.MarkError(preEmptablePipe.BackendConn.(*BackendConn))
				backendConn, err := pm.backendPool.GetConn()
				if err != nil {
					log.Printf("error unable to get connection from backendPool: %s", err.Error())
					// TODO: Can have exponential backoff here
					time.Sleep(time.Second)
					continue
				}
				log.Printf("substituted new backend for pipe related to client id: %s", clientId)
				preEmptablePipe.BackendConn = backendConn
				preEmptablePipe.BackendErr = nil
				break
			} else if preEmptablePipe.ClientErr != nil {
				// TODO: Can have intelligent way of waiting for client to comeback
				errChan <- fmt.Errorf("client connection errored out: %s", preEmptablePipe.ClientErr)
				break
			}
		}
	}
	pm.clientPipesMap.Store(clientId, preEmptablePipe)
	pipeErr := preEmptablePipe.Stream()
	if pipeErr != nil {
		errChan <- pipeErr
	}
	log.Println("returning from create pipe")
	return <-errChan
}
