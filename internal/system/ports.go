package system

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
)

var ErrNoAvailablePort = errors.New("no available port")

type PortAllocator struct {
	repo        InstanceLister
	isAvailable func(int) bool
	mu          sync.Mutex
	reserved    map[int]bool
}

func NewPortAllocator(repo InstanceLister) *PortAllocator {
	return &PortAllocator{repo: repo, isAvailable: IsPortAvailable, reserved: map[int]bool{}}
}

func IsPortAvailable(port int) bool {
	if port < 1024 || port == 3000 {
		return false
	}
	return canBind(port)
}

func IsHTTPPortAvailable(port int) bool {
	return canBind(port)
}

func (p *PortAllocator) Next(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	instances, err := p.repo.List(ctx)
	if err != nil {
		return 0, err
	}
	allocated := map[int]bool{}
	for _, instance := range instances {
		if instance.Port != nil {
			allocated[*instance.Port] = true
		}
	}
	for port := minInstancePort; port <= maxInstancePort; port++ {
		if allocated[port] || p.reserved[port] {
			continue
		}
		if p.isAvailable(port) {
			p.reserved[port] = true
			return port, nil
		}
	}
	return 0, ErrNoAvailablePort
}

func canBind(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	return listener.Close() == nil
}
