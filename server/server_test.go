package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/captainjspace/band-magic-id3/id3"
	"github.com/captainjspace/band-magic-id3/internal/testaudio"
)

// album lays out one track the way the bucket does:
// 2026/Test Song/{Test Song.mp3, Test Song.m4a, covers/, lyrics/}.
func album(t *testing.T, withM4A bool) (root string, srv *httptest.Server) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "2026", "Test Song")
	testaudio.MP3(t, dir, "Test Song.mp3")
	if withM4A {
		testaudio.M4A(t, dir, "Test Song.m4a")
	}
	os.MkdirAll(filepath.Join(dir, "covers"), 0o755)
	img := image.NewRGBA(image.Rect(0, 0, 900, 900))
	for x := 0; x < 900; x++ {
		img.Set(x, x, color.RGBA{0, 0, 200, 255})
	}
	f, _ := os.Create(filepath.Join(dir, "covers", "test-song-cover.jpg"))
	jpeg.Encode(f, img, nil)
	f.Close()
	os.MkdirAll(filepath.Join(dir, "lyrics"), 0o755)
	os.WriteFile(filepath.Join(dir, "lyrics", "Test Song.txt"), []byte("la la\n"), 0o644)

	s := &Server{Store: DirStore{Root: root}, Prefix: "2026/", MaxSize: 50 << 20}
	srv = httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return root, srv
}

func post(t *testing.T, srv *httptest.Server, path string, body any, out any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

var layers = []map[string]any{
	{"audio_metadata": map[string]any{"artist": "Test Band", "involved_people": map[string]any{"producer": "Person One"}}},
	{"album": "Test Album", "year": 2025},
	{"title": "Test Song", "track_number": 3, "involved_people": map[string]any{"mixing_engineer": "Person Two"}},
}

type tagResp struct {
	Results []fileResult `json:"results"`
	Error   string       `json:"error"`
}

func TestTagMP3AndM4A(t *testing.T) {
	root, srv := album(t, true)
	var resp tagResp
	if code := post(t, srv, "/v1/tag", map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers}, &resp); code != 200 {
		t.Fatalf("status %d: %s", code, resp.Error)
	}
	if len(resp.Results) != 2 || resp.Results[1].Format != "m4a" {
		t.Fatalf("want mp3 + m4a results, got %+v", resp.Results)
	}
	for _, r := range resp.Results {
		if r.ReadBack.Title != "Test Song" || r.ReadBack.Artist != "Test Band" || r.ReadBack.TrackNumber != 3 {
			t.Errorf("%s read_back = %+v", r.Path, r.ReadBack)
		}
		if got := r.ReadBack.InvolvedPeople; len(got["producer"]) != 1 || len(got["mixing_engineer"]) != 1 {
			t.Errorf("%s credits not merged: %v", r.Path, got)
		}
		if r.Cover == nil || r.Cover.Path != "2026/Test Song/covers/test-song-cover.jpg" || r.Lyrics != "2026/Test Song/lyrics/Test Song.txt" {
			t.Errorf("%s assets: cover %+v lyrics %q", r.Path, r.Cover, r.Lyrics)
		}
		if len(r.ReadBack.CoverArt) != 1 || r.ReadBack.CoverArt[0].Data != nil {
			t.Errorf("%s: cover_art should be listed without bytes", r.Path)
		}
	}

	// The objects in the "bucket" changed, not just the temp copies.
	mp3 := filepath.Join(root, "2026", "Test Song", "Test Song.mp3")
	d, err := id3.Read(mp3)
	if err != nil || d.Title != "Test Song" {
		t.Fatalf("stored mp3 not tagged: %v %+v", err, d)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(d.CoverArt[0].Data))
	if cfg.Width != 300 {
		t.Errorf("cover width %d", cfg.Width)
	}
	for _, f := range []string{"ID3-Test Song.json", "ID3-Test Song.m4a.json", id3.ArchiveName, "covers/test-song-cover_300x300.jpg"} {
		if _, err := os.Stat(filepath.Join(root, "2026", "Test Song", f)); err != nil {
			t.Errorf("%s not written back", f)
		}
	}

	// Second run: before is now the first run's tags.
	var again tagResp
	post(t, srv, "/v1/tag", map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers, "m4a": false}, &again)
	if len(again.Results) != 1 || again.Results[0].Before.Title != "Test Song" {
		t.Errorf("second run before = %+v", again.Results)
	}
}

