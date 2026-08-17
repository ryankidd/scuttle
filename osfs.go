package scuttle

import (
	"os"
	"path/filepath"
)

// Compile-time check that OSFS satisfies the FS interface.
var _ FS = (*OSFS)(nil)

// OSFS is an FS backed directly by the operating system's filesystem. It adds
// nothing of its own: each method is a thin pass-through to the corresponding
// os package call, and the File it returns is an *os.File. This is the
// implementation production code runs against.
//
// Names are resolved relative to a base directory fixed at construction, so a
// single OSFS is confined to one subtree. This keeps callers from reaching
// outside the directory they were handed and makes an OSFS rooted at a
// temporary directory a self-contained sandbox for tests.
type OSFS struct {
	root string
	perm os.FileMode
}

// NewOSFS returns an OSFS whose operations are rooted at dir. The directory is
// not created; the caller is responsible for its existence, exactly as it
// would be when calling the os package directly. Files created through the
// returned FS are given mode 0o644.
func NewOSFS(dir string) *OSFS {
	return &OSFS{root: dir, perm: 0o644}
}

// path resolves a caller-supplied name against the FS root.
func (fs *OSFS) path(name string) string {
	return filepath.Join(fs.root, name)
}

// Open opens an existing file for reading.
func (fs *OSFS) Open(name string) (File, error) {
	return os.Open(fs.path(name))
}

// Create creates or truncates a file and opens it for writing.
func (fs *OSFS) Create(name string) (File, error) {
	return os.OpenFile(fs.path(name), os.O_RDWR|os.O_CREATE|os.O_TRUNC, fs.perm)
}

// Remove deletes a file.
func (fs *OSFS) Remove(name string) error {
	return os.Remove(fs.path(name))
}

// Rename atomically replaces newname with oldname.
func (fs *OSFS) Rename(oldname, newname string) error {
	return os.Rename(fs.path(oldname), fs.path(newname))
}
