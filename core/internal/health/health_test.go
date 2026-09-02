package health

import (
	"net"
	"strconv"
	"testing"
)

func TestFindFreePortSkipsTCPPortWithoutHTTP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	if !IsPortOccupied(port) {
		t.Fatalf("TCP listener on %d must be reported occupied even without an HTTP endpoint", port)
	}
	if got := FindFreePort(port); got == port {
		t.Fatalf("FindFreePort selected busy non-HTTP port %d", port)
	}
}

func TestIsPortOccupiedReturnsFalseForReleasedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if IsPortOccupied(port) {
		t.Fatalf("released TCP port %s must be available", strconv.Itoa(port))
	}
}
