package id3

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Upgrade brings an MP3's tag up to ID3v2.4 in place, keeping its values —
// mutagen's update_to_v24, which the Python tool ran on every save. The tags
// before are archived first. It returns the version the file had: 0 (no tag)
// and 4 leave the file untouched, as does dryRun.
func Upgrade(path string, dryRun bool) (byte, error) {
	from, err := tagVersion(path)
	if err != nil || from == 0 || from == 4 || dryRun {
		return from, err
	}
	before, err := ReadMP3(path)
	if err != nil {
		return from, err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	stamp := time.Now().Format("20060102_150405")
	if err := AppendArchive(filepath.Join(filepath.Dir(path), ArchiveName),
		map[string][]byte{base + "_backup_" + stamp + ".json": mustJSON(before)}); err != nil {
		return from, err
	}

	tag, err := openMP3(path, true) // v2.2 is rewritten as v2.3 here
	if err != nil {
		return from, err
	}
	defer tag.Close()
	// v2.3 dates: TYER (+ TDAT as DDMM) → TDRC, TORY → TDOR; the rest of the
	// v2.3-only date frames have no v2.4 form and are dropped.
	if y := frameText(tag, "TYER"); y != "" && frameText(tag, "TDRC") == "" {
		if dm := frameText(tag, "TDAT"); len(dm) == 4 {
			y += "-" + dm[2:] + "-" + dm[:2]
		}
		setText(tag, "TDRC", y)
	}
	if y := frameText(tag, "TORY"); y != "" && frameText(tag, "TDOR") == "" {
		setText(tag, "TDOR", y)
	}
	for _, id := range []string{"TYER", "TDAT", "TIME", "TRDA", "TSIZ", "TORY"} {
		tag.DeleteFrames(id)
	}
	tag.SetVersion(4)
	return from, tag.Save()
}

// tagVersion is the ID3v2 major version in path's header, 0 if it has none.
func tagVersion(path string) (byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var h [4]byte
	if _, err := io.ReadFull(f, h[:]); err != nil || string(h[:3]) != "ID3" {
		return 0, nil
	}
	return h[3], nil
}
