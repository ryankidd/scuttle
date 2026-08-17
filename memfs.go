package scuttle

import (
	"io"
	"sync"
)

// Compile-time check that MemFS satisfies the FS interface.
var _ FS = (*MemFS)(nil)

// MemFS is an in-memory FS. Files live as byte slices in a map keyed by name;
// no data touches the disk. It is fast, needs no cleanup, and is fully
// isolated from other instances, which makes it convenient for tests that
// exercise a durable write path without the cost and noise of real I/O.
//
// Because every operation is an ordinary method call over data the FS owns
// outright, MemFS is also the natural seam for deterministic fault injection:
// a later layer can wrap it to fail a chosen Write, Sync, or Rename, or to
// model a crash by discarding writes that were never Synced. Nothing here does
// that yet — MemFS on its own is a faithful, well-behaved filesystem.
//
// A MemFS is safe for concurrent use. Its zero value is not usable; construct
// one with NewMemFS.
type MemFS struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewMemFS returns an empty in-memory filesystem.
func NewMemFS() *MemFS {
	return &MemFS{files: make(map[string][]byte)}
}

// Open opens an existing file for reading. The returned handle reads a
// snapshot of the file's contents taken at Open time.
func (fs *MemFS) Open(name string) (File, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, ok := fs.files[name]
	if !ok {
		return nil, &fileError{op: "open", name: name, err: errNotExist}
	}
	snapshot := make([]byte, len(data))
	copy(snapshot, data)
	return &memFile{fs: fs, name: name, read: snapshot}, nil
}

// Create creates or truncates a file and opens it for writing. Bytes written
// through the returned handle are appended to the file and become visible to
// subsequent Opens.
func (fs *MemFS) Create(name string) (File, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.files[name] = []byte{}
	return &memFile{fs: fs, name: name, write: true}, nil
}

// Remove deletes a file.
func (fs *MemFS) Remove(name string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if _, ok := fs.files[name]; !ok {
		return &fileError{op: "remove", name: name, err: errNotExist}
	}
	delete(fs.files, name)
	return nil
}

// Rename replaces newname with the file at oldname. It fails if oldname does
// not exist.
func (fs *MemFS) Rename(oldname, newname string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, ok := fs.files[oldname]
	if !ok {
		return &fileError{op: "rename", name: oldname, err: errNotExist}
	}
	fs.files[newname] = data
	delete(fs.files, oldname)
	return nil
}

// append writes p to the named file's stored contents under the FS lock.
func (fs *MemFS) append(name string, p []byte) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[name] = append(fs.files[name], p...)
}

// memFile is a handle onto a file in a MemFS. A handle is opened either for
// reading or for writing, matching the FS method that produced it.
type memFile struct {
	fs     *MemFS
	name   string
	write  bool
	read   []byte // remaining bytes for a read handle
	closed bool
}

// Read consumes bytes from the snapshot captured at Open.
func (f *memFile) Read(p []byte) (int, error) {
	if f.closed {
		return 0, &fileError{op: "read", name: f.name, err: errClosed}
	}
	if f.write {
		return 0, &fileError{op: "read", name: f.name, err: errWrongMode}
	}
	if len(f.read) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.read)
	f.read = f.read[n:]
	return n, nil
}

// Write appends bytes to the file backing this handle.
func (f *memFile) Write(p []byte) (int, error) {
	if f.closed {
		return 0, &fileError{op: "write", name: f.name, err: errClosed}
	}
	if !f.write {
		return 0, &fileError{op: "write", name: f.name, err: errWrongMode}
	}
	f.fs.append(f.name, p)
	return len(p), nil
}

// Sync is a no-op: an in-memory file has no stable storage below it to flush
// to. It is retained so code written against the FS interface exercises the
// same call sequence it would against a real disk.
func (f *memFile) Sync() error {
	if f.closed {
		return &fileError{op: "sync", name: f.name, err: errClosed}
	}
	return nil
}

// Close releases the handle.
func (f *memFile) Close() error {
	if f.closed {
		return &fileError{op: "close", name: f.name, err: errClosed}
	}
	f.closed = true
	return nil
}
