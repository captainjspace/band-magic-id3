// Command band-magic-id3 merges band/album/song layers and tags MP3s, plus
// the M4A Logic bounces beside each one (-m4a=false to skip).
//
//	band-magic-id3 read  [-yaml] file.mp3|file.m4a...
//	band-magic-id3 merge [-o out.json] band.yaml album.yaml song.yaml
//	band-magic-id3 write -mp3 song.mp3 [-cover img] [-lyrics txt] [-keep] [-dry-run] band.yaml album.yaml song.yaml
//	band-magic-id3 write -album-dir DIR -song "where are you" -songs songs/ band.yaml album.yaml
//	band-magic-id3 write -album-dir DIR -all -songs songs/ band.yaml album.yaml
//	band-magic-id3 init [-dry-run] DIR   # covers/ + lyrics/ per track, loose images → covers/
//
// Flags go before the layer files. Paths are taken as given, so names with
// spaces or ? need only normal shell quoting.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/captainjspace/band-magic-id3/id3"
	"gopkg.in/yaml.v3"
)

var commands = map[string]func([]string) error{
	"read":  read,
	"merge": merge,
	"write": write,
	"init":  initCmd,
}

func main() {
	if len(os.Args) < 2 || commands[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: band-magic-id3 read|merge|write|init [flags] [files]")
		os.Exit(2)
	}
	if err := commands[os.Args[1]](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func read(args []string) error {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	asYAML := fs.Bool("yaml", false, "print YAML under audio_metadata (Python tool format)")
	fs.Parse(args)
	for _, f := range fs.Args() {
		d, err := id3.Read(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		if err := emit(d, *asYAML); err != nil {
			return err
		}
	}
	return nil
}

func merge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	out := fs.String("o", "", "write merged JSON here instead of stdout")
	fs.Parse(args)
	d, err := id3.Resolve(fs.Args()...)
	if err != nil {
		return err
	}
	if *out != "" {
		b, _ := json.MarshalIndent(d, "", "  ")
		return os.WriteFile(*out, append(b, '\n'), 0o644)
	}
	return emit(d, false)
}

func write(args []string) error {
	fs := flag.NewFlagSet("write", flag.ExitOnError)
	mp3 := fs.String("mp3", "", "MP3 to tag")
	albumDir := fs.String("album-dir", "", "album folder to search, with -song or -all")
	song := fs.String("song", "", "song to find under -album-dir (exact name wins, else a unique prefix)")
	all := fs.Bool("all", false, "tag every MP3 under -album-dir that has a song layer in -songs")
	songs := fs.String("songs", "", "folder of song layers; the one matching the MP3 name is added last")
	withM4A := fs.Bool("m4a", true, "also tag the .m4a with the same name beside each MP3")
	var opt id3.Options
	fs.StringVar(&opt.Cover, "cover", "", "cover image (default: search covers/ and the MP3 folder)")
	fs.StringVar(&opt.Lyrics, "lyrics", "", "lyrics .txt (default: search lyrics/ and the MP3 folder)")
	fs.BoolVar(&opt.Keep, "keep", false, "keep frames this tool doesn't manage (default clears them)")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "resolve and find assets, print the doc, write nothing")
	fs.Parse(args)
	if *all && *songs == "" {
		return fmt.Errorf("-all needs -songs to pick each track's song layer")
	}

	files, err := targets(*mp3, *albumDir, *song, *all)
	if err != nil {
		return err
	}
	var failed int
	for _, f := range files {
		layers := fs.Args()
		if *songs != "" {
			l, err := id3.FindLayer(*songs, strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
			if err != nil && *all {
				fmt.Fprintln(os.Stderr, "skip:", err)
				continue
			}
			if err != nil {
				return err
			}
			layers = append(layers, l)
		}
		if len(layers) == 0 {
			return fmt.Errorf("no layer files given")
		}
		err := tagOne(f, layers, opt, len(files) == 1)
		if sib := strings.TrimSuffix(f, filepath.Ext(f)) + ".m4a"; err == nil && *withM4A && f != sib && exists(sib) {
			err = tagOne(sib, layers, opt, false)
		}
		if err != nil && !*all {
			return fmt.Errorf("%s: %w", f, err)
		}
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", f, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d tracks failed", failed, len(files))
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func targets(mp3, albumDir, song string, all bool) ([]string, error) {
	switch {
	case mp3 != "":
		return []string{mp3}, nil
	case albumDir == "" || (song == "") == !all:
		return nil, fmt.Errorf("give -mp3, or -album-dir with one of -song / -all")
	}
	files, err := id3.AlbumFiles(albumDir, ".mp3")
	if err != nil || all {
		return files, err
	}
	f, err := id3.FindSong(files, song)
	return []string{f}, err
}

func tagOne(mp3 string, layers []string, opt id3.Options, show bool) error {
	d, err := id3.Resolve(layers...)
	if err != nil {
		return err
	}
	if d.TrackNumber == 0 {
		d.TrackNumber = id3.TrackFromDir(mp3)
	}
	r, err := id3.Tag(mp3, d, opt)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s\n  layers: %s\n  cover:  %s\n  lyrics: %s\n", mp3, strings.Join(layers, ", "), orNone(r.Cover), orNone(r.Lyrics))
	switch {
	case opt.DryRun && show:
		return emit(withoutImageData(r.Written), false)
	case opt.DryRun:
		return nil
	}
	fmt.Fprintf(os.Stderr, "  read-back: %s\n", r.ReadBackPath)
	if show {
		return emit(withoutImageData(r.ReadBack), false)
	}
	return nil
}

func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "print what would change")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("init takes one album folder")
	}
	log, err := id3.InitAlbum(fs.Arg(0), *dryRun)
	for _, l := range log {
		fmt.Println(l)
	}
	return err
}

func emit(d id3.Doc, asYAML bool) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if !asYAML {
		_, err = fmt.Printf("%s\n", b)
		return err
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	return yaml.NewEncoder(os.Stdout).Encode(map[string]any{"audio_metadata": m})
}

// withoutImageData keeps terminal output readable; the read-back file has it all.
func withoutImageData(d id3.Doc) id3.Doc {
	pics := make([]id3.Picture, len(d.CoverArt))
	for i, p := range d.CoverArt {
		p.Description = fmt.Sprintf("%s (%d bytes)", p.Description, len(p.Data))
		p.Data = nil
		pics[i] = p
	}
	d.CoverArt = pics
	return d
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