func TestReadAndMerge(t *testing.T) {
	_, srv := album(t, false)
	var merged struct{ Doc id3.Doc }
	if post(t, srv, "/v1/merge", map[string]any{"layers": layers}, &merged); merged.Doc.Title != "Test Song" || merged.Doc.Year != "2025" {
		t.Errorf("merge = %+v", merged.Doc)
	}
	post(t, srv, "/v1/tag", map[string]any{"path": "2026/Test Song/Test Song.mp3", "doc": map[string]any{"title": "Doc Form"}}, nil)
	var read struct {
		Doc        id3.Doc
		Generation int64
		Format     string
	}
	if code := post(t, srv, "/v1/read", map[string]any{"path": "2026/Test Song/Test Song.mp3"}, &read); code != 200 || read.Doc.Title != "Doc Form" || read.Format != "mp3" || read.Generation == 0 {
		t.Errorf("read %d %+v", code, read)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	root, srv := album(t, false)
	mp3 := filepath.Join(root, "2026", "Test Song", "Test Song.mp3")
	before, _ := os.ReadFile(mp3)
	var resp tagResp
	if code := post(t, srv, "/v1/tag", map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers, "dry_run": true}, &resp); code != 200 {
		t.Fatalf("status %d: %s", code, resp.Error)
	}
	after, _ := os.ReadFile(mp3)
	if !bytes.Equal(before, after) || resp.Results[0].Applied.Title != "Test Song" {
		t.Error("dry run changed the file or returned nothing")
	}
	if _, err := os.Stat(filepath.Join(root, "2026", "Test Song", id3.ArchiveName)); err == nil {
		t.Error("dry run wrote an archive")
	}
}

func TestErrors(t *testing.T) {
	_, srv := album(t, false)
	for _, tc := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"path": "../etc/passwd.mp3", "layers": layers}, 400},
		{map[string]any{"path": "/2026/Test Song/Test Song.mp3", "layers": layers}, 400},
		{map[string]any{"path": "2025/Other/Song.mp3", "layers": layers}, 400}, // outside prefix
		{map[string]any{"path": "2026/Test Song/Test Song.wav", "layers": layers}, 400},
		{map[string]any{"path": "2026/Test Song/Test Song.mp3"}, 400}, // no layers
		{map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers, "bogus": 1}, 400},
		{map[string]any{"path": "2026/Nope/Nope.mp3", "layers": layers}, 404},
		{map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers, "cover": "2026/Test Song/covers/missing.jpg"}, 404},
	} {
		var resp tagResp
		if code := post(t, srv, "/v1/tag", tc.body, &resp); code != tc.want || resp.Error == "" {
			t.Errorf("%v → %d %q, want %d", tc.body, code, resp.Error, tc.want)
		}
	}
}

func TestTooLarge(t *testing.T) {
	root, _ := album(t, false)
	s := &Server{Store: DirStore{Root: root}, MaxSize: 1000}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	var resp tagResp
	if code := post(t, srv, "/v1/tag", map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers}, &resp); code != 413 {
		t.Errorf("status %d %q", code, resp.Error)
	}
}

// racingStore changes the audio object between download and put.
type racingStore struct{ DirStore }

func (s racingStore) Get(ctx context.Context, name, local string) (int64, error) {
	gen, err := s.DirStore.Get(ctx, name, local)
	if filepath.Ext(name) == ".mp3" {
		later := time.Now().Add(time.Hour)
		os.Chtimes(s.full(name), later, later)
	}
	return gen, err
}

func TestConflictLeavesEverythingAlone(t *testing.T) {
	root, _ := album(t, false)
	srv := httptest.NewServer((&Server{Store: racingStore{DirStore{Root: root}}}).Handler())
	defer srv.Close()
	var resp tagResp
	if code := post(t, srv, "/v1/tag", map[string]any{"path": "2026/Test Song/Test Song.mp3", "layers": layers}, &resp); code != 409 {
		t.Fatalf("status %d %q", code, resp.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "2026", "Test Song", id3.ArchiveName)); err == nil {
		t.Error("archive written despite conflict")
	}
}
