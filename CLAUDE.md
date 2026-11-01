# band-magic-id3

Go port of the Python mutagen tagger in `../yaml-id3`
(`id3-tag-manager.py` + `yaml-merge.py`). Independent package; the band-magic
app (`../../rollingblackoutapp`, Next.js) integrates via the CLI (JSON on stdout)
and later an HTTP API built on the same core.

## Behavior reference
The Python tool's real runs (`../example/`, `../yaml-id3/`, local only, to be
discarded) define the behavior. This repo is PUBLIC: no real recordings,
ISRCs, UPCs, names or release links — tests use placeholders only.

`TestGoldenAgainstPython` is the cross-check: `id3/testdata/song.yaml` (fake
song) tagged onto a generated silent MP3, compared with
`id3/testdata/python-tagged.mp3` — the same input tagged by
`id3-tag-manager.py` — via `ReadMP3` and `exiftool -json`. Intended
differences: Go writes lyrics as plain text (Python stored the list repr
`['…\n…']`), and also writes TDRC year alongside TDRL release date.

## Layout
- `id3/doc.go` — `Doc`, the resolved tag document (snake_case JSON keys = old YAML keys)
- `id3/layers.go` — load YAML/JSON layers, `Merge` (= `deep_merge`: later wins, maps merge recursively), `Resolve`
- `id3/mp3.go` — `ReadMP3` / `WriteMP3` (ID3v2.4, bogem/id3v2); `textFrames` table maps fields → frames
- `id3/assets.go` — cover/lyrics lookup + 300x300 resize
- `id3/tag.go` — pipeline: backup → assets → write → read back → `ID3-<name>.json` → archive
- `cmd/band-magic-id3` — `read`, `merge`, `write`

## Decisions
- Flow: band → album → song (→ track override, later) → cover/lyrics → write → read back.
- Merge default is override; per-field rules may come later — keep them table-driven.
- Credits are role → list of names. Repeated YAML keys collect into a list; commas
  split names except inside parentheses.
- A layer that is missing or won't parse is an error, never skipped.
- Cover: `-cover` wins; else `covers/` then MP3 folder, song/album names, then
  cover/folder/albumart. Always replaces cover_art. Lyrics only if the doc has none.
- Writes clear existing frames by default (old `--clear`); `-keep` to preserve.
- MP3 first. M4A next (iTunes atoms via go-mp4tag, same Doc, per-format field table) —
  Logic bounces WAV+MP3+M4A together; WAV is not tagged.
- YAML is an authoring/ingest format; JSON is internal. Later: Firestore stores
  layer docs; releases exported → signed → committed.

## Commands
```
go test ./...                        # self-contained; exiftool check runs if installed
go build ./cmd/band-magic-id3
band-magic-id3 write -mp3 "Track 3/Song Name?.mp3" band.yaml album.yaml song.yaml
band-magic-id3 read -yaml some.mp3
```
`exiftool -json` is the independent check (installed via Homebrew).

## Milestones
1. Working port (this) — next: song/album discovery flags (`-album-dir`, `-song`, `-all`), `init` for covers/lyrics dirs, M4A
2. HTTP API  3. UI + Firestore storage  4. Integration in band-magic
