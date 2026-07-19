package supervisor

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRegistryRejectsOverlappingOperationsForSameInstance(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})

	go func() {
		_, err := registry.WithOperation(42, func(generation int64) (ProcessSnapshot, error) {
			close(started)
			<-release
			return ProcessSnapshot{InstanceID: 42, Generation: generation, State: StateRunning, PID: 1001, StartedAt: time.Now()}, nil
		})
		if err != nil {
			t.Errorf("first WithOperation() error = %v", err)
		}
	}()

	<-started
	_, err := registry.WithOperation(42, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 42, Generation: generation, State: StateRunning}, nil
	})
	close(release)

	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("overlapping WithOperation() error = %v, want ErrOperationInProgress", err)
	}
}

func TestRegistryAllowsParallelOperationsAcrossDifferentInstances(t *testing.T) {
	registry := NewRegistry()
	started := make(chan int64, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup

	for _, instanceID := range []int64{1, 2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := registry.WithOperation(instanceID, func(generation int64) (ProcessSnapshot, error) {
				started <- instanceID
				<-release
				return ProcessSnapshot{InstanceID: instanceID, Generation: generation, State: StateRunning, PID: int(instanceID)}, nil
			})
			if err != nil {
				t.Errorf("WithOperation(%d) error = %v", instanceID, err)
			}
		}()
	}

	seen := map[int64]bool{<-started: true, <-started: true}
	if !seen[1] || !seen[2] {
		t.Fatalf("parallel operations started for instances = %v, want both 1 and 2", seen)
	}
	close(release)
	wg.Wait()
}

func TestRegistryIncrementsGenerationPerInstance(t *testing.T) {
	registry := NewRegistry()
	first, err := registry.WithOperation(7, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 7, Generation: generation, State: StateRunning}, nil
	})
	if err != nil {
		t.Fatalf("first WithOperation() error = %v", err)
	}
	second, err := registry.WithOperation(7, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 7, Generation: generation, State: StateRunning}, nil
	})
	if err != nil {
		t.Fatalf("second WithOperation() error = %v", err)
	}
	other, err := registry.WithOperation(8, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 8, Generation: generation, State: StateRunning}, nil
	})
	if err != nil {
		t.Fatalf("other WithOperation() error = %v", err)
	}

	if first.Generation != 1 || second.Generation != 2 || other.Generation != 1 {
		t.Fatalf("generations = first %d second %d other %d, want 1 2 1", first.Generation, second.Generation, other.Generation)
	}
}

func TestRegistryRejectsStaleExitUpdate(t *testing.T) {
	registry := NewRegistry()
	first, err := registry.WithOperation(9, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 9, Generation: generation, State: StateRunning, PID: 1001}, nil
	})
	if err != nil {
		t.Fatalf("first WithOperation() error = %v", err)
	}
	second, err := registry.WithOperation(9, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 9, Generation: generation, State: StateRunning, PID: 1002}, nil
	})
	if err != nil {
		t.Fatalf("second WithOperation() error = %v", err)
	}

	err = registry.MarkExited(9, first.Generation, StateFailed)
	if !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("MarkExited() error = %v, want ErrStaleGeneration", err)
	}
	snapshot, ok := registry.Get(9)
	if !ok {
		t.Fatal("Get() ok = false")
	}
	if snapshot.Generation != second.Generation || snapshot.PID != 1002 || snapshot.State != StateRunning {
		t.Fatalf("snapshot after stale exit = %+v, want newer running process", snapshot)
	}
}

func TestRegistryTransitionRejectsChangedStateForSameGeneration(t *testing.T) {
	registry := NewRegistry()
	starting, err := registry.Register(11, ProcessSnapshot{State: StateStarting, PID: 1001})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.MarkExited(11, starting.Generation, StateStopped); err != nil {
		t.Fatalf("MarkExited() error = %v", err)
	}

	running := starting
	running.State = StateRunning
	err = registry.Transition(11, starting.Generation, StateStarting, running)
	if !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("Transition() error = %v, want ErrStaleGeneration", err)
	}
	snapshot, ok := registry.Get(11)
	if !ok || snapshot.State != StateStopped || snapshot.Generation != starting.Generation {
		t.Fatalf("snapshot after rejected transition = %+v ok %v, want same generation stopped", snapshot, ok)
	}
}

func TestRegistryTransitionRejectsStaleGeneration(t *testing.T) {
	registry := NewRegistry()
	first, err := registry.Register(12, ProcessSnapshot{State: StateStarting, PID: 1001})
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := registry.MarkExited(12, first.Generation, StateStopped); err != nil {
		t.Fatalf("MarkExited() error = %v", err)
	}
	second, err := registry.Register(12, ProcessSnapshot{State: StateStarting, PID: 1002})
	if err != nil {
		t.Fatalf("second Register() error = %v", err)
	}

	running := first
	running.State = StateRunning
	err = registry.Transition(12, first.Generation, StateStarting, running)
	if !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("Transition() error = %v, want ErrStaleGeneration", err)
	}
	snapshot, ok := registry.Get(12)
	if !ok || snapshot.Generation != second.Generation || snapshot.PID != 1002 || snapshot.State != StateStarting {
		t.Fatalf("snapshot after stale transition = %+v ok %v, want second starting generation", snapshot, ok)
	}
}

