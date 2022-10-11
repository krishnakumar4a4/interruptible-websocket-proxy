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
The solution can be consumed in two ways

1. Wire up the `PipeManager` to a regular websocket server thus making it a proxy. Here is an [example usage](./pipemanager_test.go)

2. Wire up `InterruptibleWebsocketProxyHandler` to a http server to make a proxy as well. Here is an [example usage](./proxy_test.go)

A sample curl request for the examples
```curl
curl --location --request GET 'http://localhost:8080/098d8a97-3615-4eb8-b803-c57c01c7536c'
```

Additionally, in order register websocket urls to backend pool. We have to use the following method

```
// Refer to below pipemanager from example 1
pipeManager.AddConnectionToPool("ws://localhost:8081/listener")

// Refer to below interruptibleWebsocketProxyHandler from example 2
interruptibleWebsocketProxyHandler.AddConnectionToPool("ws://localhost:8081/listener")
```



