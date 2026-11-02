package id3

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveName is the rollback archive kept beside each MP3, same name the
// Python tool used so existing archives keep growing.
const ArchiveName = "song-id3-architect.tar.gz"

// formats dispatches on file extension; both share the same Doc.
var formats = map[string]struct {
	read  func(string) (Doc, error)
	write func(string, Doc, bool) error
}{
	".mp3": {ReadMP3, WriteMP3},
	".m4a": {ReadM4A, WriteM4A},
}

// Read returns the tags on an MP3 or M4A.
func Read(path string) (Doc, error) {
	f, ok := formats[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return Doc{}, fmt.Errorf("%s: not an .mp3 or .m4a", path)
	}
	return f.read(path)
}

type Options struct {
	Lookup
	Keep bool // keep unmanaged frames (default clears, like --clear)
}

type Result struct {
	Cover, Lyrics string
	Before        Doc // tags on the file before this write
	Written       Doc // what was handed to the writer
	ReadBack      Doc // what is on the file afterwards
	ReadBackPath  string
}

// Tag is the whole write pipeline for one MP3 or M4A:
// back up current tags → embed cover/lyrics → write → read back →
// save ID3-<name>.json → append backup + supplied doc to the archive.
func Tag(mp3 string, d Doc, opt Options) (Result, error) {
	var r Result
	format, ok := formats[strings.ToLower(filepath.Ext(mp3))]
	if !ok {
		return r, fmt.Errorf("%s: not an .mp3 or .m4a", mp3)
	}
	before, err := format.read(mp3)
	if err != nil {
		return r, err
	}
	r.Before = before
	supplied := d

	if r.Cover, r.Lyrics, err = ApplyAssets(mp3, &d, opt.Lookup); err != nil {
		return r, err
	}
	r.Written = d
	if opt.DryRun {
		return r, nil
	}
	if err := format.write(mp3, d, !opt.Keep); err != nil {
		return r, err
	}
	if r.ReadBack, err = format.read(mp3); err != nil {
		return r, err
	}

	dir := filepath.Dir(mp3)
	// MP3 keeps the old ID3-<name> naming; an M4A beside it gets <name>.m4a.
	base := strings.TrimSuffix(filepath.Base(mp3), filepath.Ext(mp3))
	if ext := strings.ToLower(filepath.Ext(mp3)); ext != ".mp3" {
		base += ext
	}
	r.ReadBackPath = filepath.Join(dir, "ID3-"+base+".json")
	if err := writeReadOnly(r.ReadBackPath, mustJSON(r.ReadBack)); err != nil {
		return r, err
	}

	stamp := time.Now().Format("20060102_150405")
	members := map[string][]byte{"supplied-" + base + "-" + stamp + ".json": mustJSON(supplied)}
	if !isZero(before) {
		members[base+"_backup_"+stamp+".json"] = mustJSON(before)
	}
	return r, AppendArchive(filepath.Join(dir, ArchiveName), members)
}

// AppendArchive rewrites the tar.gz with its old members plus new ones,
// via a temp file and rename, and leaves it read-only.
func AppendArchive(path string, members map[string][]byte) error {
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)

	if err := copyMembers(path, tw); err != nil {
		return err
	}
	now := time.Now()
	for name, body := range members {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: now}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return writeReadOnly(path, out.Bytes())
}

func copyMembers(path string, tw *tar.Writer) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
}

// writeReadOnly replaces path atomically and marks it 0400, matching the
// Python tool's read-only outputs.
func writeReadOnly(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o400); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

func isZero(d Doc) bool {
	return string(mustJSON(d)) == "{}\n"
}
