package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/captainjspace/band-magic-id3/id3"
)

// Server is the HTTP API in API.md.
type Server struct {
	Store   Store
	Prefix  string // if set, every path must start with it
	MaxSize int64  // bytes; larger files get 413
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("POST /v1/merge", handle(s.merge))
	mux.HandleFunc("POST /v1/read", handle(s.read))
	mux.HandleFunc("POST /v1/tag", handle(s.tag))
	return mux
}

type httpError struct {
	code int
	msg  string
}

func (e httpError) Error() string { return e.msg }

func bad(format string, a ...any) error {
	return httpError{http.StatusBadRequest, fmt.Sprintf(format, a...)}
}

func status(err error) int {
	var he httpError
	switch {
	case errors.As(err, &he):
		return he.code
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, ErrPrecondition):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// handle decodes the JSON body into Req, runs f, and writes JSON or {error}.
func handle[Req any](f func(context.Context, Req) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20))
		dec.DisallowUnknownFields()
		out, err := any(nil), dec.Decode(&req)
		if err != nil {
			err = bad("bad request body: %v", err)
		} else {
			out, err = f(r.Context(), req)
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(status(err))
			out = map[string]string{"error": err.Error()}
		}
		json.NewEncoder(w).Encode(out)
	}
}

type mergeReq struct {
	Layers []map[string]any `json:"layers"`
}

func (s *Server) merge(_ context.Context, req mergeReq) (any, error) {
	d, err := id3.ResolveLayers(req.Layers...)
	if err != nil {
		return nil, bad("layers: %v", err)
	}
	return map[string]id3.Doc{"doc": d}, nil
}

type readReq struct {
	Path   string `json:"path"`
	Images bool   `json:"images"`
}

func (s *Server) read(ctx context.Context, req readReq) (any, error) {
	if err := s.checkAudio(req.Path); err != nil {
		return nil, err
	}
	w, err := s.newWork()
	if err != nil {
		return nil, err
	}
	defer w.close()
	gen, err := s.fetchAudio(ctx, w, req.Path)
	if err != nil {
		return nil, err
	}
	d, err := id3.Read(w.local(req.Path))
	if err != nil {
		return nil, err
	}
	if !req.Images {
		d = noImages(d)
	}
	return map[string]any{"path": req.Path, "format": format(req.Path), "generation": gen, "doc": d}, nil
}

type tagReq struct {
	Path   string           `json:"path"`
	Layers []map[string]any `json:"layers"`
	Doc    map[string]any   `json:"doc"`
	Cover  string           `json:"cover"`
	Lyrics string           `json:"lyrics"`
	M4A    *bool            `json:"m4a"`
	Keep   bool             `json:"keep"`
	DryRun bool             `json:"dry_run"`
}

type coverRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type fileResult struct {
	Path         string    `json:"path"`
	Format       string    `json:"format"`
	Generation   int64     `json:"generation"`
	Before       id3.Doc   `json:"before"`
	Applied      id3.Doc   `json:"applied"`
	ReadBack     id3.Doc   `json:"read_back"`
	Cover        *coverRef `json:"cover"`
	Lyrics       string    `json:"lyrics"`
	ReadBackPath string    `json:"read_back_path,omitempty"`
	Archive      string    `json:"archive,omitempty"`
}

func (s *Server) tag(ctx context.Context, req tagReq) (any, error) {
	if err := s.checkAudio(req.Path); err != nil {
		return nil, err
	}
	layers := req.Layers
	if req.Doc != nil {
		layers = []map[string]any{req.Doc}
	}
	if len(layers) == 0 {
		return nil, bad("give layers or doc")
	}
	doc, err := id3.ResolveLayers(layers...)
	if err != nil {
		return nil, bad("layers: %v", err)
	}

	w, err := s.newWork()
	if err != nil {
		return nil, err
	}
	defer w.close()

	// The file, plus the .m4a beside an .mp3 when asked (default yes).
	files := []string{req.Path}
	if sib := strings.TrimSuffix(req.Path, path.Ext(req.Path)) + ".m4a"; format(req.Path) == "mp3" && (req.M4A == nil || *req.M4A) {
		if _, _, err := s.Store.Stat(ctx, sib); err == nil {
			files = append(files, sib)
		}
	}
	gens := map[string]int64{}
	for _, f := range files {
		if gens[f], err = s.fetchAudio(ctx, w, f); err != nil {
			return nil, err
		}
	}

	opt := id3.Options{Keep: req.Keep, Lookup: id3.Lookup{DryRun: req.DryRun}}
	dir := path.Dir(req.Path)
	if opt.Cover, err = s.fetchAsset(ctx, w, req.Cover, []string{dir + "/covers", dir}, ".jpg", ".jpeg", ".png"); err != nil {
		return nil, err
	}
	if opt.Lyrics, err = s.fetchAsset(ctx, w, req.Lyrics, []string{dir + "/lyrics", dir}, ".txt"); err != nil {
		return nil, err
	}
	archive := path.Join(dir, id3.ArchiveName)
	if _, err := w.fetch(ctx, archive); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	var results []fileResult
	for _, f := range files {
		r, err := id3.Tag(w.local(f), doc, opt)
		if err != nil {
			return nil, err
		}
		// The .m4a gets exactly what the .mp3 got, not the 300x300 copy it made.
		if opt.Cover == "" {
			opt.Cover = r.Cover
		}
		if opt.Lyrics == "" {
			opt.Lyrics = r.Lyrics
		}
		res := fileResult{
			Path: f, Format: format(f), Generation: gens[f],
			Before: noImages(r.Before), Applied: noImages(r.Written), ReadBack: noImages(r.ReadBack),
			Cover: w.coverRef(r.Cover, r.Written), Lyrics: w.object(r.Lyrics),
		}
		if !req.DryRun {
			res.ReadBackPath, res.Archive = w.object(r.ReadBackPath), archive
		}
		results = append(results, res)
	}
	if req.DryRun {
		return map[string]any{"results": results}, nil
	}

	// Audio first: if someone changed it meanwhile, nothing else is written.
	for i, f := range files {
		if results[i].Generation, err = s.Store.Put(ctx, f, w.local(f), gens[f]); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		w.done[f] = true
	}
	if err := w.putChanged(ctx, s.Store); err != nil {
		return nil, err
	}
	return map[string]any{"results": results}, nil
}

