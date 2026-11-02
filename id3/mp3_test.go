package id3

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"github.com/captainjspace/band-magic-id3/internal/testaudio"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func silentMP3(t *testing.T, dir, name string) string { return testaudio.MP3(t, dir, name) }

func TestWriteReadRoundTrip(t *testing.T) {
	mp3 := silentMP3(t, t.TempDir(), "Test Song?.mp3")
	in := Doc{
		Title: "Test Song?", Artist: "Test Band", Album: "Test Album",
		Year: "2025", ReleaseDate: "2025-06-22", TrackNumber: 3, TrackTotal: 10, DiscNumber: 1, DiscTotal: 1,
		Composer: "Test Composer", BPM: "153", ISRC: "XX-TST-25-00001",
		ArtistSort: "Test Band", Website: "https://example.com",
		Comment:        "A made-up comment 💙",
		LyricsSections: []LangText{{"eng", "Full Lyrics", "line one\n\nline two\n"}},
		CoverArt:       []Picture{{3, "image/jpeg", "front", []byte{1, 2, 3}}},
		InvolvedPeople: map[string]People{"producer": {"Person One"}, "recording_engineer": {"Person Two", "Person One"}},
		Musicians:      map[string]People{"vocals": {"Person One", "Person Three"}},
		UniqueFileIDs:  []UFID{{"test-upc", "0102"}},
		CustomTags:     map[string]Text{"Original Filename": "Test Song?.wav"},
		PrivateFrames:  []Private{{"com.rbmc", "cafe"}},
	}
	if err := WriteMP3(mp3, in, true); err != nil {
		t.Fatal(err)
	}
	out, err := ReadMP3(mp3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asMap(t, in), asMap(t, out)) {
		t.Errorf("round trip mismatch\n in: %s\nout: %s", mustJSON(in), mustJSON(out))
	}
}

func TestTagPipeline(t *testing.T) {
	dir := t.TempDir()
	mp3 := silentMP3(t, dir, "Song.mp3")
	os.Mkdir(filepath.Join(dir, "covers"), 0o755)
	os.Mkdir(filepath.Join(dir, "lyrics"), 0o755)
	writeCover(t, filepath.Join(dir, "covers", "song-cover.jpg"), 1200)
	os.WriteFile(filepath.Join(dir, "lyrics", "song_lyrics.txt"), []byte("la la\n"), 0o644)

	for run := 0; run < 2; run++ {
		r, err := Tag(mp3, Doc{Title: "Song", Artist: "Test Band"}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.ReadBack.CoverArt) != 1 || r.ReadBack.Lyrics != "" || len(r.ReadBack.LyricsSections) != 1 {
			t.Fatalf("assets not embedded: %s", mustJSON(withoutData(r.ReadBack)))
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(r.ReadBack.CoverArt[0].Data))
		if err != nil || cfg.Width != 300 || cfg.Height != 300 {
			t.Errorf("cover %dx%d %v", cfg.Width, cfg.Height, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "covers", "song-cover_300x300.jpg")); err != nil {
		t.Error("300x300 copy not saved")
	}
	if fi, err := os.Stat(filepath.Join(dir, "ID3-Song.json")); err != nil || fi.Mode().Perm() != 0o400 {
		t.Errorf("read-back file: %v %v", fi, err)
	}
	if n := countMembers(t, filepath.Join(dir, ArchiveName)); n != 3 {
		t.Errorf("archive members = %d, want 3 (supplied, supplied+backup)", n)
	}
}

// TestGoldenAgainstPython tags a silent file from testdata/song.yaml and
// compares it with testdata/python-tagged.mp3, the same input tagged by the
// original id3-tag-manager.py. Known, intended differences:
//   - lyrics: Python stored the list repr ("['line\\n...']"); Go stores the text
//   - year: Go also writes TDRC when release_date is set
func TestGoldenAgainstPython(t *testing.T) {
	dir := t.TempDir()
	mp3 := silentMP3(t, dir, "Test Song.mp3")
	os.Mkdir(filepath.Join(dir, "covers"), 0o755)
	writeCover(t, filepath.Join(dir, "covers", "test-song-cover.jpg"), 600)
	os.Mkdir(filepath.Join(dir, "lyrics"), 0o755)
	copyFile(t, "testdata/lyrics.txt", filepath.Join(dir, "lyrics", "Test Song.txt"))

	in, err := Resolve("testdata/song.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Tag(mp3, in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := ReadMP3("testdata/python-tagged.mp3")
	if err != nil {
		t.Fatal(err)
	}
	lyrics, _ := os.ReadFile("testdata/lyrics.txt")
	want.LyricsSections[0].Text = string(lyrics)
	want.Year = in.Year

	got, want := withoutData(r.ReadBack), withoutData(want)
	if !reflect.DeepEqual(asMap(t, got), asMap(t, want)) {
		t.Errorf("differs from Python-tagged file\n go: %s\npy: %s", mustJSON(got), mustJSON(want))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(r.ReadBack.CoverArt[0].Data))
	if err != nil || cfg.Width != 300 || cfg.Height != 300 {
		t.Errorf("cover %dx%d %v", cfg.Width, cfg.Height, err)
	}

	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Log("exiftool not installed; skipping independent check")
		return
	}
	goTags, pyTags := exif(t, mp3), exif(t, "testdata/python-tagged.mp3")
	for _, k := range []string{"Lyrics", "InvolvedPeople", "MusicianCredits", "RecordingTime"} {
		delete(goTags, k) // credits differ in pair layout only; checked above via ReadMP3
		delete(pyTags, k)
	}
	if !reflect.DeepEqual(goTags, pyTags) {
		t.Errorf("exiftool views differ\n go: %v\npy: %v", goTags, pyTags)
	}
}

func exif(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := exec.Command("exiftool", "-json", "-ID3v2_4:all", "--Picture", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	delete(out[0], "SourceFile")
	return out[0]
}

func writeCover(t *testing.T, path string, size int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for x := 0; x < size; x++ {
		img.Set(x, x, color.RGBA{200, 0, 0, 255})
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

func withoutData(d Doc) Doc {
	pics := make([]Picture, len(d.CoverArt))
	for i, p := range d.CoverArt {
		p.Data = nil
		pics[i] = p
	}
	d.CoverArt = pics
	return d
}

func asMap(t *testing.T, d Doc) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(mustJSON(d), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func countMembers(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for tr := tar.NewReader(gz); ; n++ {
		if _, err := tr.Next(); err != nil {
			return n
		}
	}
}
