package interruptible_websocket_proxy

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/websocket"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type testLogger struct{}

func (testLogger) Warn(msg string, nestedErr error) {}

func (testLogger) Error(msg string, nestedErr error) {}

func (testLogger) Debug(msg string) {}

func TestNewBackendConnPool(t *testing.T) {
	tl := &testLogger{}

	t.Run("ShouldBeAbleToABackendUrlExactlyOnceToThePool", func(t *testing.T) {
		pool := NewBackendConnPool(5, 100, tl)
		err := pool.AddToPool("ws://localhost:8081")
		assert.Nil(t, err)
		err = pool.AddToPool("ws://localhost:8081")
		assert.NotNil(t, err)
	})

	t.Run("ShouldBeAbleToGetConnWhenAvailable", func(t *testing.T) {
		go func() {
			server := websocket.Server{
				Handler: func(c *websocket.Conn) {
					defer c.Close()
					time.Sleep(time.Second)
				},
			}
			err := http.ListenAndServe(":8081", server)
			assert.Nil(t, err)
		}()
		pool := NewBackendConnPool(5, 1, tl)
		expUrl := "ws://localhost:8081"

		err := pool.AddToPool(expUrl)
		assert.Nil(t, err)

		_, ok := pool.inUseMap.Load(expUrl)
		assert.False(t, ok)

		conn := pool.GetConn()
		assert.Nil(t, err)
		assert.Equal(t, expUrl, conn.connUrl)

		_, ok = pool.inUseMap.Load(conn.connUrl)
		assert.True(t, ok)
	})

	t.Run("ShouldBeAbleToMarkErrorToExistingBackendAndIncrementErrorCount", func(t *testing.T) {
		go func() {
			server := websocket.Server{
				Handler: func(c *websocket.Conn) {
					defer c.Close()
					time.Sleep(time.Second)
				},
			}
			err := http.ListenAndServe(":8082", server)
			assert.Nil(t, err)
		}()
		pool := NewBackendConnPool(5, 1, tl)
		expUrl := "ws://localhost:8082"
		err := pool.AddToPool(expUrl)
		assert.Nil(t, err)
		conn := pool.GetConn()
		pool.MarkError(conn)
		assert.Equal(t, int64(1), conn.errorCount)
	})

	t.Run("ShouldRemoveConnectionIfReachedMaxErrorCount", func(t *testing.T) {
		go func() {
			server := websocket.Server{
				Handler: func(c *websocket.Conn) {
					defer c.Close()
					time.Sleep(time.Second)
				},
			}
			err := http.ListenAndServe(":8083", server)
			assert.Nil(t, err)
		}()
		pool := NewBackendConnPool(5, 2, tl)
		expUrl := "ws://localhost:8083"
		err := pool.AddToPool(expUrl)
		assert.Nil(t, err)
		conn := pool.GetConn()
		pool.MarkError(conn)

		conn = pool.GetConn()
		pool.MarkError(conn)

		assert.Eventually(t, func() bool {
			_, ok := pool.inUseMap.Load(expUrl)
			fmt.Println(ok)
			fmt.Println(atomic.LoadInt64(pool.idleConnCount))
			fmt.Println(pool.idleConnections.Len())
			fmt.Println(pool.erroredConnections.Len())
			return !ok && atomic.LoadInt64(pool.idleConnCount) == -1 && pool.idleConnections.Len() == 0 && pool.erroredConnections.Len() == 0
		}, time.Second*4, time.Second)
	})
}
