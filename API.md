> **SUPERSEDED (2026-11-01)** — describes the GCS-path design. The service now
> takes a file and returns the tagged file, running in a container; see CLAUDE.md
> "Seam" and "Paused". A new strict OpenAPI spec replaces this.

# band-magic-id3 HTTP API — v1

The tagging engine as a service. It reads, merges, tags and archives audio files in
object storage (GCS) or a local folder, and returns JSON. It does **not** store
documents, users or UI state — the caller (the band-magic app) owns Firestore and
persists what this API returns.

- Base URL: `ID3_SERVICE_URL` (Cloud Run, private). Callers send a Google ID token
  for that URL as `Authorization: Bearer <token>` (Cloud Run IAM checks it; the
  service itself does no auth).
- All bodies are JSON. All paths are **object paths inside the bucket** (the same
  string the app stores as `catalog.path`), e.g. `2026/Where Are You/Where Are You?.mp3`.
  Absolute paths and `..` are rejected. The service may restrict paths to a prefix.
- Formats: `.mp3` (ID3v2.4) and `.m4a` (iTunes atoms). Files above the service's
  size limit get **413** — tag those with the CLI and upload the tagged file.

## Doc
A tag document is a JSON object with the keys below (snake_case; same keys as the
YAML layer files). All optional. Image bytes are **never** included in responses
except `/v1/read` with `images: true`.

```json
{
  "title": "Test Song", "artist": "Test Band", "album": "Test Album",
  "album_artist": "Test Band", "genre": "Rock", "year": "2025",
  "release_date": "2025-06-22", "track_number": 3, "track_total": 10,
  "disc_number": 1, "disc_total": 1, "composer": "…", "lyricist": "…",
  "isrc": "…", "copyright": "…", "publisher": "…", "encoded_by": "…",
  "encoder_settings": "…", "bpm": "153",
  "title_sort": "…", "artist_sort": "…", "album_sort": "…",
  "album_artist_sort": "…", "composer_sort": "…", "grouping": "…", "subtitle": "…",
  "website": "https://…", "album_website": "https://…",
  "comment": "…", "comments": [{"language": "eng", "description": "", "text": "…"}],
  "lyrics": "…", "lyrics_sections": [{"language": "eng", "description": "Full Lyrics", "text": "…"}],
  "cover_art": [{"type": 3, "mime_type": "image/jpeg", "description": "…"}],
  "involved_people": {"producer": ["…"], "recording_engineer": ["…", "…"]},
  "musicians": {"vocals": ["…"], "guitar": ["…", "…"]},
  "unique_file_identifiers": [{"owner_identifier": "…", "identifier": "<hex>"}],
  "custom_tags": {"Original Filename": "…", "Lyrics Link": "https://…"},
  "private_frames": [{"owner_identifier": "…", "data_hex": "…"}]
}
```
Input is lenient (numbers or strings for `year`/`bpm`, `"A, B"` or `["A","B"]` for
people, `"Front Cover"` or `3` for picture type, an `audio_metadata` wrapper is
unwrapped). Output is always the canonical form above.

## Layers
`layers` is an ordered list of partial Docs — band, album, song, track override.
Later layers win; nested maps (`involved_people`, `musicians`, `custom_tags`) merge
key by key; lists and scalars are replaced.

## Endpoints

### `GET /healthz` → `200 ok`

### `POST /v1/merge`
Pure — no storage access.
```json
{"layers": [{"artist": "Test Band"}, {"album": "Test Album"}, {"title": "Test Song"}]}
```
→ `200 {"doc": {"title": "Test Song", "artist": "Test Band", "album": "Test Album"}}`

### `POST /v1/read`
```json
{"path": "2026/Test Song/Test Song.mp3", "images": false}
```
→ `200 {"path": "…", "format": "mp3", "generation": 1730501234567890, "doc": {…}}`

### `POST /v1/tag`
```json
{
  "path": "2026/Test Song/Test Song.mp3",
  "layers": [{…band}, {…album}, {…song}],
  "cover": "2026/Test Song/covers/test-song-cover.jpg",
  "lyrics": "2026/Test Song/lyrics/Test Song.txt",
  "m4a": true,
  "keep": false,
  "dry_run": false
}
```
- `layers` or `doc` (an already-merged Doc) — one is required.
- `cover` / `lyrics`: optional object paths. If omitted, the service looks beside
  the file the way the CLI does: `<folder>/covers/` then `<folder>/`, matching the
  song title, album title, then `cover` / `folder` / `albumart` (lyrics: `<title>.txt`
  or a file containing `lyrics`). Lyrics are only added if the doc has none.
- `m4a` (default `true`): also tag the `.m4a` with the same name beside the file.
- `keep` (default `false`): keep existing frames this tool doesn't manage.
- `dry_run`: resolve and find assets, write nothing.

Per file the service: downloads it → reads current tags (`before`) → embeds cover
(scaled to 300×300) and lyrics → writes → reads back → **replaces the object** only if
it hasn't changed since download (generation precondition) → writes beside it
`ID3-<name>.json` (read-back), `song-id3-architect.tar.gz` (dated backups of
before + supplied docs, appended) and the `_300x300` cover copy.

→ `200`
```json
{"results": [{
  "path": "2026/Test Song/Test Song.mp3",
  "format": "mp3",
  "generation": 1730501299999999,
  "before":    {…},
  "applied":   {…},
  "read_back": {…},
  "cover":  {"path": "2026/Test Song/covers/test-song-cover.jpg", "sha256": "…"},
  "lyrics": "2026/Test Song/lyrics/Test Song.txt",
  "read_back_path": "2026/Test Song/ID3-Test Song.json",
  "archive": "2026/Test Song/song-id3-architect.tar.gz"
}, {"path": "2026/Test Song/Test Song.m4a", "format": "m4a", …}]}
```
`generation` is the new object generation (GCS) — store it to detect later changes.
`cover` is `null` and `lyrics` is `""` when none were used.

## Errors
`{"error": "message"}` with:
| Status | When |
|---|---|
| 400 | bad JSON, no layers/doc, a layer that doesn't parse, absolute or `..` path, unsupported extension |
| 404 | the file (or a named cover/lyrics object) doesn't exist |
| 409 | the file changed between download and replace — re-read and retry |
| 413 | file larger than the service limit — tag with the CLI and upload |
| 500 | anything else |

## For the app (persistence is the caller's job)
Suggested Firestore shape, keyed like `catalog` (`encodeURIComponent(path)`):
```
id3/{catalogId}: {path, generation, format, applied, cover, lyrics, taggedAt, taggedBy}
id3/{catalogId}/history/{stamp}: {before, applied}
```
