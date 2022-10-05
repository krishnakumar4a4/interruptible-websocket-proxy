# Interrupt-able WebSocket Proxy

Websocket connections are meant to be persistent for longer durations.If a backend is supporting multiple client websocket connections, it makes backend to less dynamic in terms of scaling, load balancing, deployments, quick restarts, minor network interruptions etc.

The core idea of this library to give you the ability to restore a backend websocket connection for a given client websocket connection when interrupted. The client will never know of the interruption as long as it's a minor one.

## Working model
This library creates a persistent connection object called `PreEmptiblePipe` for each unique client request using `PipeManager` instance.   
When the backend connection is down, next available backend connection is chosen from the pool. Once a new backend connection is restored, all the data received in the meantime will be flushed to the backend connection first before connecting client stream.   
The data received from client is temporarily stored in a byte array in memory. There is a max limit for each byte array, if reached before finding a backed connection from pool, the client connection is dropped as well to prevent memory hog. 
`PipeManager` also can register/de-register a backend connection to availability pool.
