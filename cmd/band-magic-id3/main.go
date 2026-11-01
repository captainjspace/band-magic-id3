// Command band-magic-id3 merges band/album/song layers and tags MP3s.
//
//	band-magic-id3 read  [-yaml] file.mp3...
//	band-magic-id3 merge [-o out.json] band.yaml album.yaml song.yaml
//	band-magic-id3 write -mp3 song.mp3 [-cover img] [-lyrics txt] [-keep] [-dry-run] band.yaml album.yaml song.yaml
//
// Flags go before the layer files. Paths are taken as given, so names with
// spaces or ? need only normal shell quoting.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/captainjspace/band-magic-id3/id3"
	"gopkg.in/yaml.v3"
)

var commands = map[string]func([]string) error{
	"read":  read,
	"merge": merge,
	"write": write,
}

func main() {
	if len(os.Args) < 2 || commands[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: band-magic-id3 read|merge|write [flags] [files]")
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
		d, err := id3.ReadMP3(f)
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
	mp3 := fs.String("mp3", "", "MP3 to tag (required)")
	var opt id3.Options
	fs.StringVar(&opt.Cover, "cover", "", "cover image (default: search covers/ and the MP3 folder)")
	fs.StringVar(&opt.Lyrics, "lyrics", "", "lyrics .txt (default: search lyrics/ and the MP3 folder)")
	fs.BoolVar(&opt.Keep, "keep", false, "keep frames this tool doesn't manage (default clears them)")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "resolve and find assets, print the doc, write nothing")
	fs.Parse(args)
	if *mp3 == "" || fs.NArg() == 0 {
		return fmt.Errorf("write needs -mp3 and at least one layer file")
	}

	d, err := id3.Resolve(fs.Args()...)
	if err != nil {
		return err
	}
	r, err := id3.Tag(*mp3, d, opt)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "cover:  %s\nlyrics: %s\n", orNone(r.Cover), orNone(r.Lyrics))
	if opt.DryRun {
		return emit(withoutImageData(r.Written), false)
	}
	fmt.Fprintf(os.Stderr, "tagged: %s\nread-back: %s\n", *mp3, r.ReadBackPath)
	return emit(withoutImageData(r.ReadBack), false)
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
