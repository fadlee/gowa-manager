package system

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fadlee/gowa-manager/internal/instances"
)

const (
	minInstancePort = 8000
	maxInstancePort = 9000
)

type InstanceLister interface {
	List(context.Context) ([]instances.Instance, error)
}

type SystemService struct {
	repo                    InstanceLister
	dataDir                 string
	managerVersion          string
	started                 time.Time
	isInstancePortAvailable func(int) bool
}

type SystemStatus struct {
	TotalInstances    int
	RunningInstances  int
	StoppedInstances  int
	AllocatedPorts    int
	NextAvailablePort int
	Uptime            int64
	ManagerVersion    string
}

type SystemConfig struct {
	PortRange         PortRange
	DataDirectory     string
	BinariesDirectory string
}

type PortRange struct {
	Min int
	Max int
}

func NewSystemService(repo InstanceLister, dataDir, managerVersion string) *SystemService {
	return &SystemService{
		repo:                    repo,
		dataDir:                 dataDir,
		managerVersion:          managerVersion,
		started:                 time.Now(),
		isInstancePortAvailable: IsPortAvailable,
	}
}

func (s *SystemService) GetSystemStatus(ctx context.Context) (SystemStatus, error) {
	instances, err := s.repo.List(ctx)
	if err != nil {
		return SystemStatus{}, err
	}
	status := SystemStatus{ManagerVersion: s.managerVersion}
	status.TotalInstances = len(instances)
	allocated := map[int]bool{}
	for _, instance := range instances {
		switch instance.Status {
		case "running":
			status.RunningInstances++
		case "stopped":
			status.StoppedInstances++
		}
		if instance.Port != nil {
			status.AllocatedPorts++
			allocated[*instance.Port] = true
		}
	}
	status.NextAvailablePort = minInstancePort
	for port := minInstancePort; port <= maxInstancePort; port++ {
		if allocated[port] {
			continue
		}
		if s.isInstancePortAvailable(port) {
			status.NextAvailablePort = port
			break
		}
	}
	status.Uptime = time.Since(s.started).Milliseconds()
	return status, nil
}

func (s *SystemService) GetSystemConfig() (SystemConfig, error) {
	dataDir, err := filepath.Abs(s.dataDir)
	if err != nil {
		return SystemConfig{}, err
	}
	return SystemConfig{
		PortRange: PortRange{
			Min: minInstancePort,
			Max: maxInstancePort,
		},
		DataDirectory:     dataDir,
		BinariesDirectory: filepath.Join(dataDir, "binaries"),
	}, nil
}
