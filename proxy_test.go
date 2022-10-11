package interruptible_websocket_proxy

import (
	"go.uber.org/zap"
	"golang.org/x/net/websocket"
	"log"
	"net/http"
	"net/url"
)

func ExampleNewInterruptibleWebsocketProxyHandler() {
	logger := zap.NewExample()

	lgr := &exampleLogger{
		logger: logger,
	}

	parsedURL, err := url.Parse("ws://localhost:8080")
	if err != nil {
		log.Printf("error parsing url for setting origin: %s", err)
		return
	}

	memoryLimitPerConn := 5 * 1024 * 1024

	// Create proxy handler instance
	interruptibleWebsocketProxyHandler := NewInterruptibleWebsocketProxyHandler(websocket.Config{Origin: parsedURL}, HandlerConfig{
		MaxIdleConnCount:                   5,
		MaxAllowedErrorCountPerConn:        100,
		InterruptMemoryLimitPerConnInBytes: memoryLimitPerConn,
		ClientIdExtractFunc:                nil,
	}, lgr)

	// Register proxy handler to http server
	mux := http.NewServeMux()
	mux.Handle("/", interruptibleWebsocketProxyHandler)

	// Start webserver
	err = http.ListenAndServe(":8080", mux)
	if err != nil {
		lgr.Error("error starting websocket server", err)
		return
	}
}
