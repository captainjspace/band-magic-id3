package id3

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeLayer(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveOverridesAndMergesMaps(t *testing.T) {
	dir := t.TempDir()
	band := writeLayer(t, dir, "band.yaml", `
audio_metadata:
  artist: Test Band
  genre: Rock
  website: https://example.com
`)
	album := writeLayer(t, dir, "album.yaml", `
audio_metadata:
  album: Test Album
  year: 2026
  release_date: 2026-01-09
  involved_people:
    producer: Person One
    engineer: Person One
    engineer: Person Two (Tracks 1,6,9)
  musicians:
    vocals: Person One
    vocals: Person Three
`)
	song := writeLayer(t, dir, "song.yaml", `
audio_metadata:
  title: Freedom
  genre: Rock, Classic Rock
  composer: 'Test Composer'
  bpm: 110
  involved_people:
    recording_engineer: "Person Two"
`)
	d, err := Resolve(band, album, song)
	if err != nil {
		t.Fatal(err)
	}
	if d.Genre != "Rock, Classic Rock" || d.Composer != "Test Composer" || d.Artist != "Test Band" {
		t.Errorf("override: %+v", d)
	}
	if d.Year != "2026" || d.ReleaseDate != "2026-01-09" || d.BPM != "110" {
		t.Errorf("scalars: year=%q date=%q bpm=%q", d.Year, d.ReleaseDate, d.BPM)
	}
	want := map[string]People{
		"producer":           {"Person One"},
		"engineer":           {"Person One", "Person Two (Tracks 1,6,9)"},
		"recording_engineer": {"Person Two"},
	}
	if !reflect.DeepEqual(d.InvolvedPeople, want) {
		t.Errorf("involved_people = %v", d.InvolvedPeople)
	}
	if got := d.Musicians["vocals"]; !reflect.DeepEqual(got, People{"Person One", "Person Three"}) {
		t.Errorf("repeated vocals = %v", got)
	}
}

func TestResolveFailsOnBadLayer(t *testing.T) {
	dir := t.TempDir()
	bad := writeLayer(t, dir, "bad.yaml", "audio_metadata:\n  title: \"unclosed\n")
	if _, err := Resolve(bad); err == nil || !strings.Contains(err.Error(), "bad.yaml") {
		t.Errorf("want error naming the file, got %v", err)
	}
	if _, err := Resolve(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("missing layer must be an error")
	}
}

func TestLoadPythonReadBack(t *testing.T) {
	dir := t.TempDir()
	p := writeLayer(t, dir, "rb.yaml", `
audio_metadata:
  title: X
  cover_art:
  - type: !!python/object/new:mutagen.id3._specs.PictureType
    - 3
    mime_type: image/jpeg
    description: Auto-found cover art
    data: AQID
`)
	d, err := Resolve(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.CoverArt) != 1 || d.CoverArt[0].Type != 3 || string(d.CoverArt[0].Data) != "\x01\x02\x03" {
		t.Errorf("cover_art = %+v", d.CoverArt)
	}
}

func TestPicTypeNames(t *testing.T) {
	for in, want := range map[string]PicType{`"Front Cover"`: 3, `"COVER_FRONT"`: 3, `"Back Cover"`: 4, `4`: 4} {
		var p PicType
		if err := p.UnmarshalJSON([]byte(in)); err != nil || p != want {
			t.Errorf("%s → %d, %v", in, p, err)
		}
	}
}

func TestDigitStringsKeepTheirText(t *testing.T) {
	p := writeLayer(t, t.TempDir(), "ids.yaml", "audio_metadata:\n  isrc: 00112233\n  unique_file_identifiers:\n  - owner_identifier: test-upc\n    identifier: 012345678905\n  bpm: 110\n")
	d, err := Resolve(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.ISRC != "00112233" || d.UniqueFileIDs[0].Identifier != "012345678905" || d.BPM != "110" {
		t.Errorf("isrc %q identifier %q bpm %q", d.ISRC, d.UniqueFileIDs[0].Identifier, d.BPM)
	}
}
