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

type ConnectionProviderPool interface {
	AddToPool(url string) error
	GetConn() (*BackendConn, error)
	MarkError(conn *BackendConn)
}

type WebsocketPipeManager struct {
	// Eventually this should be offloaded to another dedicated service/ redis
	clientPipesMap sync.Map

	backendPool ConnectionProviderPool
	backOffFunc func(counter *int64)
}

// NewWebsocketPipeManager Creates a websocket pipe manager with provided connection pool
func NewWebsocketPipeManager(pool ConnectionProviderPool) *WebsocketPipeManager {
	return &WebsocketPipeManager{
		clientPipesMap: sync.Map{},
		backendPool:    pool,
	}
}

// NewDefaultWebsocketPipeManager Creates a default pipe manager with given pool configuration as arguments
func NewDefaultWebsocketPipeManager(maxIdleConnCount, maxAllowedErrorCount int64) *WebsocketPipeManager {
	pool := NewBackendConnPool(maxIdleConnCount, maxAllowedErrorCount)
	return &WebsocketPipeManager{
		clientPipesMap: sync.Map{},
		backendPool:    pool,
	}
}

// AddConnectionToPool Can add connection to the pool
// Note: the format for the URL should be checked for validity beforehand before calling this
// If you pass invalid websocket url, it will unnecessarily add delays for fetching fresh connections
func (pm *WebsocketPipeManager) AddConnectionToPool(url string) error {
	return pm.backendPool.AddToPool(url)
}

// SetBackOffStrategyFunc Can set a custom defined back off function when failed to get new connection for backend
// The argument for the back off function can be used to pass counter from outside which can help with strategies like exponential backoff etc
func (pm *WebsocketPipeManager) SetBackOffStrategyFunc(backOffFunc func(counter *int64)) {
	pm.backOffFunc = backOffFunc
}

// CreatePipe This function is a blocking call when the pipe runs till completion.
// Returns nil if client closed the connection for any reason, otherwise can return error during connection fetch, stream
func (pm *WebsocketPipeManager) CreatePipe(clientId uuid.UUID, conn io.ReadWriteCloser) error {
	if _, ok := pm.clientPipesMap.Load(clientId); ok {
		return fmt.Errorf("a pipe already existed with clientId: %s", clientId)
	}
	errChan := make(chan error)
	// Create and get backendConn
	backendConn, err := pm.backendPool.GetConn()
	if err != nil {
		return err
	}

	persistentPipe := NewPersistentPipe(clientId, conn, backendConn)
	persistentPipe.ErrorListener = func(pipeId uuid.UUID, err error) {
		log.Printf("error during stream: %s", err.Error())
		for {
			if persistentPipe.BackendErr != nil {
				log.Printf("recognised backedend error: %s, attempting another connection", persistentPipe.BackendErr)
				pm.backendPool.MarkError(persistentPipe.BackendConn.(*BackendConn))
				backendConn, err := pm.backendPool.GetConn()
				if err != nil {
					log.Printf("error unable to get connection from backendPool: %s", err.Error())
					if pm.backOffFunc != nil {
						pm.backOffFunc(nil)
					} else {
						time.Sleep(time.Second)
					}
					continue
				}
				log.Printf("substituted new backend for pipe related to client id: %s", clientId)
				persistentPipe.BackendConn = backendConn
				persistentPipe.BackendErr = nil
				break
			} else if persistentPipe.ClientErr != nil {
				// TODO: Can have intelligent way of waiting for client to comeback
				errChan <- fmt.Errorf("client connection errored out: %s", persistentPipe.ClientErr)
				break
			}
		}
	}
	pm.clientPipesMap.Store(clientId, persistentPipe)
	pipeErr := persistentPipe.Stream()
	if pipeErr != nil {
		errChan <- pipeErr
	}
	log.Println("returning from create pipe")
	return <-errChan
}
