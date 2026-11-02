package id3

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func touch(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAlbumFilesAndFindSong(t *testing.T) {
	album := t.TempDir()
	j := func(p ...string) string { return filepath.Join(append([]string{album}, p...)...) }
	touch(t,
		j("Track 3", "Test Song?.mp3"),
		j("Track 7", "Blue Song.mp3"),
		j("Track 7", "Blue Song-A.mp3"),
		j("Track 7", "Blue Song-7.7MST.mp3"),
		j("Track 7", "temp", "Blue Song.mp3"),
		j("Track 7", "Louder", "Blue Song.mp3"),
		j("Track 9", "Other Song.mp3"),
		j("Track 9", "Other Song.wav"),
	)
	files, err := AlbumFiles(album, ".mp3")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("temp/ and .wav should be skipped: %v", files)
	}
	for song, want := range map[string]string{
		"test song":   j("Track 3", "Test Song?.mp3"),
		"Test-Song":   j("Track 3", "Test Song?.mp3"),
		"blue song":   j("Track 7", "Blue Song.mp3"), // exact beats the -A / -7.7MST takes
		"blue song a": j("Track 7", "Blue Song-A.mp3"),
		"other":       j("Track 9", "Other Song.mp3"), // unique prefix
	} {
		if got, err := FindSong(files, song); err != nil || got != want {
			t.Errorf("FindSong(%q) = %q, %v", song, got, err)
		}
	}
	if _, err := FindSong(files, "blue"); err == nil || !strings.Contains(err.Error(), "Blue Song-A.mp3") {
		t.Errorf("ambiguous prefix should list matches, got %v", err)
	}
}

func TestFindLayerAndTrackDir(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "03-ID3-Test-Song.yaml"), filepath.Join(dir, "07-ID3-Blue-Song.yaml"), filepath.Join(dir, "notes.txt"),
		filepath.Join(dir, "04-ID3-Home.yaml"), filepath.Join(dir, "05-ID3-Going-Home.yaml"),
		filepath.Join(dir, "08-ID3-Where-Are-You.yaml"), filepath.Join(dir, "09-ID3-Where-Are-You-Now.yaml"))
	if got, err := FindLayer(dir, "Test Song?"); err != nil || filepath.Base(got) != "03-ID3-Test-Song.yaml" {
		t.Errorf("FindLayer = %q, %v", got, err)
	}
	for song, want := range map[string]string{"Home": "04-ID3-Home.yaml", "Where Are You?": "08-ID3-Where-Are-You.yaml", "Going Home": "05-ID3-Going-Home.yaml"} {
		if got, err := FindLayer(dir, song); err != nil || filepath.Base(got) != want {
			t.Errorf("FindLayer(%q) = %q, %v", song, got, err)
		}
	}
	if _, err := FindLayer(dir, "?!"); err == nil {
		t.Error("punctuation-only name must not match everything")
	}
	if _, err := FindLayer(dir, "Missing"); err == nil {
		t.Error("missing song layer should be an error")
	}
	if n := TrackFromDir("/a/Album/Track 10/x.mp3"); n != 10 {
		t.Errorf("TrackFromDir = %d", n)
	}
	if n := TrackFromDir("/a/Album/x.mp3"); n != 0 {
		t.Errorf("TrackFromDir no track folder = %d", n)
	}
}

func TestInitAlbum(t *testing.T) {
	album := t.TempDir()
	touch(t, filepath.Join(album, "Track 1", "Song.mp3"), filepath.Join(album, "Track 1", "art.JPG"), filepath.Join(album, "Track 2", "Other.mp3"),
		filepath.Join(album, "Track 2", "cover.jpg"), filepath.Join(album, "Track 2", "covers", "cover.jpg"))
	if _, err := InitAlbum(album, false); err != nil {
		t.Fatal(err)
	}
	var got []string
	filepath.WalkDir(album, func(p string, _ os.DirEntry, _ error) error {
		rel, _ := filepath.Rel(album, p)
		got = append(got, rel)
		return nil
	})
	want := []string{".", "Track 1", "Track 1/Song.mp3", "Track 1/covers", "Track 1/covers/art.JPG", "Track 1/lyrics",
		"Track 2", "Track 2/Other.mp3", "Track 2/cover.jpg", "Track 2/covers", "Track 2/covers/cover.jpg", "Track 2/lyrics"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tree = %v", got)
	}
	if log, _ := InitAlbum(album, false); len(log) != 1 {
		t.Errorf("second run should only report the skipped duplicate: %v", log)
	}
}
