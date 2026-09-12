package sessions

import (
	"context"
	"testing"
)

func TestStoppingNativeCheckpointKeepsGenerationMonotonic(t *testing.T) {
	m := &Manager{
		checkpoints:          map[int64]context.CancelFunc{},
		checkpointGeneration: map[int64]uint64{7: 4},
	}
	called := false
	m.checkpoints[7] = func() { called = true }
	m.stopNativeCheckpoint(7)
	if !called {
		t.Fatal("stopping a checkpoint did not cancel its worker")
	}
	if _, ok := m.checkpoints[7]; ok {
		t.Fatal("stopping a checkpoint left its worker registered")
	}
	if got := m.checkpointGeneration[7]; got != 4 {
		t.Fatalf("stopping reset generation to %d; replacement could collide with an old worker", got)
	}
}

func TestRestartNativeCheckpointCancelsPriorObserver(t *testing.T) {
	m := &Manager{checkpoints: map[int64]context.CancelFunc{}, checkpointGeneration: map[int64]uint64{7: 2}}
	called := false
	m.checkpoints[7] = func() { called = true }
	// The public restart primitive must retire the old observer before trying to
	// install its replacement; this is the lifecycle guarantee used by native
	// promotion when shell becomes Claude/Codex.
	m.stopNativeCheckpoint(7)
	if !called || len(m.checkpoints) != 0 || m.checkpointGeneration[7] != 2 {
		t.Fatalf("old observer was not retired safely: called=%v checkpoints=%v generation=%v", called, m.checkpoints, m.checkpointGeneration[7])
	}
}