func format(p string) string { return strings.TrimPrefix(strings.ToLower(path.Ext(p)), ".") }

func (s *Server) checkPath(p string) error {
	switch {
	case p == "" || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../"):
		return bad("path %q must be a clean relative object path", p)
	case s.Prefix != "" && !strings.HasPrefix(p, s.Prefix):
		return bad("path %q is outside %q", p, s.Prefix)
	}
	return nil
}

func (s *Server) checkAudio(p string) error {
	if err := s.checkPath(p); err != nil {
		return err
	}
	if f := format(p); f != "mp3" && f != "m4a" {
		return bad("path %q is not an .mp3 or .m4a", p)
	}
	return nil
}

func (s *Server) fetchAudio(ctx context.Context, w *work, name string) (int64, error) {
	size, _, err := s.Store.Stat(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if s.MaxSize > 0 && size > s.MaxSize {
		return 0, httpError{http.StatusRequestEntityTooLarge,
			fmt.Sprintf("%s is %d MB, over the %d MB limit — tag it with the CLI and upload", name, size>>20, s.MaxSize>>20)}
	}
	return w.fetch(ctx, name)
}

// fetchAsset downloads the named object, or — when none is named — every
// candidate file in dirs so the id3 lookup can choose as the CLI does. It
// returns the local path for an explicit asset, "" for discovery.
func (s *Server) fetchAsset(ctx context.Context, w *work, name string, dirs []string, exts ...string) (string, error) {
	if name != "" {
		if err := s.checkPath(name); err != nil {
			return "", err
		}
		if _, err := w.fetch(ctx, name); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return w.local(name), nil
	}
	for _, d := range dirs {
		names, err := s.Store.List(ctx, d)
		if err != nil {
			return "", err
		}
		for _, n := range names {
			for _, e := range exts {
				if strings.EqualFold(path.Ext(n), e) {
					if _, err := w.fetch(ctx, n); err != nil {
						return "", err
					}
				}
			}
		}
	}
	return "", nil
}

// work is one request's temp dir, mirroring object paths, plus what was
// downloaded so changed and new files can be put back.
type work struct {
	root  string
	store Store
	got   map[string]fetched // object name → state at download
	done  map[string]bool    // already uploaded
}

type fetched struct {
	gen  int64
	size int64
	mod  time.Time
}

func (s *Server) newWork() (*work, error) {
	root, err := os.MkdirTemp("", "id3-")
	return &work{root: root, store: s.Store, got: map[string]fetched{}, done: map[string]bool{}}, err
}

func (w *work) close() { os.RemoveAll(w.root) }

func (w *work) local(name string) string { return filepath.Join(w.root, filepath.FromSlash(name)) }

func (w *work) object(local string) string {
	if local == "" {
		return ""
	}
	rel, err := filepath.Rel(w.root, local)
	if err != nil {
		return local
	}
	return filepath.ToSlash(rel)
}

func (w *work) fetch(ctx context.Context, name string) (int64, error) {
	if f, ok := w.got[name]; ok {
		return f.gen, nil
	}
	gen, err := w.store.Get(ctx, name, w.local(name))
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(w.local(name))
	if err != nil {
		return 0, err
	}
	w.got[name] = fetched{gen, fi.Size(), fi.ModTime()}
	return gen, nil
}

// putChanged uploads every file the pipeline created or rewrote: the
// read-back JSON, the archive, the 300x300 cover copy.
func (w *work) putChanged(ctx context.Context, s Store) error {
	return filepath.WalkDir(w.root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		name := w.object(p)
		fi, err := e.Info()
		if err != nil || w.done[name] {
			return err
		}
		ifGen := Any
		if f, ok := w.got[name]; ok {
			if f.size == fi.Size() && f.mod.Equal(fi.ModTime()) {
				return nil
			}
			ifGen = f.gen
		}
		if _, err := s.Put(ctx, name, p, ifGen); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
}

func (w *work) coverRef(local string, applied id3.Doc) *coverRef {
	if local == "" || len(applied.CoverArt) == 0 {
		return nil
	}
	sum := sha256.Sum256(applied.CoverArt[0].Data)
	return &coverRef{w.object(local), hex.EncodeToString(sum[:])}
}

func noImages(d id3.Doc) id3.Doc {
	pics := make([]id3.Picture, len(d.CoverArt))
	for i, p := range d.CoverArt {
		p.Data = nil
		pics[i] = p
	}
	if len(pics) == 0 {
		pics = nil
	}
	d.CoverArt = pics
	return d
}
