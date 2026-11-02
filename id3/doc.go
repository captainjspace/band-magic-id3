// Package id3 is the tagging core: resolve band/album/song layers into one
// Doc, find cover art and lyrics, write the tags, read them back.
package id3

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Doc is the resolved tag document. JSON keys match the original Python
// tool's YAML keys, so existing layer files load unchanged.
type Doc struct {
	Title           Text `json:"title,omitempty"`
	Artist          Text `json:"artist,omitempty"`
	Album           Text `json:"album,omitempty"`
	AlbumArtist     Text `json:"album_artist,omitempty"`
	Genre           Text `json:"genre,omitempty"`
	Year            Text `json:"year,omitempty"`
	ReleaseDate     Text `json:"release_date,omitempty"`
	TrackNumber     Int  `json:"track_number,omitempty"`
	TrackTotal      Int  `json:"track_total,omitempty"`
	DiscNumber      Int  `json:"disc_number,omitempty"`
	DiscTotal       Int  `json:"disc_total,omitempty"`
	Composer        Text `json:"composer,omitempty"`
	Lyricist        Text `json:"lyricist,omitempty"`
	ISRC            Text `json:"isrc,omitempty"`
	Copyright       Text `json:"copyright,omitempty"`
	Publisher       Text `json:"publisher,omitempty"`
	EncodedBy       Text `json:"encoded_by,omitempty"`
	EncoderSettings Text `json:"encoder_settings,omitempty"`
	BPM             Text `json:"bpm,omitempty"`
	TitleSort       Text `json:"title_sort,omitempty"`
	ArtistSort      Text `json:"artist_sort,omitempty"`
	AlbumSort       Text `json:"album_sort,omitempty"`
	AlbumArtistSort Text `json:"album_artist_sort,omitempty"`
	ComposerSort    Text `json:"composer_sort,omitempty"`
	Grouping        Text `json:"grouping,omitempty"`
	Subtitle        Text `json:"subtitle,omitempty"`
	Website         Text `json:"website,omitempty"`
	AlbumWebsite    Text `json:"album_website,omitempty"`

	Comment        Text       `json:"comment,omitempty"`
	Comments       []LangText `json:"comments,omitempty"`
	Lyrics         Text       `json:"lyrics,omitempty"`
	LyricsSections []LangText `json:"lyrics_sections,omitempty"`
	CoverArt       []Picture  `json:"cover_art,omitempty"`

	InvolvedPeople map[string]People `json:"involved_people,omitempty"`
	Musicians      map[string]People `json:"musicians,omitempty"`

	UniqueFileIDs []UFID          `json:"unique_file_identifiers,omitempty"`
	CustomTags    map[string]Text `json:"custom_tags,omitempty"`
	PrivateFrames []Private       `json:"private_frames,omitempty"`
}

type LangText struct {
	Language    string `json:"language,omitempty"`
	Description string `json:"description"`
	Text        string `json:"text"`
}

// Picture data is []byte, so JSON carries it as base64 like the Python tool did.
type Picture struct {
	Type        PicType `json:"type"`
	MimeType    string  `json:"mime_type,omitempty"`
	Description string  `json:"description"`
	Data        []byte  `json:"data,omitempty"`
}

type UFID struct {
	Owner      string `json:"owner_identifier"`
	Identifier Text   `json:"identifier"` // hex; a bare YAML number is fine
}

type Private struct {
	Owner   string `json:"owner_identifier"`
	DataHex string `json:"data_hex"`
}

// Text accepts a JSON string, number or bool (YAML `bpm: 110` vs `bpm: '153'`).
type Text string

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = Text(s)
		return nil
	}
	if string(b) != "null" {
		*t = Text(b)
	}
	return nil
}

// Int accepts a JSON number or numeric string.
type Int int

func (i *Int) UnmarshalJSON(b []byte) error {
	n, err := strconv.Atoi(strings.Trim(string(b), `"`))
	if err == nil {
		*i = Int(n)
	}
	return err
}

// People is one role's names. Accepts "A, B", ["A", "B"], or a mix —
// repeated YAML keys arrive as a list.
type People []string

func (p *People) UnmarshalJSON(b []byte) error {
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		list = []string{s}
	}
	*p = nil
	for _, s := range list {
		for _, name := range splitNames(s) {
			if name = strings.TrimSpace(name); name != "" {
				*p = append(*p, name)
			}
		}
	}
	return nil
}

// splitNames splits on commas outside parentheses, so
// "Person Two (Tracks 1,6,9), Person One" is two names.
func splitNames(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case r == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// PicType is the APIC picture type. Accepts 3, "Front Cover" or "COVER_FRONT".
type PicType byte

var picTypeNames = []string{
	"Other", "File Icon", "Other File Icon", "Front Cover", "Back Cover",
	"Leaflet Page", "Media", "Lead Artist", "Artist", "Conductor",
	"Band/Orchestra", "Composer", "Lyricist", "Recording Location",
	"During Recording", "During Performance", "Movie/Video Screen Capture",
	"A Bright Coloured Fish", "Illustration", "Band/Artist Logotype",
	"Publisher/Studio Logotype",
}

func (p *PicType) UnmarshalJSON(b []byte) error {
	var n int
	if json.Unmarshal(b, &n) == nil {
		*p = PicType(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*p = 3
	for i, name := range picTypeNames {
		if strings.EqualFold(s, name) || strings.EqualFold(s, mutagenPicName(name)) {
			*p = PicType(i)
		}
	}
	return nil
}

// mutagenPicName turns "Front Cover" into mutagen's "COVER_FRONT" style.
func mutagenPicName(name string) string {
	f := strings.Fields(strings.ToUpper(name))
	if len(f) == 2 {
		f[0], f[1] = f[1], f[0]
	}
	return strings.Join(f, "_")
}
