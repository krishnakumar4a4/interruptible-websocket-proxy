# Interruptible WebSocket Proxy

### (a.k.a UnInterrupted WebSocket Proxy)

Websocket connections are meant to be persistent for longer durations. If a backend is supporting multiple client
websocket connections, it makes the backend to be less dynamic in terms of scaling, load balancing, deployments, quick
restarts, minor network interruptions etc.

The core idea of this library is to be used an interrupt resistant component for a websocket proxy. It gives the proxy an ability to restore a backend websocket connection for a given client websocket connection from minor interruptions.

Note that overall tolerance of this proxy is based on the availability of number of connections and free RAM. It's best to limit number of connections to this proxy and also set per connection memory limit. There is a default per connection memory limit of 5MB (if not set explicitly).

## Working model

This library creates a persistent connection object called `PersistentPipe` for each unique client request
using `PipeManager` instance.   
When the backend connection is down, next available backend connection is chosen from the pool. Once a new backend
connection is restored, all the data received in the meantime will be flushed to the backend connection first before
connecting client stream.   
The data received from client is temporarily stored in a byte array in memory. There is a max limit for each byte array,
if reached before finding a backed connection from pool, the client connection is dropped to prevent memory hog of a particular connection over other ones.
`PipeManager` also can register/de-register a backend connection to availability pool.

## How to Use

Wire up the `PipeManager` to a regular websocket server thus making it a proxy.

Example Usage:

```go
package interruptible_websocket_proxy

import (
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/net/websocket"
	"net/http"
	"net/url"
	"strings"
)

type proxyLogger struct {
	logger *zap.Logger
}

func (pl *proxyLogger) Warn(msg string, nestedErr error) {
	pl.logger.Warn(msg, zap.Error(nestedErr))
}

func (pl *proxyLogger) Error(msg string, nestedErr error) {
	pl.logger.Error(msg, zap.Error(nestedErr))
}

func (pl *proxyLogger) Debug(msg string) {
	pl.logger.Debug(msg)
}

func Run() {
	mux := http.NewServeMux()
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err.Error())
	}
	lgr := &proxyLogger{
		logger: logger,
	}
	pool := NewBackendConnPool(5, 100, lgr)
	memoryLimitPerConn := 5 * 1024 * 1024
	pipeManager := NewWebsocketPipeManager(pool, memoryLimitPerConn, lgr)
	proxyWSHandler := websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()
		clientIdString := strings.TrimPrefix(conn.Request().URL.Path, "/")
		clientId, err := uuid.Parse(clientIdString)
		if err != nil {
			lgr.Error(fmt.Sprintf("error parsing clientId, invalid clientIdString in the path: %s", clientIdString), err)
			return
		}
		err = pipeManager.CreatePipe(clientId, conn)
		if err != nil {
			lgr.Error("error creating persistent pipe", err)
			return
		}
	})

	parsedURL, err := url.Parse("ws://localhost:8080")
	if err != nil {
		lgr.Error("error parsing url for setting origin", err)
		return
	}

	server := websocket.Server{
		Config:  websocket.Config{Origin: parsedURL},
		Handler: proxyWSHandler,
	}
	mux.Handle("/", server)
	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		lgr.Error("error starting websocket server", err)
		return
	}
}

```

```curl
curl --location --request GET 'http://localhost:8080/098d8a97-3615-4eb8-b803-c57c01c7536c'
```
