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

type exampleLogger struct {
	logger *zap.Logger
}

func (pl *exampleLogger) Warn(msg string, nestedErr error) {
	pl.logger.Warn(msg, zap.Error(nestedErr))
}

func (pl *exampleLogger) Error(msg string, nestedErr error) {
	pl.logger.Error(msg, zap.Error(nestedErr))
}

func (pl *exampleLogger) Debug(msg string) {
	pl.logger.Debug(msg)
}

// ExampleNewWebsocketPipeManager Example to use a PipeManager instance with a websocket server. The usage of PipeManager
// instance within websocket server handler instance effectively converts websocket server into a transparent interruptible
// websocket proxy
func ExampleNewWebsocketPipeManager() {
	logger := zap.NewExample()

	lgr := &exampleLogger{
		logger: logger,
	}

	// Create pipe manager instance
	pool := NewBackendConnPool(5, 100, lgr)
	memoryLimitPerConn := 5 * 1024 * 1024
	pipeManager := NewWebsocketPipeManager(pool, memoryLimitPerConn, lgr)

	// Websocket Handler
	proxyWSHandler := websocket.Handler(func(conn *websocket.Conn) {
		defer conn.Close()

		clientIdString := strings.TrimPrefix(conn.Request().URL.Path, "/")
		clientId, err := uuid.Parse(clientIdString)
		if err != nil {
			lgr.Error(fmt.Sprintf("error parsing clientId, invalid clientIdString in the path: %s", clientIdString), err)
			return
		}

		// Create persistent pipe, this is a blocking call
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

	// Setup websocket server with basic config and handler
	websocketServer := websocket.Server{
		Config:  websocket.Config{Origin: parsedURL},
		Handler: proxyWSHandler,
	}

	mux := http.NewServeMux()
	mux.Handle("/", websocketServer)

	// Start webserver
	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		lgr.Error("error starting websocket server", err)
		return
	}
}
