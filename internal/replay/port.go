package replay

import (
	"fmt"
	"net"
)

// GetFreePort returns an available TCP port for ephemeral server execution.
func GetFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to allocate free port: %w", err)
	}
	defer l.Close()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("failed to cast listen address to TCPAddr")
	}
	return addr.Port, nil
}
