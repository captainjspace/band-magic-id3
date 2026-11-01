package id3

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "image/gif"

	"golang.org/x/image/draw"
)

const coverSize = 300

// Lookup controls where cover art and lyrics come from. Explicit paths win;
// otherwise the MP3's covers/ and lyrics/ folders are searched, then the MP3
// folder itself.
type Lookup struct {
	Cover      string   // explicit image path
	Lyrics     string   // explicit .txt path
	CoverNames []string // traditional names, tried after song/album names
	DryRun     bool     // don't save the resized cover
}

var DefaultCoverNames = []string{"cover", "folder", "albumart"}

var imageExts = []string{".jpg", ".jpeg", ".png"}

// ApplyAssets embeds the cover (always replacing cover_art, as the Python
// tool did) and lyrics (only when the doc has none). It returns the files used.
func ApplyAssets(mp3 string, d *Doc, l Lookup) (cover, lyrics string, err error) {
	dir := filepath.Dir(mp3)
	names := l.CoverNames
	if names == nil {
		names = DefaultCoverNames
	}

	cover = l.Cover
	if cover == "" {
		cover = find([]string{filepath.Join(dir, "covers"), dir}, coverCandidates(d, names), names, imageExts)
	}
	if cover != "" {
		pic, err := coverPicture(cover, l.DryRun)
		if err != nil {
			return "", "", err
		}
		d.CoverArt = []Picture{pic}
	}

	lyrics = l.Lyrics
	if lyrics == "" && d.Lyrics == "" && len(d.LyricsSections) == 0 {
		lyrics = find([]string{filepath.Join(dir, "lyrics"), dir}, variants(string(d.Title), ".txt"), []string{"lyrics"}, []string{".txt"})
	}
	if lyrics != "" {
		b, err := os.ReadFile(lyrics)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(string(b)) != "" {
			d.Lyrics = ""
			d.LyricsSections = []LangText{{"eng", "Full Lyrics", string(b)}}
		}
	}
	return cover, lyrics, nil
}

// coverCandidates keeps the Python priority: 300x300 copies first (song,
// album, traditional), then full-size in the same order.
func coverCandidates(d *Doc, traditional []string) []string {
	bases := append(variants(string(d.Title), ""), variants(string(d.Album), "")...)
	bases = append(bases, traditional...)
	var out []string
	for _, suffix := range []string{"_300x300", ""} {
		for _, b := range bases {
			for _, ext := range imageExts {
				out = append(out, b+suffix+ext)
			}
		}
	}
	return out
}

func variants(name, ext string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if u := strings.ReplaceAll(name, " ", "_"); u != name {
		return []string{name + ext, u + ext}
	}
	return []string{name + ext}
}

// find returns the first file, case-insensitively, matching an exact name,
// then one containing a keyword (preferring existing _300x300 copies).
func find(dirs, exact, keywords, exts []string) string {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var files []string
		for _, e := range entries {
			if e.Type().IsRegular() && hasExt(e.Name(), exts) {
				files = append(files, e.Name())
			}
		}
		sort.SliceStable(files, func(i, j int) bool {
			return strings.Contains(strings.ToLower(files[i]), "_300x300") && !strings.Contains(strings.ToLower(files[j]), "_300x300")
		})
		for _, want := range exact {
			for _, f := range files {
				if strings.EqualFold(f, want) {
					return filepath.Join(dir, f)
				}
			}
		}
		for _, k := range keywords {
			for _, f := range files {
				if strings.Contains(strings.ToLower(f), strings.ToLower(k)) {
					return filepath.Join(dir, f)
				}
			}
		}
	}
	return ""
}

func hasExt(name string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// coverPicture loads an image, scales it to 300x300 when needed and saves
// that copy next to the source as <name>_300x300.<ext>.
func coverPicture(path string, dryRun bool) (Picture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Picture{}, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Picture{}, err
	}
	b := img.Bounds()
	if b.Dx() == coverSize && b.Dy() == coverSize && hasExt(path, imageExts) {
		mime := "image/jpeg"
		if hasExt(path, []string{".png"}) {
			mime = "image/png"
		}
		return Picture{3, mime, "Auto-found cover art", raw}, nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, coverSize, coverSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)

	var buf bytes.Buffer
	mime, ext := "image/jpeg", ".jpg"
	if dst.Opaque() {
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90})
	} else {
		mime, ext = "image/png", ".png"
		err = png.Encode(&buf, dst)
	}
	if err != nil {
		return Picture{}, err
	}
	if !dryRun {
		base := strings.TrimSuffix(path, filepath.Ext(path))
		if err := os.WriteFile(base+"_300x300"+ext, buf.Bytes(), 0o644); err != nil {
			return Picture{}, err
		}
	}
	return Picture{3, mime, "Auto-found cover art", buf.Bytes()}, nil
}
