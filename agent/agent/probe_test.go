package agent

import (
	"net"
	"testing"
	"time"
)

func TestProbeTCP_LocalListener(t *testing.T) {
	// start a local listener on random port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)
	port := addr.Port

	// run probeTCP against that port
	ports := []int{port, port + 1}
	open, err := probeTCP("127.0.0.1", ports, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("probeTCP returned error: %v", err)
	}
	found := false
	for _, p := range open {
		if p == port {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected port %d to be reported open, got %v", port, open)
	}
}
