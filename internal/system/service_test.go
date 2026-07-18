package system

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fadlee/gowa-manager/internal/instances"
)

type fakeInstanceLister struct {
	instances []instances.Instance
	err       error
}

func (f fakeInstanceLister) List(context.Context) ([]instances.Instance, error) {
	return f.instances, f.err
}

func TestGetSystemStatusCountsInstancesAndReportsMetadata(t *testing.T) {
	port8000 := 8000
	port8002 := 8002
	service := NewSystemService(fakeInstanceLister{instances: []instances.Instance{
		{Status: "running", Port: &port8000},
		{Status: "stopped"},
		{Status: "running", Port: &port8002},
	}}, "./data", "v1.2.3")
	service.started = time.Now().Add(-1500 * time.Millisecond)

	status, err := service.GetSystemStatus(context.Background())
	if err != nil {
		t.Fatalf("GetSystemStatus() error = %v", err)
	}

	if status.TotalInstances != 3 {
		t.Fatalf("TotalInstances = %d, want 3", status.TotalInstances)
	}
	if status.RunningInstances != 2 {
		t.Fatalf("RunningInstances = %d, want 2", status.RunningInstances)
	}
	if status.StoppedInstances != 1 {
		t.Fatalf("StoppedInstances = %d, want 1", status.StoppedInstances)
	}
	if status.AllocatedPorts != 2 {
		t.Fatalf("AllocatedPorts = %d, want 2", status.AllocatedPorts)
	}
	if status.NextAvailablePort != 8001 {
		t.Fatalf("NextAvailablePort = %d, want 8001", status.NextAvailablePort)
	}
	if status.Uptime < 1000 || status.Uptime > 5000 {
		t.Fatalf("Uptime = %d, want milliseconds near 1500", status.Uptime)
	}
	if status.ManagerVersion != "v1.2.3" {
		t.Fatalf("ManagerVersion = %q, want v1.2.3", status.ManagerVersion)
	}
}

func TestGetSystemStatusNextAvailablePortSkipsAllocatedAndUnavailablePorts(t *testing.T) {
	port8000 := 8000
	service := NewSystemService(fakeInstanceLister{instances: []instances.Instance{
		{Port: &port8000},
	}}, "./data", "dev")
	service.isInstancePortAvailable = func(port int) bool { return port != 8001 }

	status, err := service.GetSystemStatus(context.Background())
	if err != nil {
		t.Fatalf("GetSystemStatus() error = %v", err)
	}

	if status.NextAvailablePort != 8002 {
		t.Fatalf("NextAvailablePort = %d, want 8002", status.NextAvailablePort)
	}
}

func TestGetSystemStatusDefaultsNextAvailablePortTo8000(t *testing.T) {
	service := NewSystemService(fakeInstanceLister{}, "./data", "dev")

	status, err := service.GetSystemStatus(context.Background())
	if err != nil {
		t.Fatalf("GetSystemStatus() error = %v", err)
	}

	if status.NextAvailablePort != 8000 {
		t.Fatalf("NextAvailablePort = %d, want 8000", status.NextAvailablePort)
	}
}

func TestGetSystemConfigResolvesDataAndBinariesDirectories(t *testing.T) {
	service := NewSystemService(fakeInstanceLister{}, filepath.Join("relative", "data"), "dev")

	config, err := service.GetSystemConfig()
	if err != nil {
		t.Fatalf("GetSystemConfig() error = %v", err)
	}

	wantDataDir, err := filepath.Abs(filepath.Join("relative", "data"))
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if config.PortRange.Min != 8000 || config.PortRange.Max != 9000 {
		t.Fatalf("PortRange = %+v, want min 8000 max 9000", config.PortRange)
	}
	if config.DataDirectory != wantDataDir {
		t.Fatalf("DataDirectory = %q, want %q", config.DataDirectory, wantDataDir)
	}
	if config.BinariesDirectory != filepath.Join(wantDataDir, "binaries") {
		t.Fatalf("BinariesDirectory = %q, want binaries under data dir", config.BinariesDirectory)
	}
}
