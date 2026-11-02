// Package testaudio makes silent audio files for tests, so no real
// recordings ever go in this repo.
package testaudio

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// MP3 writes one second of silence: MPEG-1 Layer III, 128 kbps, 44.1 kHz
// frames (417 bytes) with zeroed side info.
func MP3(t testing.TB, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	frame := append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 413)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, bytes.Repeat(frame, 40), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// M4A makes one second of silent AAC with afconvert, Apple's encoder — the
// same kind of file a Logic bounce produces. Skips the test without it.
func M4A(t testing.TB, dir, name string) string {
	t.Helper()
	if _, err := exec.LookPath("afconvert"); err != nil {
		t.Skip("afconvert not available")
	}
	var wav bytes.Buffer
	for _, v := range []any{[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + 88200), [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(1), uint32(44100), uint32(88200), uint16(2), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, uint32(88200)} {
		binary.Write(&wav, binary.LittleEndian, v)
	}
	wav.Write(make([]byte, 88200))
	src := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(src, wav.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name)
	if b, err := exec.Command("afconvert", "-f", "m4af", "-d", "aac", src, out).CombinedOutput(); err != nil {
		t.Fatalf("afconvert: %v %s", err, b)
	}
	return out
}
