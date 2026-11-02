// Package server is the HTTP API over the id3 package: it pulls files from a
// Store into a temp dir, runs the same pipeline the CLI runs, and puts the
// changed files back.
package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrPrecondition means the object changed since it was read.
var ErrPrecondition = errors.New("object changed since it was read")

// Any is the Put precondition meaning "overwrite whatever is there".
const Any int64 = -1

// Store is object storage as the server needs it. Names are slash paths
// relative to the bucket or root. Missing objects return fs.ErrNotExist.
type Store interface {
	Stat(ctx context.Context, name string) (size, gen int64, err error)
	Get(ctx context.Context, name, local string) (gen int64, err error)
	// Put uploads local as name if the current generation is ifGen
	// (0 = must not exist, Any = no check) and returns the new generation.
	Put(ctx context.Context, name, local string, ifGen int64) (gen int64, err error)
	// List returns object names directly inside dir (not deeper).
	List(ctx context.Context, dir string) ([]string, error)
}

// DirStore is a Store over a local folder — tests, and running the API
// against a drive. Generation is the file's mtime in nanoseconds.
type DirStore struct{ Root string }

func (s DirStore) full(name string) string { return filepath.Join(s.Root, filepath.FromSlash(name)) }

func (s DirStore) Stat(_ context.Context, name string) (int64, int64, error) {
	fi, err := os.Stat(s.full(name))
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), fi.ModTime().UnixNano(), nil
}

func (s DirStore) Get(ctx context.Context, name, local string) (int64, error) {
	_, gen, err := s.Stat(ctx, name)
	if err != nil {
		return 0, err
	}
	return gen, copyFile(s.full(name), local)
}

func (s DirStore) Put(ctx context.Context, name, local string, ifGen int64) (int64, error) {
	_, gen, err := s.Stat(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		gen = 0
	case err != nil:
		return 0, err
	}
	if ifGen != Any && ifGen != gen {
		return 0, ErrPrecondition
	}
	dst := s.full(name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp := dst + ".tmp-put"
	if err := copyFile(local, tmp); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	_, gen, err = s.Stat(ctx, name)
	return gen, err
}

func (s DirStore) List(_ context.Context, dir string) ([]string, error) {
	entries, err := os.ReadDir(s.full(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, path.Join(dir, e.Name()))
		}
	}
	return out, err
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
