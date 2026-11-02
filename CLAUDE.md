# band-magic-id3

Go port of the Python mutagen tagger in `../yaml-id3`
(`id3-tag-manager.py` + `yaml-merge.py`). Independent package with a CLI and an
HTTP API (`API.md` is the contract) on the same core.

## Seam with the band-magic app (`../../rollingblackoutapp`) — decided 2026-11-01
- **This repo = tagging engine on a local block filesystem, run in a container.** It
  takes a file (plus layers, cover, lyrics), tags it on the container's filesystem,
  and returns the tagged file and JSON. It does
  **not** touch GCS, Firestore, users or UI.
- **The app's existing system** reads/writes GCS: it onboards a file, sends it here
  to be tagged, and passes the result on. It owns persistence (Firestore).
- The API contract must be strict and explicit (exact required fields, unknown
  fields rejected), with none of the CLI's conveniences (folder search, sibling
  .m4a, lenient input forms). `api/openapi.yaml` holds a superseded GCS-path draft
  whose Doc/Credits/LangText/PictureInfo/Error schemas carry over.

## Paused (resume here)
- `server/` + `cmd/band-magic-id3-api` still implement the old GCS-path design
  (`GCSStore`, `DirStore`, `/v1/tag` by path). To be reworked to file-in /
  tagged-file-out against a new strict OpenAPI spec; `GCSStore` goes away.
- Open review findings that only apply to that old server (archive-before-audio
  ordering, preconditions on new objects, MP3+M4A partial failure, root-dir asset
  search, asset-search memory, prefix boundary) are moot once it is replaced.
- Track number from a `Track N` folder is a CLI convenience; the API requires an
  explicit `track_number`.

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
- `id3/m4a.go` — `ReadM4A` / `WriteM4A` (iTunes atoms, go-mp4tag); fields with no atom go in `----:com.apple.iTunes:<NAME>` freeform atoms, credits as `CREDIT:<Role>` / `MUSICIAN:<Instrument>`
- `id3/discover.go` — album search (`album/` and `album/Track N/` only, skips temp/tmp), song and song-layer matching by normalized name, `Track N` → track number, `InitAlbum`
- `id3/tag.go` — pipeline (dispatches on .mp3/.m4a): backup → assets → write → read back → `ID3-<name>.json` (`ID3-<name>.m4a.json` for M4A) → archive
- `cmd/band-magic-id3` — `read`, `merge`, `write`, `init`
- `server/` — HTTP API: `Store` (`DirStore` local folder, `GCSStore`), handlers; each
  request mirrors the objects it needs into a temp dir and runs `id3.Tag` unchanged,
  then puts back the audio (precondition) and any new/changed files
- `cmd/band-magic-id3-api` — API binary (`BUCKET` or `-root`, `PORT`, `ALLOWED_PREFIX`, `MAX_MB`)
- `internal/testaudio` — generated silent MP3/M4A for tests

## Decisions
- Flow: band → album → song (→ track override, later) → cover/lyrics → write → read back.
- Merge default is override; per-field rules may come later — keep them table-driven.
- Credits are role → list of names. Repeated YAML keys collect into a list; commas
  split names except inside parentheses.
- A layer that is missing or won't parse is an error, never skipped.
- Cover: `-cover` wins; else `covers/` then MP3 folder, song/album names, then
  cover/folder/albumart. Always replaces cover_art. Lyrics only if the doc has none.
- Writes clear existing frames by default (old `--clear`); `-keep` to preserve.
- MP3 and M4A from the same Doc — Logic bounces WAV+MP3+M4A together; `write` tags
  the `.m4a` beside each MP3 (`-m4a=false` to skip). WAV is not tagged.
- Song lookup: exact normalized name wins, else a unique prefix; several matches is an
  error listing them. `-all` skips MP3s with no song layer (alternate takes) and keeps
  going past failures, reporting them at the end.
- YAML is an authoring/ingest format; JSON is internal. Later: Firestore stores
  layer docs; releases exported → signed → committed.

## go-mp4tag quirks (worked around)
- Its `Date` is never written (bug), so `©day` holds the year; the full release date is
  the `RELEASEDATE` freeform atom. With no year it writes an empty `©day`.
- `Year` is only applied when `Artist` is set.
- Writes sort atoms but doesn't read them back — `ReadM4A` omits sort fields.
- Needs an existing `ilst` box (Logic/afconvert files have one).
- Clearing would drop the encoder's `iTunSMPB`/`iTunNORM` (gapless/volume); `WriteM4A` carries them over.

## Commands
```
go test ./...                        # self-contained; exiftool/afconvert checks run if installed
go build ./cmd/band-magic-id3
band-magic-id3 write -mp3 "Track 3/Song Name?.mp3" band.yaml album.yaml song.yaml
band-magic-id3 write -album-dir "/Volumes/…/Album" -all -songs songs/ band.yaml album.yaml
band-magic-id3 write -album-dir "/Volumes/…/Album" -song "song name" -songs songs/ band.yaml album.yaml
band-magic-id3 init -dry-run "/Volumes/…/Album"     # covers/ + lyrics/ per track
band-magic-id3 read -yaml some.mp3
band-magic-id3-api -root "/Volumes/…/Audio"         # API over a local folder, :8080
BUCKET=<audio bucket> band-magic-id3-api            # API over GCS
```

`exiftool -json` is the independent check (installed via Homebrew).

## Deploy — home cluster (running since 2026-11-01)
MicroK8s, amd64 nodes; `k8s/base` (namespace `band-magic`, ClusterIP + Traefik
Ingress, wildcard TLS). `make local_deploy` uses the `k8s_*` vars from `.envrc`.
The `Dockerfile` cross-compiles via `TARGETARCH`, so builds on the Mac (arm64)
come out amd64. The pod runs the paused server with `-root /data` (emptyDir) until
the file-in/file-out rewrite. Locally: `docker run -p 8080:8080 <image> -root /data`.

## Deploy — Cloud Run (not done yet — needs the go-ahead)
Public repo: real project, bucket and service-account names are kept out of it.
Cloud Run `band-magic-id3` in the app's project, from the `Dockerfile`;
`--no-allow-unauthenticated`; env `BUCKET=<audio bucket>`, `ALLOWED_PREFIX`;
1 GiB memory (temp files are in RAM); its own service account with
`roles/storage.objectAdmin` on the audio bucket (which lives in a different
project — cross-project grant); `roles/run.invoker` for the app's runtime service account.

## Milestones
1. Working port — done: MP3 + M4A, album discovery, init. Possible next: `restore` from the archive, per-field merge rules, track overrides
2. HTTP API — built and tested locally (`API.md`); not deployed
3. UI + Firestore storage (app side)  4. Integration in band-magic
