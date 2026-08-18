package scuttle

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

// runWorkload drives a fixed sequence of filesystem operations through a
// FaultFS wrapping a fresh MemFS, and returns the faults that were injected.
// The sequence is the same on every call, so any difference in the returned
// faults is down to the seed alone. Errors are deliberately ignored: the
// workload issues the same operations whatever the schedule decides, which is
// what keeps the decision stream aligned across runs.
func runWorkload(seed int64, prob float64) []Fault {
	fsys := NewFaultFS(NewMemFS(), seed, prob)

	for i := 0; i < 8; i++ {
		f, err := fsys.Create("wal")
		if err == nil {
			_, _ = f.Write([]byte("record-a"))
			_, _ = f.Write([]byte("record-b"))
			_ = f.Sync()
			_ = f.Close()
		}
		_ = fsys.Rename("wal", "wal.old")
		if g, err := fsys.Open("wal.old"); err == nil {
			_, _ = io.ReadAll(g)
			_ = g.Close()
		}
		_ = fsys.Remove("wal.old")
	}

	return fsys.Faults()
}

func TestSameSeedReplaysSameFaults(t *testing.T) {
	first := runWorkload(1234, 0.3)
	second := runWorkload(1234, 0.3)

	if len(first) == 0 {
		t.Fatal("expected the schedule to inject at least one fault")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same seed produced different faults:\n first=%v\nsecond=%v", first, second)
	}
}

func TestDifferentSeedsDiverge(t *testing.T) {
	a := runWorkload(1, 0.3)
	b := runWorkload(2, 0.3)

	if reflect.DeepEqual(a, b) {
		t.Fatalf("different seeds produced identical faults: %v", a)
	}
}

func TestZeroProbabilityInjectsNothing(t *testing.T) {
	fsys := NewFaultFS(NewMemFS(), 99, 0)

	f, err := fsys.Create("wal")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Write([]byte("payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if faults := fsys.Faults(); len(faults) != 0 {
		t.Fatalf("prob 0 injected faults: %v", faults)
	}

	// The write must have reached the underlying FS untouched.
	g, err := fsys.Open("wal")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, _ := io.ReadAll(g)
	if string(got) != "payload" {
		t.Fatalf("got %q, want %q", got, "payload")
	}
}

func TestCertainProbabilityFailsFirstOperation(t *testing.T) {
	fsys := NewFaultFS(NewMemFS(), 7, 1)

	_, err := fsys.Create("wal")
	if !errors.Is(err, ErrInjected) {
		t.Fatalf("got %v, want ErrInjected", err)
	}

	faults := fsys.Faults()
	if len(faults) != 1 {
		t.Fatalf("got %d faults, want 1", len(faults))
	}
	if faults[0].Op != "create" || faults[0].Name != "wal" || faults[0].Seq != 1 {
		t.Fatalf("unexpected fault: %+v", faults[0])
	}
}
