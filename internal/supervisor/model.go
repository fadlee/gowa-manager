package supervisor

import "time"

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateFailed   State = "failed"
)

type platformHandle struct{}

type ProcessRecord struct {
	InstanceID int64
	Generation int64
	PID        int
	StartedAt  time.Time
	handle     *platformHandle
}

type ProcessSnapshot struct {
	InstanceID int64
	Generation int64
	State      State
	PID        int
	StartedAt  time.Time
}
