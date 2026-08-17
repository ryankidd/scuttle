package scuttle

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"testing"
)

// implementations returns each FS under test, freshly constructed. Every test
// runs against all of them so the two implementations are held to the same
// observable behaviour.
func implementations(t *testing.T) map[string]FS {
	t.Helper()
	return map[string]FS{
		"osfs":  NewOSFS(t.TempDir()),
		"memfs": NewMemFS(),
	}
}

// readAll drains a File to EOF and returns its contents. A File satisfies
// io.Reader, so it can be handed straight to io.ReadAll.
func readAll(t *testing.T, f File) []byte {
	t.Helper()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return data
}

// TestWriteSyncReopenRead is the core durability round-trip: write bytes to a
// file, force them to storage, close it, reopen it by name, and read the same
// bytes back.
func TestWriteSyncReopenRead(t *testing.T) {
	want := []byte("the quick brown fox\njumped over\n")

	for name, filesystem := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			w, err := filesystem.Create("log")
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if n, err := w.Write(want); err != nil || n != len(want) {
				t.Fatalf("write: n=%d err=%v", n, err)
			}
			if err := w.Sync(); err != nil {
				t.Fatalf("sync: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			r, err := filesystem.Open("log")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer r.Close()

			if got := readAll(t, r); !bytes.Equal(got, want) {
				t.Fatalf("round-trip mismatch: got %q want %q", got, want)
			}
		})
	}
}

// TestWriteIsAppending verifies that successive writes to one handle accumulate
// in order rather than overwriting.
func TestWriteIsAppending(t *testing.T) {
	for name, filesystem := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			w, err := filesystem.Create("log")
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			for _, part := range []string{"one\n", "two\n", "three\n"} {
				if _, err := w.Write([]byte(part)); err != nil {
					t.Fatalf("write %q: %v", part, err)
				}
			}
			if err := w.Sync(); err != nil {
				t.Fatalf("sync: %v", err)
			}
			w.Close()

			r, err := filesystem.Open("log")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer r.Close()

			want := "one\ntwo\nthree\n"
			if got := readAll(t, r); string(got) != want {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
}

// TestAtomicRename exercises the write-temp, sync, rename-into-place sequence a
// crash-safe update is built on, and checks the destination sees the new
// contents after the rename.
func TestAtomicRename(t *testing.T) {
	for name, filesystem := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			want := []byte("committed state")

			w, err := filesystem.Create("state.tmp")
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if _, err := w.Write(want); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := w.Sync(); err != nil {
				t.Fatalf("sync: %v", err)
			}
			w.Close()

			if err := filesystem.Rename("state.tmp", "state"); err != nil {
				t.Fatalf("rename: %v", err)
			}

			if _, err := filesystem.Open("state.tmp"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("source still present after rename: err=%v", err)
			}

			r, err := filesystem.Open("state")
			if err != nil {
				t.Fatalf("open destination: %v", err)
			}
			defer r.Close()

			if got := readAll(t, r); !bytes.Equal(got, want) {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
}

// TestRemove checks that a removed file no longer opens, and that the failure
// is reported as a not-exist error across implementations.
func TestRemove(t *testing.T) {
	for name, filesystem := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			w, err := filesystem.Create("scratch")
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			w.Close()

			if err := filesystem.Remove("scratch"); err != nil {
				t.Fatalf("remove: %v", err)
			}
			if _, err := filesystem.Open("scratch"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("expected not-exist after remove, got %v", err)
			}
		})
	}
}

// TestOpenMissing checks that opening an absent file fails uniformly.
func TestOpenMissing(t *testing.T) {
	for name, filesystem := range implementations(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := filesystem.Open("nope"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("expected not-exist, got %v", err)
			}
		})
	}
}
