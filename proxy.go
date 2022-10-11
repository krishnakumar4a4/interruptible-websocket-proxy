package interruptible_websocket_proxy

import (
	"fmt"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
	"strings"
)

// InterruptibleWebsocketProxyHandler Wrapped [Golang websocket server](https://pkg.go.dev/golang.org/x/net/websocket#Server)
type InterruptibleWebsocketProxyHandler struct {
	websocket.Server
	*WebsocketPipeManager
}

// HandlerConfig Configuration for the proxy and websocket handler
type HandlerConfig struct {
	MaxIdleConnCount                   int64
	MaxAllowedErrorCountPerConn        int64
	InterruptMemoryLimitPerConnInBytes int
	ClientIdExtractFunc                func(conn *websocket.Conn) (uuid.UUID, error)
}

// NewInterruptibleWebsocketProxyHandler Returns a readily configured websocket handler. The returned handler can be
// directly registered to a http web server.
//
// E.g:
//  mux := http.NewServeMux()
//	mux.Handle("/", websocketServer)
//
//	// Start webserver
//	err = http.ListenAndServe(":8080", mux)
//	if err != nil {
//		lgr.Error("error starting websocket server", err)
//		return
//	}
func NewInterruptibleWebsocketProxyHandler(wsConfig websocket.Config,
	handlerConfig HandlerConfig, logger logger) *InterruptibleWebsocketProxyHandler {

	pool := NewBackendConnPool(handlerConfig.MaxIdleConnCount, handlerConfig.MaxAllowedErrorCountPerConn, logger)
	pipeManager := NewWebsocketPipeManager(pool, handlerConfig.InterruptMemoryLimitPerConnInBytes, logger)

	var proxyWSHandler = websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()

		var clientId uuid.UUID
		var err error

		if handlerConfig.ClientIdExtractFunc != nil {
			clientId, err = handlerConfig.ClientIdExtractFunc(conn)
		} else {
			clientIdString := strings.TrimPrefix(conn.Request().URL.Path, "/")
			clientId, err = uuid.Parse(clientIdString)
		}
		if err != nil {
			logger.Error(fmt.Sprintf("error extracting clientId"), err)
			return
		}

		// Create persistent pipe, this is a blocking call
		err = pipeManager.CreatePipe(clientId, conn)
		if err != nil {
			logger.Error("error creating persistent pipe", err)
			return
		}
	})

	websocketServer := websocket.Server{
		Config:  wsConfig,
		Handler: proxyWSHandler,
	}

	return &InterruptibleWebsocketProxyHandler{websocketServer, pipeManager}
}
