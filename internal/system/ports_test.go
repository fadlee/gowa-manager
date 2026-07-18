package system

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/fadlee/gowa-manager/internal/instances"
)

func TestIsPortAvailableRejectsPrivilegedAndReservedInstancePorts(t *testing.T) {
	if IsPortAvailable(1023) {
		t.Fatal("IsPortAvailable(1023) = true, want false")
	}
	if IsPortAvailable(3000) {
		t.Fatal("IsPortAvailable(3000) = true, want false")
	}
}

func TestIsHTTPPortAvailableAllowsManagerPort3000WhenOSAvailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:3000")
	if err != nil {
		t.Skipf("port 3000 unavailable on this host: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !IsHTTPPortAvailable(3000) {
		t.Fatal("IsHTTPPortAvailable(3000) = false, want true when OS port is free")
	}
}

func TestPortAvailabilityChecksOSBinding(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	if IsPortAvailable(port) {
		t.Fatalf("IsPortAvailable(%d) = true, want false while bound", port)
	}
	if IsHTTPPortAvailable(port) {
		t.Fatalf("IsHTTPPortAvailable(%d) = true, want false while bound", port)
	}
}

func TestPortAllocatorStartsAt8000AndSkipsAllocatedAndOSUnavailablePorts(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:8001")
	if err != nil {
		t.Skipf("port 8001 unavailable on this host: %v", err)
	}
	defer listener.Close()
	port8000 := 8000
	repo := fakeInstanceLister{instances: []instances.Instance{{Port: &port8000}}}
	allocator := NewPortAllocator(repo)

	port, err := allocator.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if port != 8002 {
		t.Fatalf("Next() = %d, want 8002", port)
	}
}

func TestPortAllocatorReturnsErrNoAvailablePortWhenExhausted(t *testing.T) {
	repo := fakeInstanceLister{}
	allocator := NewPortAllocator(repo)
	allocator.isAvailable = func(int) bool { return false }

	_, err := allocator.Next(context.Background())
	if !errors.Is(err, ErrNoAvailablePort) {
		t.Fatalf("Next() error = %v, want ErrNoAvailablePort", err)
	}
}

func TestIsHTTPPortAvailableRejectsInvalidPorts(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		t.Run(strconv.Itoa(port), func(t *testing.T) {
			if IsHTTPPortAvailable(port) {
				t.Fatalf("IsHTTPPortAvailable(%d) = true, want false", port)
			}
		})
	}
}
