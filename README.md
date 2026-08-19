# scuttle

A deterministic fault-injection harness for testing crash and I/O-failure recovery.

Durable software — a write-ahead log, an atomically-replaced file, a
compacting store — earns its durability from a small number of filesystem
operations behaving exactly as expected: a write reaches the file, an `fsync`
pushes it to stable storage, a `rename` swaps a new file in for an old one.
The failures that break those guarantees — a partial write, an `fsync` that
never returned before the machine died, a rename whose directory entry was
never persisted — almost never occur on a healthy disk during a test run, so
the recovery code that handles them is the code least likely to have been
exercised.

`scuttle` gives that code a filesystem it can be run against. Instead of
calling the `os` package directly, a program depends on the `FS` and `File`
interfaces. In production it uses `OSFS`, a plain pass-through to the operating
system. In tests it uses an in-memory filesystem that is fast, isolated, and
built to have deterministic faults interposed on it.

## Interfaces

```go
type FS interface {
    Open(name string) (File, error)
    Create(name string) (File, error)
    Remove(name string) error
    Rename(oldname, newname string) error
}

type File interface {
    Read(p []byte) (int, error)
    Write(p []byte) (int, error)
    Sync() error
    Close() error
}
```

Two implementations satisfy them:

- **`OSFS`** — backed by the real filesystem, rooted at a base directory.
  Construct it with `NewOSFS(dir)`. This is what production code runs against.
- **`MemFS`** — an in-memory filesystem holding each file as a byte slice.
  Construct it with `NewMemFS()`. It is safe for concurrent use and leaves
  nothing behind to clean up.

Both return `fs.ErrNotExist` for a missing file, so callers test for it the
same way regardless of which implementation they hold.

## Fault Injection

`FaultFS` wraps any `FS` and injects faults into the operations that pass
through it. Every fault-eligible operation — `Open`, `Create`, `Remove`,
`Rename` on the `FS`, and `Write` and `Sync` on an open file — consults a
single seeded pseudo-random stream: with the configured probability the
operation fails with `ErrInjected` instead of reaching the underlying `FS`,
and otherwise it is forwarded untouched.

Because every decision is drawn from one `rand.Rand` seeded at construction,
replaying a workload with the same seed injects exactly the same faults at
exactly the same points. A test that reproduces a failure need only record the
seed; a bug found on a CI runner replays bit-for-bit on a laptop.

```go
fs := scuttle.NewFaultFS(scuttle.NewMemFS(), seed, 0.3)
```

`NewFaultFS(fs, seed, prob)` takes the underlying `FS`, an `int64` seed, and
a fault probability in `[0, 1]`. Two `FaultFS` values built with the same seed
and driven through the same sequence of operations inject an identical
sequence of faults. The `Faults()` method returns a copy of the injected
faults as `[]Fault`, each recording the ordinal, operation, and file name, so
a test can assert replay fidelity:

```go
first  := runWorkload(seed, 0.3)
second := runWorkload(seed, 0.3)
if !reflect.DeepEqual(first, second) {
    t.Fatal("same seed produced different faults")
}
```

A `FaultFS` is safe for concurrent use, but deterministic replay assumes a
deterministic sequence of operations — a workload that issues operations from
several goroutines gives up the ordering guarantee.

## Usage

Write code against the interface and pick the implementation at the edge:

```go
func writeConfig(filesystem scuttle.FS, data []byte) error {
    f, err := filesystem.Create("config.tmp")
    if err != nil {
        return err
    }
    if _, err := f.Write(data); err != nil {
        f.Close()
        return err
    }
    if err := f.Sync(); err != nil {
        f.Close()
        return err
    }
    if err := f.Close(); err != nil {
        return err
    }
    // Atomically replace the live file with the fully-written temporary.
    return filesystem.Rename("config.tmp", "config")
}
```

In production:

```go
fs := scuttle.NewOSFS("/var/lib/myapp")
err := writeConfig(fs, payload)
```

In a test:

```go
fs := scuttle.NewMemFS()
err := writeConfig(fs, payload)
```

The same `writeConfig` runs unchanged against either.

## Testing

```
go test ./...
```

The suite runs every test against both implementations through the shared
interface, so the two behave identically on the operations that matter:
write-sync-reopen-read round-trips, appending writes, atomic rename, removal,
and missing-file errors. The fault-injection tests verify that the same seed
replays the same sequence of faults, that different seeds diverge, and that
the extremes of probability (0 and 1) behave correctly. Run it under the race
detector with `go test -race ./...`.

## Requirements

Go 1.22 or newer. There are no external dependencies.
