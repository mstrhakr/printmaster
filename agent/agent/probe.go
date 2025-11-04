package agent

import (
	"net"
	"strconv"
	"time"
)

// probeTCP tries to connect to the provided ports on the IP with the given timeout.
// Returns the slice of ports that accepted a TCP connection.
func probeTCP(ip string, ports []int, timeout time.Duration) ([]int, error) {
	open := []int{}
	for _, p := range ports {
		addr := net.JoinHostPort(ip, strconv.Itoa(p))
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			// treat as closed/filtered
			continue
		}
		conn.Close()
		open = append(open, p)
	}
	return open, nil
}
