package server

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

// GCSStore is a Store over one bucket. Objects are immutable, so "tag in
// place" is Get → tag locally → Put with a generation precondition.
type GCSStore struct{ Bucket *storage.BucketHandle }

func NewGCSStore(ctx context.Context, bucket string) (GCSStore, error) {
	c, err := storage.NewClient(ctx)
	if err != nil {
		return GCSStore{}, err
	}
	return GCSStore{c.Bucket(bucket)}, nil
}

func (s GCSStore) Stat(ctx context.Context, name string) (int64, int64, error) {
	a, err := s.Bucket.Object(name).Attrs(ctx)
	if err != nil {
		return 0, 0, gcsErr(err)
	}
	return a.Size, a.Generation, nil
}

func (s GCSStore) Get(ctx context.Context, name, local string) (int64, error) {
	r, err := s.Bucket.Object(name).NewReader(ctx)
	if err != nil {
		return 0, gcsErr(err)
	}
	defer r.Close()
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(local)
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return 0, err
	}
	return r.Attrs.Generation, f.Close()
}

func (s GCSStore) Put(ctx context.Context, name, local string, ifGen int64) (int64, error) {
	obj := s.Bucket.Object(name)
	switch ifGen {
	case Any:
	case 0:
		obj = obj.If(storage.Conditions{DoesNotExist: true})
	default:
		obj = obj.If(storage.Conditions{GenerationMatch: ifGen})
	}
	f, err := os.Open(local)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := obj.NewWriter(ctx)
	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return 0, gcsErr(err)
	}
	if err := w.Close(); err != nil {
		return 0, gcsErr(err)
	}
	return w.Attrs().Generation, nil
}

func (s GCSStore) List(ctx context.Context, dir string) ([]string, error) {
	prefix := strings.TrimSuffix(dir, "/") + "/"
	it := s.Bucket.Objects(ctx, &storage.Query{Prefix: prefix, Delimiter: "/"})
	var out []string
	for {
		a, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return nil, gcsErr(err)
		}
		if a.Name != "" && !strings.HasSuffix(a.Name, "/") {
			out = append(out, a.Name)
		}
	}
}

// gcsErr maps GCS errors onto the ones the server understands.
func gcsErr(err error) error {
	var ge *googleapi.Error
	switch {
	case errors.Is(err, storage.ErrObjectNotExist):
		return fs.ErrNotExist
	case errors.As(err, &ge) && ge.Code == http.StatusPreconditionFailed:
		return ErrPrecondition
	}
	return err
}
