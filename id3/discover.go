package id3

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Folders the old runner pruned from the album search.
var skipDirs = map[string]bool{"temp": true, "tmp": true, "covers": true, "lyrics": true}

// norm reduces a name to lowercase letters and digits, so "Where Are You?",
// "Where-Are-You" and "where_are_you" all compare equal.
func norm(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

func baseName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// AlbumFiles lists files with the given extension in an album folder and
// its track folders (album/x.mp3, album/Track N/x.mp3) — deeper folders hold
// alternate mixes and are left alone, as are temp/tmp/covers/lyrics.
func AlbumFiles(albumDir, ext string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(albumDir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() && p != albumDir && (skipDirs[strings.ToLower(e.Name())] || filepath.Dir(p) != filepath.Clean(albumDir)) {
			return filepath.SkipDir
		}
		if !e.IsDir() && strings.EqualFold(filepath.Ext(p), ext) {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// FindSong picks the file for a song name: an exact (normalized) name match
// wins, then a single prefix match. Several prefix matches is an error that
// lists them — pass the file explicitly instead.
func FindSong(files []string, song string) (string, error) {
	want := norm(song)
	var prefix []string
	for _, f := range files {
		n := norm(baseName(f))
		if n == want {
			return f, nil
		}
		if strings.HasPrefix(n, want) {
			prefix = append(prefix, f)
		}
	}
	switch len(prefix) {
	case 0:
		return "", fmt.Errorf("no file matches %q", song)
	case 1:
		return prefix[0], nil
	}
	return "", fmt.Errorf("%q matches several files, pick one with -mp3:\n  %s", song, strings.Join(prefix, "\n  "))
}

// layerPrefix is the "03-ID3-" numbering on song layer file names.
var layerPrefix = regexp.MustCompile(`(?i)^\d+[-_ ]*(id3[-_ ]*)?`)

// FindLayer finds the song layer in dir for a song name. A layer whose name
// (minus "NN-ID3-") equals the song wins; otherwise exactly one layer must
// contain it. "Home" never silently picks "Going-Home" when "Home" exists,
// and several containing matches is an error that lists them.
func FindLayer(dir, song string) (string, error) {
	want := norm(song)
	if want == "" {
		return "", fmt.Errorf("song name %q has no letters or digits", song)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var hits []string
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.Type().IsRegular() || (ext != ".yaml" && ext != ".yml" && ext != ".json") {
			continue
		}
		n := norm(layerPrefix.ReplaceAllString(baseName(e.Name()), ""))
		if n == want {
			return filepath.Join(dir, e.Name()), nil
		}
		if strings.Contains(n, want) {
			hits = append(hits, filepath.Join(dir, e.Name()))
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no song layer for %q in %s", song, dir)
	case 1:
		return hits[0], nil
	}
	return "", fmt.Errorf("several song layers for %q:\n  %s", song, strings.Join(hits, "\n  "))
}

var trackDir = regexp.MustCompile(`(?i)^track\s*(\d+)$`)

// TrackFromDir reads N from a "Track N" parent folder, 0 if there isn't one.
func TrackFromDir(path string) Int {
	m := trackDir.FindStringSubmatch(filepath.Base(filepath.Dir(path)))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return Int(n)
}

// InitAlbum gives every track folder covers/ and lyrics/, and moves loose
// images into covers/ (what mk-track-subdirs.zsh did). Returns what it did.
func InitAlbum(albumDir string, dryRun bool) ([]string, error) {
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		return nil, err
	}
	var log []string
	do := func(msg string, f func() error) error {
		log = append(log, msg)
		if dryRun {
			return nil
		}
		return f()
	}
	for _, e := range entries {
		if !e.IsDir() || skipDirs[strings.ToLower(e.Name())] {
			continue
		}
		track := filepath.Join(albumDir, e.Name())
		for _, sub := range []string{"covers", "lyrics"} {
			dir := filepath.Join(track, sub)
			if _, err := os.Stat(dir); err == nil {
				continue
			}
			if err := do("mkdir "+dir, func() error { return os.Mkdir(dir, 0o755) }); err != nil {
				return log, err
			}
		}
		files, _ := os.ReadDir(track)
		for _, f := range files {
			if !f.Type().IsRegular() || !hasExt(f.Name(), imageExts) {
				continue
			}
			from, to := filepath.Join(track, f.Name()), filepath.Join(track, "covers", f.Name())
			if _, err := os.Stat(to); err == nil {
				log = append(log, "skip "+from+" (covers/ already has "+f.Name()+")")
				continue
			}
			if err := do("mv "+from+" → covers/", func() error { return os.Rename(from, to) }); err != nil {
				return log, err
			}
		}
	}
	return log, nil
}
