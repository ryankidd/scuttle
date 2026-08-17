// Package scuttle provides a small filesystem abstraction for programs that
// need to reason about crash safety.
//
// A durable write path — a write-ahead log, an atomically-replaced config
// file, a compacting store — depends on a handful of filesystem operations
// behaving the way the program expects: a write reaches the file, an fsync
// pushes it to stable storage, a rename swaps a new file in for an old one
// atomically. That path is hard to test precisely, because the failures that
// matter (a partial write, an fsync that never returned before the machine
// died, a rename that landed but whose directory entry was never persisted)
// almost never happen on a healthy disk during a unit test.
//
// scuttle addresses this by routing every filesystem operation through the FS
// and File interfaces defined here, rather than calling the os package
// directly. Real code runs against OSFS, an unadorned pass-through to the
// operating system. Tests can run the same code against an in-memory
// implementation, which is fast, isolated, and — because every operation is a
// plain method call — a natural place to interpose deterministic faults.
package scuttle

// File is an open file handle returned by an FS. It exposes exactly the
// operations a durable write path performs on a file it owns: sequential
// reading and writing, an explicit Sync to force buffered data to stable
// storage, and Close.
//
// The interface is deliberately narrow. A consumer that only ever appends and
// replays records needs nothing more, and keeping the surface small keeps the
// alternative implementations honest and keeps later fault interposition
// tractable — there are only four places an error can be injected.
type File interface {
	// Read reads up to len(p) bytes into p, following the usual io.Reader
	// contract. It returns io.EOF when the file is exhausted.
	Read(p []byte) (int, error)

	// Write writes len(p) bytes to the file. Like io.Writer, it returns a
	// non-nil error if it wrote fewer than len(p) bytes.
	Write(p []byte) (int, error)

	// Sync forces any buffered writes for this file through to stable
	// storage. Until Sync returns nil, a write that Write reported as
	// successful may still be lost to a power failure or kernel panic.
	Sync() error

	// Close releases the handle. Close does not imply Sync; a caller that
	// needs durability must Sync before Close.
	Close() error
}

// FS abstracts the filesystem operations a crash-safe program depends on. All
// names are interpreted by the implementation; an OS-backed FS may root them
// under a base directory, while an in-memory FS treats them as opaque keys.
//
// Every method returns an error, and implementations are expected to surface
// the same error conditions the operating system would — a missing file on
// Open, a name collision on Rename's target directory, and so on. That
// uniformity is what lets an alternative implementation stand in for the real
// filesystem without the code under test being able to tell the difference.
type FS interface {
	// Open opens an existing file for reading. It fails if the file does
	// not exist.
	Open(name string) (File, error)

	// Create creates or truncates a file and opens it for writing. If the
	// file already exists it is truncated to zero length.
	Create(name string) (File, error)

	// Remove deletes a file. It fails if the file does not exist.
	Remove(name string) error

	// Rename atomically replaces newname with the file currently at
	// oldname. On a real filesystem this is the primitive an atomic update
	// is built on: write a temporary file, Sync it, then Rename it over the
	// destination.
	Rename(oldname, newname string) error
}
