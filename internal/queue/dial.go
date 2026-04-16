package queue

import (
	"net"
	"time"
)

// dialTCP tries to connect to addr with a timeout.
func dialTCP(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, timeout)
}
