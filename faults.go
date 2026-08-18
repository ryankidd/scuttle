package scuttle

import (
	"errors"
	"math/rand"
	"sync"
)

// ErrInjected is the error a FaultFS returns in place of performing an
// operation the fault schedule chose to fail. Callers test for it with
// errors.Is(err, ErrInjected); it is wrapped in a *fileError so the operation
// and file name travel with it, matching the shape of every other error the
// package returns.
var ErrInjected = errors.New("injected fault")

// Compile-time check that FaultFS satisfies the FS interface.
var _ FS = (*FaultFS)(nil)

// FaultFS wraps an FS and injects faults into the operations that pass through
// it. Every fault-eligible operation — Open, Create, Remove, Rename on the FS,
// and Write and Sync on an open file — consults a single seeded stream of
// pseudo-random decisions: with the configured probability the operation fails
// with ErrInjected instead of reaching the underlying FS, and otherwise it is
// forwarded untouched.
//
// The decision stream is the whole point. Because every decision is drawn from
// one rand.Rand seeded at construction, and because a given workload issues the
// same operations in the same order on every run, replaying that workload with
// the same seed injects exactly the same faults at exactly the same points. A
// test that reproduces a failure need only record the seed; a bug found on a
// CI runner replays bit-for-bit on a laptop.
//
// The zero value is not usable; construct one with NewFaultFS. A FaultFS is
// safe for concurrent use, though a workload that issues operations from
// several goroutines gives up the ordering guarantee that makes replay useful —
// deterministic replay assumes a deterministic sequence of operations.
type FaultFS struct {
	fs   FS
	prob float64

	mu  sync.Mutex
	rng *rand.Rand
	seq int
	log []Fault
}

// Fault records a single injected failure: the ordinal of the decision point
// within the run, the operation that was failed, and the file it named. The
// slice of Faults from one run is what a replay is checked against.
type Fault struct {
	// Seq is the 1-based index of this decision point in the run's stream of
	// fault-eligible operations. It advances on every decision, injected or
	// not, so it doubles as the position in the pseudo-random stream.
	Seq int
	// Op is the operation that was failed: "open", "create", "remove",
	// "rename", "write", or "sync".
	Op string
	// Name is the file the operation named.
	Name string
}

// NewFaultFS wraps fs so that each fault-eligible operation fails with
// probability prob (in [0, 1]), with all decisions drawn from a stream seeded
// by seed. Two FaultFS values built with the same seed and driven through the
// same sequence of operations inject an identical sequence of faults.
func NewFaultFS(fs FS, seed int64, prob float64) *FaultFS {
	return &FaultFS{
		fs:   fs,
		prob: prob,
		rng:  rand.New(rand.NewSource(seed)),
	}
}

// Faults returns a copy of the faults injected so far, in the order they were
// injected.
func (f *FaultFS) Faults() []Fault {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Fault, len(f.log))
	copy(out, f.log)
	return out
}

// decide advances the seeded stream by one draw and reports whether this
// operation should be failed, recording the fault when it is. Every
// fault-eligible operation calls decide exactly once, so the stream stays in
// lockstep with the sequence of operations across runs.
func (f *FaultFS) decide(op, name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	if f.rng.Float64() < f.prob {
		f.log = append(f.log, Fault{Seq: f.seq, Op: op, Name: name})
		return true
	}
	return false
}

// Open opens name for reading unless the schedule fails the operation.
func (f *FaultFS) Open(name string) (File, error) {
	if f.decide("open", name) {
		return nil, &fileError{op: "open", name: name, err: ErrInjected}
	}
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &faultFile{fs: f, name: name, File: file}, nil
}

// Create creates or truncates name unless the schedule fails the operation.
func (f *FaultFS) Create(name string) (File, error) {
	if f.decide("create", name) {
		return nil, &fileError{op: "create", name: name, err: ErrInjected}
	}
	file, err := f.fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &faultFile{fs: f, name: name, File: file}, nil
}

// Remove deletes name unless the schedule fails the operation.
func (f *FaultFS) Remove(name string) error {
	if f.decide("remove", name) {
		return &fileError{op: "remove", name: name, err: ErrInjected}
	}
	return f.fs.Remove(name)
}

// Rename replaces newname with oldname unless the schedule fails the
// operation. A failed Rename does not touch the underlying FS, modelling a
// rename that never took effect.
func (f *FaultFS) Rename(oldname, newname string) error {
	if f.decide("rename", oldname) {
		return &fileError{op: "rename", name: oldname, err: ErrInjected}
	}
	return f.fs.Rename(oldname, newname)
}

// faultFile wraps a File so that its durability-relevant operations — Write and
// Sync — pass through the same seeded schedule as the FS-level operations. Read
// and Close are forwarded unchanged by the embedded File.
type faultFile struct {
	File
	fs   *FaultFS
	name string
}

// Write forwards to the underlying file unless the schedule fails the
// operation, in which case nothing is written and ErrInjected is returned.
func (f *faultFile) Write(p []byte) (int, error) {
	if f.fs.decide("write", f.name) {
		return 0, &fileError{op: "write", name: f.name, err: ErrInjected}
	}
	return f.File.Write(p)
}

// Sync forwards to the underlying file unless the schedule fails the
// operation. A failed Sync models an fsync that never reached stable storage:
// the preceding writes may or may not survive a crash.
func (f *faultFile) Sync() error {
	if f.fs.decide("sync", f.name) {
		return &fileError{op: "sync", name: f.name, err: ErrInjected}
	}
	return f.File.Sync()
}
