package interruptible_websocket_proxy

import (
	"container/list"
	"fmt"
	"golang.org/x/net/websocket"
	"math"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type ErrorInfo struct {
	lastCheckedTime *time.Time
	errorCount      int64
}

type BackendConn struct {
	net.Conn
	connUrl string
	ErrorInfo
}

// TODO: Can modify implementation to use channels

// BackendWSConnPool This should give a new connection for client connection request
// Should keep track of available backends at any point of time
// In a way it feels like it is doing the job of load balancer,
// but this additionally has to ensure there is at most one client connection per backend/pod
type BackendWSConnPool struct {
	// When new backend is available, it's url is added to the list here
	availableBackendUrls *list.List

	// Required to de-duplicate backendUrls
	registeredBackendUrls sync.Map

	// When a new backend connection is created, a reference is maintained here
	inUseMap sync.Map
	// When a client closes its connection with/without an error
	idleConnections      *list.List
	idleConnCount        *int64
	idleConnMutex        sync.Mutex
	erroredConnections   *list.List
	maxIdleConnections   int64
	maxAllowedErrorCount int64
	logger               logger
}

func NewBackendConnPool(maxIdleConnCount, maxAllowedErrorCountPerConn int64, logger logger) *BackendWSConnPool {
	urlList := list.New()
	erroredUrlList := list.New()
	idleConnList := list.New()
	var idleConnCount int64 = 0
	pool := &BackendWSConnPool{
		availableBackendUrls:  urlList,
		registeredBackendUrls: sync.Map{},
		inUseMap:              sync.Map{},
		idleConnections:       idleConnList,
		erroredConnections:    erroredUrlList,
		idleConnCount:         &idleConnCount,
		maxIdleConnections:    maxIdleConnCount,
		maxAllowedErrorCount:  maxAllowedErrorCountPerConn,
		logger:                logger,
	}
	pool.startIdleConnectionFiller()
	pool.erroredConnectionRefresher()
	return pool
}

// GetConn as soon as this is called, the connection will be immediately marked for use,
// defer calling this till the moment you need it
func (bp *BackendWSConnPool) GetConn() *BackendConn {
	i := 0
	for {
		conn := bp.tryAndFetchConnectionFromIdleList()
		if conn == nil {
			bp.logger.Debug("no idle connection is available, waiting for one to be available")
			backOffWait(&i, 5)
			continue
		}
		if conn.Conn == nil {
			netConn, err := newConn(conn.connUrl)
			if err != nil {
				bp.MarkError(conn)
				bp.logger.Error("obtained new connection but errored out while dialing", err)
				continue
			}
			conn.Conn = netConn
		}
		atomic.AddInt64(bp.idleConnCount, -1)
		bp.inUseMap.Store(conn.connUrl, conn)
		return conn
	}
}

func backOffWait(i *int, maxBackOffExponent int) {
	if *i >= maxBackOffExponent {
		*i = maxBackOffExponent
	}
	backOffSeconds := math.Pow(2, float64(*i))
	time.Sleep(time.Second * time.Duration(backOffSeconds))
	*i += 1
}

func (bp *BackendWSConnPool) AddToPool(url string) error {
	if _, ok := bp.registeredBackendUrls.Load(url); ok {
		return fmt.Errorf("backend url: %s already registered, retry later", url)
	}
	bp.availableBackendUrls.PushBack(url)
	bp.registeredBackendUrls.Store(url, true)
	bp.logger.Debug(fmt.Sprintf("added new connection to backend pool: %s", url))
	return nil
}

func (bp *BackendWSConnPool) MarkError(conn *BackendConn) {
	bp.inUseMap.Delete(conn.connUrl)
	now := time.Now()
	conn.lastCheckedTime = &now
	conn.errorCount += 1
	bp.erroredConnections.PushBack(conn)
}

func (bp *BackendWSConnPool) tryAndFetchConnectionFromIdleList() *BackendConn {
	bp.idleConnMutex.Lock()
	defer bp.idleConnMutex.Unlock()
	conn := bp.idleConnections.Front()
	if conn != nil {
		bp.idleConnections.Remove(conn)
		return conn.Value.(*BackendConn)
	}
	return nil
}

func (bp *BackendWSConnPool) startIdleConnectionFiller() {
	go func() {
		for {
			if atomic.LoadInt64(bp.idleConnCount) > bp.maxIdleConnections {
				time.Sleep(time.Second * 2)
				continue
			}

			front := bp.availableBackendUrls.Front()
			if front == nil {
				time.Sleep(time.Second * 2)
				continue
			}

			bp.availableBackendUrls.Remove(front)
			atomic.AddInt64(bp.idleConnCount, 1)
			bp.idleConnections.PushBack(&BackendConn{
				Conn:      nil,
				connUrl:   front.Value.(string),
				ErrorInfo: ErrorInfo{},
			})
			bp.logger.Debug(fmt.Sprintf("added new available url into idle connection list: %s", front.Value.(string)))
		}
	}()
}

func (bp *BackendWSConnPool) erroredConnectionRefresher() {
	go func() {
		for {
			front := bp.erroredConnections.Front()
			if front == nil {
				time.Sleep(time.Second * 2)
				continue
			}
			bp.erroredConnections.Remove(front)
			backendConn := front.Value.(*BackendConn)
			if backendConn.errorCount < bp.maxAllowedErrorCount {
				backendConn.Conn = nil
				bp.idleConnections.PushBack(backendConn)
				bp.logger.Debug(fmt.Sprintf("errored connection added back to idle connection list, current error count: %d", backendConn.errorCount))
			} else {
				bp.logger.Warn(fmt.Sprintf("de-registering the url as it has reached max error count: %s", backendConn.connUrl), nil)
			}
		}
	}()
}

func newConn(wsUrl string) (net.Conn, error) {
	parsedWSUrl, err := url.Parse(wsUrl)
	if err != nil {
		return nil, err
	}
	conn, err := websocket.Dial(wsUrl, "", parsedWSUrl.Host)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
