package scuttle

import (
	"errors"
	"io/fs"
)

// Errors reported by the in-memory FS. errNotExist aliases the standard
// library's fs.ErrNotExist so that callers can test for a missing file with
// errors.Is(err, fs.ErrNotExist) regardless of which FS implementation raised
// it — an OS-backed open and an in-memory open fail the same way.
var (
	errNotExist  = fs.ErrNotExist
	errClosed    = errors.New("file already closed")
	errWrongMode = errors.New("file opened in the wrong mode")
)

// fileError attaches the operation and file name to an underlying error,
// mirroring the shape of the *fs.PathError values the os package returns.
type fileError struct {
	op   string
	name string
	err  error
}

func (e *fileError) Error() string {
	return e.op + " " + e.name + ": " + e.err.Error()
}

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (e *fileError) Unwrap() error {
	return e.err
}
