package supervisor

import (
	"errors"
	"sync"
)

var (
	ErrOperationInProgress = errors.New("supervisor operation already in progress")
	ErrStaleGeneration     = errors.New("supervisor stale generation")
)

type OperationFunc func(generation int64) (ProcessSnapshot, error)

type Registry struct {
	mu      sync.Mutex
	entries map[int64]*registryEntry
}

type registryEntry struct {
	mu               sync.Mutex
	busy             bool
	nextGeneration   int64
	removeGeneration int64
	snapshot         ProcessSnapshot
	hasProcess       bool
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[int64]*registryEntry)}
}

func (r *Registry) WithOperation(instanceID int64, operation OperationFunc) (ProcessSnapshot, error) {
	entry := r.entryFor(instanceID)

	entry.mu.Lock()
	if entry.busy {
		entry.mu.Unlock()
		return ProcessSnapshot{}, ErrOperationInProgress
	}
	entry.busy = true
	entry.nextGeneration++
	generation := entry.nextGeneration
	removeGeneration := entry.removeGeneration
	entry.mu.Unlock()

	var snapshot ProcessSnapshot
	var err error
	defer func() {
		entry.mu.Lock()
		entry.busy = false
		entry.mu.Unlock()
	}()
	snapshot, err = operation(generation)

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err != nil {
		return ProcessSnapshot{}, err
	}
	if entry.removeGeneration != removeGeneration {
		return snapshot, nil
	}
	snapshot.InstanceID = instanceID
	snapshot.Generation = generation
	entry.snapshot = snapshot
	entry.hasProcess = true
	return snapshot, nil
}

func (r *Registry) Get(instanceID int64) (ProcessSnapshot, bool) {
	r.mu.Lock()
	entry := r.entries[instanceID]
	r.mu.Unlock()
	if entry == nil {
		return ProcessSnapshot{}, false
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if !entry.hasProcess {
		return ProcessSnapshot{}, false
	}
	return entry.snapshot, true
}

func (r *Registry) MarkExited(instanceID int64, generation int64, state State) error {
	r.mu.Lock()
	entry := r.entries[instanceID]
	r.mu.Unlock()
	if entry == nil {
		return nil
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.hasProcess && generation != entry.snapshot.Generation {
		return ErrStaleGeneration
	}
	if !entry.hasProcess {
		return nil
	}
	entry.snapshot.State = state
	return nil
}

func (r *Registry) Remove(instanceID int64) error {
	r.mu.Lock()
	entry := r.entries[instanceID]
	r.mu.Unlock()
	if entry == nil {
		return nil
	}

	entry.mu.Lock()
	entry.removeGeneration++
	entry.hasProcess = false
	entry.snapshot = ProcessSnapshot{}
	entry.mu.Unlock()
	return nil
}

func (r *Registry) entryFor(instanceID int64) *registryEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[instanceID]
	if entry == nil {
		entry = &registryEntry{}
		r.entries[instanceID] = entry
	}
	return entry
}