func TestRegistryFailedOperationDoesNotInvalidateCurrentProcessExit(t *testing.T) {
	registry := NewRegistry()
	running, err := registry.WithOperation(10, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 10, Generation: generation, State: StateRunning, PID: 1001}, nil
	})
	if err != nil {
		t.Fatalf("running WithOperation() error = %v", err)
	}

	operationErr := errors.New("operation failed")
	if _, err := registry.WithOperation(10, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{}, operationErr
	}); !errors.Is(err, operationErr) {
		t.Fatalf("failed WithOperation() error = %v, want %v", err, operationErr)
	}

	if err := registry.MarkExited(10, running.Generation, StateStopped); err != nil {
		t.Fatalf("MarkExited() after failed operation error = %v", err)
	}
	snapshot, ok := registry.Get(10)
	if !ok {
		t.Fatal("Get() ok = false")
	}
	if snapshot.Generation != running.Generation || snapshot.State != StateStopped {
		t.Fatalf("snapshot after MarkExited() = %+v, want generation %d stopped", snapshot, running.Generation)
	}
}

func TestRegistryRemoveIsIdempotent(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Remove(404); err != nil {
		t.Fatalf("Remove() missing error = %v", err)
	}
	if _, err := registry.WithOperation(404, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 404, Generation: generation, State: StateRunning}, nil
	}); err != nil {
		t.Fatalf("WithOperation() error = %v", err)
	}
	if err := registry.Remove(404); err != nil {
		t.Fatalf("Remove() existing error = %v", err)
	}
	if err := registry.Remove(404); err != nil {
		t.Fatalf("Remove() second error = %v", err)
	}
	if _, ok := registry.Get(404); ok {
		t.Fatal("Get() ok = true after Remove")
	}
}

func TestRegistryRemovePreservesActiveOperationGate(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		_, err := registry.WithOperation(505, func(generation int64) (ProcessSnapshot, error) {
			close(started)
			<-release
			return ProcessSnapshot{InstanceID: 505, Generation: generation, State: StateRunning, PID: 1001}, nil
		})
		done <- err
	}()

	<-started
	if err := registry.Remove(505); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	operationRan := false
	_, err := registry.WithOperation(505, func(generation int64) (ProcessSnapshot, error) {
		operationRan = true
		return ProcessSnapshot{InstanceID: 505, Generation: generation, State: StateRunning, PID: 1002}, nil
	})
	close(release)

	if !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("overlapping WithOperation() after Remove error = %v, want ErrOperationInProgress", err)
	}
	if operationRan {
		t.Fatal("overlapping WithOperation() ran while first operation was active")
	}
	if err := <-done; err != nil {
		t.Fatalf("first WithOperation() error = %v", err)
	}
}

func TestRegistryRemoveDuringActiveOperationPreventsResurrection(t *testing.T) {
	registry := NewRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		_, err := registry.WithOperation(506, func(generation int64) (ProcessSnapshot, error) {
			close(started)
			<-release
			return ProcessSnapshot{InstanceID: 506, Generation: generation, State: StateRunning, PID: 1001}, nil
		})
		done <- err
	}()

	<-started
	if err := registry.Remove(506); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("WithOperation() error = %v", err)
	}
	if _, ok := registry.Get(506); ok {
		t.Fatal("Get() ok = true after Remove completed over active operation")
	}
}

func TestRegistryPanicDuringOperationClearsActiveGate(t *testing.T) {
	registry := NewRegistry()

	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("recovered panic = %v, want boom", recovered)
			}
		}()
		_, _ = registry.WithOperation(606, func(generation int64) (ProcessSnapshot, error) {
			panic("boom")
		})
	}()

	if _, err := registry.WithOperation(606, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 606, Generation: generation, State: StateRunning, PID: 1001}, nil
	}); err != nil {
		t.Fatalf("WithOperation() after panic error = %v", err)
	}
}

func TestRegistryStatusReadsAreRaceFreeSnapshots(t *testing.T) {
	registry := NewRegistry()
	if _, err := registry.WithOperation(55, func(generation int64) (ProcessSnapshot, error) {
		return ProcessSnapshot{InstanceID: 55, Generation: generation, State: StateRunning, PID: 1001, StartedAt: time.Now()}, nil
	}); err != nil {
		t.Fatalf("WithOperation() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snapshot, ok := registry.Get(55)
				if !ok {
					t.Errorf("worker %d Get() ok = false", worker)
					return
				}
				snapshot.PID = worker
				_ = snapshot
			}
		}(i)
	}

	for i := 0; i < 100; i++ {
		if err := registry.MarkExited(55, 1, StateStopped); err != nil {
			t.Fatalf("MarkExited() error = %v", err)
		}
	}
	wg.Wait()

	snapshot, ok := registry.Get(55)
	if !ok {
		t.Fatal("Get() ok = false")
	}
	if snapshot.PID != 1001 {
		t.Fatalf("snapshot PID = %d, want immutable internal PID 1001", snapshot.PID)
	}
}
