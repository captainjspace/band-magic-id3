package id3

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/bogem/id3v2/v2"
	"github.com/captainjspace/band-magic-id3/internal/testaudio"
)

// v22File writes a silent MP3 behind a hand-built ID3v2.2 tag, like an
// early iTunes rip. Returns the path and the audio bytes.
func v22File(t *testing.T) (string, []byte) {
	t.Helper()
	audio, err := os.ReadFile(testaudio.MP3(t, t.TempDir(), "audio.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	frame := func(id, body string) []byte {
		n := len(body)
		return append([]byte{id[0], id[1], id[2], byte(n >> 16), byte(n >> 8), byte(n)}, body...)
	}
	var frames []byte
	for _, f := range [][]byte{
		frame("TT2", "\x00Old Title"),
		frame("TP1", "\x00Old Artist"),
		frame("TRK", "\x003/12"),
		frame("TYE", "\x002008"),
		frame("COM", "\x00eng\x00an old comment"),
		frame("PIC", "\x00JPG\x03\x00\xff\xd8\xff\xd9"),
		frame("XYZ", "\x00no v2.3 equivalent"),
	} {
		frames = append(frames, f...)
	}
	frames = append(frames, make([]byte, 32)...) // padding
	n := len(frames)
	tag := append([]byte{'I', 'D', '3', 2, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}, frames...)

	p := filepath.Join(t.TempDir(), "old.mp3")
	if err := os.WriteFile(p, append(tag, audio...), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, audio
}

func TestReadV22LeavesFileAlone(t *testing.T) {
	p, _ := v22File(t)
	orig, _ := os.ReadFile(p)

	d, err := ReadMP3(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Old Title" || d.Artist != "Old Artist" || d.Year != "2008" ||
		d.TrackNumber != 3 || d.TrackTotal != 12 || d.Comment != "an old comment" {
		t.Errorf("read %+v", d)
	}
	if len(d.CoverArt) != 1 || d.CoverArt[0].MimeType != "image/jpeg" || d.CoverArt[0].Type != 3 {
		t.Errorf("cover %+v", d.CoverArt)
	}
	if now, _ := os.ReadFile(p); !bytes.Equal(now, orig) {
		t.Error("ReadMP3 changed the file")
	}
}

func TestWriteV22UpgradesToV24(t *testing.T) {
	p, audio := v22File(t)

	if err := WriteMP3(p, Doc{Title: "New Title", Artist: "Test Band", TrackNumber: 4}, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b[:3]) != "ID3" || b[3] != 4 {
		t.Fatalf("header % x, want ID3v2.4", b[:5])
	}
	if !bytes.HasSuffix(b, audio) {
		t.Error("audio frames changed")
	}
	d, err := ReadMP3(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "New Title" || d.Artist != "Test Band" || d.TrackNumber != 4 || d.Comment != "" || len(d.CoverArt) != 0 {
		t.Errorf("read back %+v", d)
	}
}

func TestUpgradeV22IgnoresOtherVersions(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("ID3\x03\x00\x00\x00\x00\x00\x00"), []byte("not a tag at all")} {
		if up, err := upgradeV22(b); up != nil || err != nil {
			t.Errorf("upgradeV22(%q) = %q, %v", b, up, err)
		}
	}
}

func TestUpgradeV22KeepsValues(t *testing.T) {
	p, audio := v22File(t)
	before, _ := ReadMP3(p)

	if from, err := Upgrade(p, false); err != nil || from != 2 {
		t.Fatalf("Upgrade = %d, %v", from, err)
	}
	b, _ := os.ReadFile(p)
	if b[3] != 4 || !bytes.HasSuffix(b, audio) {
		t.Fatalf("header % x, audio kept %v", b[:5], bytes.HasSuffix(b, audio))
	}
	after, err := ReadMP3(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("values changed\nbefore %+v\nafter  %+v", before, after)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p), ArchiveName)); err != nil {
		t.Errorf("no archive: %v", err)
	}
	if from, err := Upgrade(p, false); err != nil || from != 4 {
		t.Errorf("second Upgrade = %d, %v", from, err)
	}
}

func TestUpgradeV23Dates(t *testing.T) {
	p := testaudio.MP3(t, t.TempDir(), "v23.mp3")
	tag, err := id3v2.Open(p, id3v2.Options{})
	if err != nil {
		t.Fatal(err)
	}
	tag.SetVersion(3)
	tag.AddTextFrame("TYER", id3v2.EncodingISO, "2008")
	tag.AddTextFrame("TDAT", id3v2.EncodingISO, "2506")
	tag.AddTextFrame("TIT2", id3v2.EncodingISO, "Old Title")
	if err := tag.Save(); err != nil {
		t.Fatal(err)
	}
	tag.Close()

	if from, err := Upgrade(p, false); err != nil || from != 3 {
		t.Fatalf("Upgrade = %d, %v", from, err)
	}
	d, _ := ReadMP3(p)
	if d.Year != "2008-06-25" || d.Title != "Old Title" {
		t.Errorf("got year %q title %q", d.Year, d.Title)
	}
}
