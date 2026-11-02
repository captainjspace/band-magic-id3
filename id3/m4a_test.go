package id3

import (
	"encoding/json"
	"fmt"
	"github.com/captainjspace/band-magic-id3/internal/testaudio"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func silentM4A(t *testing.T, dir, name string) string { return testaudio.M4A(t, dir, name) }

func TestM4ARoundTrip(t *testing.T) {
	m4a := silentM4A(t, t.TempDir(), "Test Song?.m4a")
	in := Doc{
		Title: "Test Song?", Artist: "Test Band", Album: "Test Album", AlbumArtist: "Test Band",
		Genre: "Rock, Test Rock", Year: "2025", ReleaseDate: "2025-06-22",
		TrackNumber: 3, TrackTotal: 10, DiscNumber: 1, DiscTotal: 1,
		Composer: "Test Composer", Lyricist: "Test Lyricist", ISRC: "XX-TST-25-00001", BPM: "153",
		Copyright: "© 2025 Test Label", Publisher: "Test Publishing", EncoderSettings: "Logic Pro 11.2",
		ArtistSort: "Band, Test", Website: "https://example.com",
		Comment: "A made-up comment 💙", Lyrics: "line one\n\nline two\n",
		CoverArt:       []Picture{{3, "image/jpeg", "", []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}}},
		InvolvedPeople: map[string]People{"producer": {"Person One"}, "recording_engineer": {"Person Two (Tracks 1,6,9)", "Person One"}},
		Musicians:      map[string]People{"vocals": {"Person One", "Person Three"}},
		CustomTags:     map[string]Text{"Original Filename": "Test Song?.wav", "Lyrics Link": "https://example.com/lyrics"},
	}
	before := exif4(t, m4a)["iTunSMPB"]
	for run := 0; run < 2; run++ { // second run proves clear replaces instead of piling up
		if err := WriteM4A(m4a, in, true); err != nil {
			t.Fatal(err)
		}
	}
	out, err := ReadM4A(m4a)
	if err != nil {
		t.Fatal(err)
	}
	want := in
	want.ArtistSort = "" // go-mp4tag writes sort atoms but can't read them; exiftool checks below
	if !reflect.DeepEqual(asMap(t, want), asMap(t, out)) {
		t.Errorf("round trip mismatch\n in: %s\nout: %s", mustJSON(in), mustJSON(out))
	}

	if _, err := exec.LookPath("exiftool"); err != nil {
		return
	}
	ex := exif4(t, m4a)
	for k, v := range map[string]string{"Title": "Test Song?", "Artist": "Test Band", "TrackNumber": "3 of 10", "ISRC": "XX-TST-25-00001", "Composer": "Test Composer", "SortArtist": "Band, Test", "iTunSMPB": fmt.Sprint(before)} {
		if got := fmt.Sprint(ex[k]); got != v {
			t.Errorf("exiftool %s = %q, want %q", k, got, v)
		}
	}
}

func TestTagM4ABesideMP3(t *testing.T) {
	dir := t.TempDir()
	mp3 := silentMP3(t, dir, "Song.mp3")
	m4a := silentM4A(t, dir, "Song.m4a")
	os.Mkdir(filepath.Join(dir, "covers"), 0o755)
	writeCover(t, filepath.Join(dir, "covers", "cover.jpg"), 800)

	d := Doc{Title: "Song", Artist: "Test Band", Composer: "Test Composer"}
	for _, f := range []string{mp3, m4a} {
		if _, err := Tag(f, d, Options{}); err != nil {
			t.Fatal(f, err)
		}
	}
	a, _ := Read(mp3)
	b, _ := Read(m4a)
	if a.Title != b.Title || a.Composer != b.Composer || len(b.CoverArt) != 1 || string(a.CoverArt[0].Data) != string(b.CoverArt[0].Data) {
		t.Errorf("mp3 and m4a disagree:\nmp3 %s\nm4a %s", mustJSON(withoutData(a)), mustJSON(withoutData(b)))
	}
	for _, f := range []string{"ID3-Song.json", "ID3-Song.m4a.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("read-back %s missing", f)
		}
	}
}

func exif4(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := exec.Command("exiftool", "-json", "-ItemList:all", "-iTunes:all", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out[0]
}
