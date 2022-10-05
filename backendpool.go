package interruptible_websocket_proxy

import (
	"container/list"
	"golang.org/x/net/websocket"
	"log"
	"math"
	"net"
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

// BackendWSConnPool This should give a new connection for client connection request
// Should keep track of available backends at any point of time
// In a way it feels like it is doing the job of load balancer,
// but this additionally has to ensure there is at most one client connection per backend/pod
// TODO: When to remove an url and associated conn from Pool?
type BackendWSConnPool struct {
	// When new backend is available, it's url is added to the list here
	availableBackendUrls *list.List
	// When a new backend connection is created, a reference is maintained here
	// TODO: Sync map here to prevent races
	inUseMap map[string]*BackendConn
	// When a client closes its connection with/without an error
	// TODO: Sync map here to prevent races
	idleConnections      *list.List
	idleConnCount        *int64
	idleConnMutex        sync.Mutex
	erroredConnections   *list.List
	maxIdleConnections   int64
	maxAllowedErrorCount int64
}

func NewBackendConnPool() *BackendWSConnPool {
	urlList := list.New()
	erroredUrlList := list.New()
	idleConnList := list.New()
	var idleConnCount int64 = 0
	pool := &BackendWSConnPool{
		availableBackendUrls: urlList,
		inUseMap:             make(map[string]*BackendConn),
		idleConnections:      idleConnList,
		erroredConnections:   erroredUrlList,
		idleConnCount:        &idleConnCount,
		maxIdleConnections:   5,
		maxAllowedErrorCount: 100,
	}
	pool.startIdleConnectionFiller()
	pool.erroredConnectionRefresher()
	return pool
}

// GetConn as soon as this is called, the connection will be immediately marked for use,
// defer calling this till the moment you need it
func (bp *BackendWSConnPool) GetConn() (*BackendConn, error) {
	i := 0
	for {
		conn := bp.tryAndFetchConnectionFromIdleList()
		if conn == nil {
			log.Printf("WARN: no idle connection is available, waiting for one to be available")
			backOffWait(&i, 5)
			continue
		}
		if conn.Conn == nil {
			netConn, err := newConn(conn.connUrl)
			if err != nil {
				bp.MarkError(conn)
				log.Printf("ERROR: obtained new connection but errored out while dialing: %s", err.Error())
				continue
			}
			conn.Conn = netConn
		}
		log.Printf("INFO: obtained new connection from idle connections")
		atomic.AddInt64(bp.idleConnCount, -1)
		return conn, nil
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

func (bp *BackendWSConnPool) AddToPool(url string) {
	log.Printf("INFO: adding connection to backend pool: %s", url)
	//TODO: Deduplicate urls using hashmap?
	bp.availableBackendUrls.PushBack(url)
}

func (bp *BackendWSConnPool) MarkError(conn *BackendConn) {
	delete(bp.inUseMap, conn.connUrl)
	now := time.Now()
	conn.lastCheckedTime = &now
	conn.errorCount += 1
	log.Printf("ERROR: connection marked as error: %s", conn.connUrl)
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
				log.Printf("INFO: minimum idle connections exist, skip adding new one")
				time.Sleep(time.Second * 2)
				continue
			}

			front := bp.availableBackendUrls.Front()
			if front == nil {
				time.Sleep(time.Second * 2)
				continue
			}

			log.Printf("INFO: adding available url into idle connection list: %s", front.Value.(string))
			bp.availableBackendUrls.Remove(front)
			atomic.AddInt64(bp.idleConnCount, 1)
			bp.idleConnections.PushBack(&BackendConn{
				Conn:      nil,
				connUrl:   front.Value.(string),
				ErrorInfo: ErrorInfo{},
			})
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
				log.Printf("INFO: errored connection is added back to idle connection list as it did not reach max error count, current count: %d", backendConn.errorCount)
			} else {
				log.Printf("WARN: de-registering the url as it has reached max error count: %s", backendConn.connUrl)
			}
		}
	}()
}

func newConn(url string) (net.Conn, error) {
	// TODO: remove hard coded origin here
	conn, err := websocket.Dial(url, "", "http://localhost")
	if err != nil {
		return nil, err
	}
	return conn, nil
}
