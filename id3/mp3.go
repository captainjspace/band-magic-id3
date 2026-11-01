package id3

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
)

var utf8 = id3v2.EncodingUTF8

// textFrames maps plain Doc fields to ID3v2.4 text frames (and the two URL
// frames, which carry a bare URL with no encoding byte).
var textFrames = []struct {
	id  string
	get func(*Doc) *Text
}{
	{"TIT2", func(d *Doc) *Text { return &d.Title }},
	{"TPE1", func(d *Doc) *Text { return &d.Artist }},
	{"TALB", func(d *Doc) *Text { return &d.Album }},
	{"TPE2", func(d *Doc) *Text { return &d.AlbumArtist }},
	{"TCON", func(d *Doc) *Text { return &d.Genre }},
	{"TDRC", func(d *Doc) *Text { return &d.Year }},
	{"TDRL", func(d *Doc) *Text { return &d.ReleaseDate }},
	{"TCOM", func(d *Doc) *Text { return &d.Composer }},
	{"TEXT", func(d *Doc) *Text { return &d.Lyricist }},
	{"TSRC", func(d *Doc) *Text { return &d.ISRC }},
	{"TCOP", func(d *Doc) *Text { return &d.Copyright }},
	{"TPUB", func(d *Doc) *Text { return &d.Publisher }},
	{"TENC", func(d *Doc) *Text { return &d.EncodedBy }},
	{"TSSE", func(d *Doc) *Text { return &d.EncoderSettings }},
	{"TBPM", func(d *Doc) *Text { return &d.BPM }},
	{"TSOT", func(d *Doc) *Text { return &d.TitleSort }},
	{"TSOP", func(d *Doc) *Text { return &d.ArtistSort }},
	{"TSOA", func(d *Doc) *Text { return &d.AlbumSort }},
	{"TSO2", func(d *Doc) *Text { return &d.AlbumArtistSort }},
	{"TSOC", func(d *Doc) *Text { return &d.ComposerSort }},
	{"TIT1", func(d *Doc) *Text { return &d.Grouping }},
	{"TIT3", func(d *Doc) *Text { return &d.Subtitle }},
	{"WOAR", func(d *Doc) *Text { return &d.Website }},
	{"WOAS", func(d *Doc) *Text { return &d.AlbumWebsite }},
}

// Frames this package owns; on write they are always replaced, even
// without clear, so stale values never survive.
var managed = []string{"TRCK", "TPOS", "TYER", "COMM", "USLT", "APIC", "TIPL", "TMCL", "TPRO", "TPE4", "UFID", "TXXX", "PRIV"}

// Roles with their own single-value frame instead of a TIPL entry.
var dedicatedRoles = map[string]string{"producer": "TPRO", "remixer": "TPE4"}

// ReadMP3 returns the tags on an MP3 as a Doc (zero Doc if untagged).
func ReadMP3(path string) (Doc, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return Doc{}, err
	}
	defer tag.Close()

	var d Doc
	for _, f := range textFrames {
		*f.get(&d) = Text(frameText(tag, f.id))
	}
	if d.Year == "" {
		d.Year = Text(frameText(tag, "TYER"))
	}
	d.TrackNumber, d.TrackTotal = splitPos(frameText(tag, "TRCK"))
	d.DiscNumber, d.DiscTotal = splitPos(frameText(tag, "TPOS"))

	for _, f := range tag.GetFrames("COMM") {
		if c, ok := f.(id3v2.CommentFrame); ok {
			d.Comments = append(d.Comments, LangText{c.Language, c.Description, c.Text})
		}
	}
	for _, f := range tag.GetFrames("USLT") {
		if l, ok := f.(id3v2.UnsynchronisedLyricsFrame); ok {
			d.LyricsSections = append(d.LyricsSections, LangText{l.Language, l.ContentDescriptor, l.Lyrics})
		}
	}
	// Same shape as the Python read-back: a lone plain English entry collapses.
	d.Comment, d.Comments = collapse(d.Comments)
	d.Lyrics, d.LyricsSections = collapse(d.LyricsSections)

	for _, f := range tag.GetFrames("APIC") {
		if p, ok := f.(id3v2.PictureFrame); ok {
			d.CoverArt = append(d.CoverArt, Picture{PicType(p.PictureType), p.MimeType, p.Description, p.Picture})
		}
	}

	d.InvolvedPeople = readPeople(frameText(tag, "TIPL"))
	for role, id := range dedicatedRoles {
		if name := frameText(tag, id); name != "" {
			d.InvolvedPeople = addPerson(d.InvolvedPeople, role, name)
		}
	}
	d.Musicians = readPeople(frameText(tag, "TMCL"))

	for _, f := range tag.GetFrames("UFID") {
		if u, ok := f.(id3v2.UFIDFrame); ok {
			d.UniqueFileIDs = append(d.UniqueFileIDs, UFID{u.OwnerIdentifier, hex.EncodeToString(u.Identifier)})
		}
	}
	for _, f := range tag.GetFrames("TXXX") {
		if u, ok := f.(id3v2.UserDefinedTextFrame); ok && u.Description != "" {
			if d.CustomTags == nil {
				d.CustomTags = map[string]Text{}
			}
			d.CustomTags[u.Description] = Text(strings.TrimSpace(u.Value))
		}
	}
	for _, f := range tag.GetFrames("PRIV") {
		if u, ok := f.(id3v2.UnknownFrame); ok {
			owner, data, _ := strings.Cut(string(u.Body), "\x00")
			d.PrivateFrames = append(d.PrivateFrames, Private{owner, hex.EncodeToString([]byte(data))})
		}
	}
	return d, nil
}

// WriteMP3 writes d as an ID3v2.4 tag. clear drops every existing frame
// first (the Python tool's --clear); otherwise only managed frames change.
func WriteMP3(path string, d Doc, clear bool) error {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return err
	}
	defer tag.Close()
	tag.SetVersion(4)
	tag.SetDefaultEncoding(utf8)
	if clear {
		tag.DeleteAllFrames()
	}
	for _, id := range managed {
		tag.DeleteFrames(id)
	}

	for _, f := range textFrames {
		tag.DeleteFrames(f.id)
		if v := strings.TrimSpace(string(*f.get(&d))); v != "" {
			setText(tag, f.id, v)
		}
	}
	setText(tag, "TRCK", joinPos(d.TrackNumber, d.TrackTotal))
	setText(tag, "TPOS", joinPos(d.DiscNumber, d.DiscTotal))

	for _, c := range texts(d.Comments, d.Comment) {
		tag.AddCommentFrame(id3v2.CommentFrame{Encoding: utf8, Language: c.Language, Description: c.Description, Text: c.Text})
	}
	for _, l := range texts(d.LyricsSections, d.Lyrics) {
		tag.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{Encoding: utf8, Language: l.Language, ContentDescriptor: l.Description, Lyrics: l.Text})
	}
	for _, p := range d.CoverArt {
		if len(p.Data) > 0 {
			tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: utf8, MimeType: p.MimeType, PictureType: byte(p.Type), Description: p.Description, Picture: p.Data})
		}
	}

	people := map[string]People{}
	for role, names := range d.InvolvedPeople {
		if id, ok := dedicatedRoles[strings.ToLower(role)]; ok && len(names) > 0 {
			setText(tag, id, names[0])
			continue
		}
		people[role] = names
	}
	setText(tag, "TIPL", writePeople(people))
	setText(tag, "TMCL", writePeople(d.Musicians))

	for _, u := range d.UniqueFileIDs {
		id, err := hex.DecodeString(u.Identifier)
		if err != nil {
			return fmt.Errorf("unique_file_identifiers %q: %w", u.Owner, err)
		}
		tag.AddUFIDFrame(id3v2.UFIDFrame{OwnerIdentifier: u.Owner, Identifier: id})
	}
	for desc, v := range d.CustomTags {
		if v != "" {
			tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: utf8, Description: desc, Value: string(v)})
		}
	}
	for _, p := range d.PrivateFrames {
		data, err := hex.DecodeString(p.DataHex)
		if err != nil {
			return fmt.Errorf("private_frames %q: %w", p.Owner, err)
		}
		tag.AddFrame("PRIV", id3v2.UnknownFrame{Body: append([]byte(p.Owner+"\x00"), data...)})
	}
	return tag.Save()
}

func setText(tag *id3v2.Tag, id, v string) {
	switch {
	case v == "":
	case id[0] == 'W':
		tag.AddFrame(id, id3v2.UnknownFrame{Body: []byte(v)})
	default:
		tag.AddTextFrame(id, utf8, v)
	}
}

func frameText(tag *id3v2.Tag, id string) string {
	switch f := tag.GetLastFrame(id).(type) {
	case id3v2.TextFrame:
		return strings.TrimRight(f.Text, "\x00")
	case id3v2.UnknownFrame:
		return strings.TrimRight(string(f.Body), "\x00")
	}
	return ""
}

func splitPos(s string) (Int, Int) {
	n, total, _ := strings.Cut(s, "/")
	a, _ := strconv.Atoi(strings.TrimSpace(n))
	b, _ := strconv.Atoi(strings.TrimSpace(total))
	return Int(a), Int(b)
}

func joinPos(n, total Int) string {
	switch {
	case n == 0:
		return ""
	case total == 0:
		return strconv.Itoa(int(n))
	}
	return fmt.Sprintf("%d/%d", n, total)
}

// texts prefers the list form; a lone string becomes one English entry.
func texts(list []LangText, single Text) []LangText {
	if len(list) > 0 {
		for i := range list {
			if list[i].Language == "" {
				list[i].Language = "eng"
			}
		}
		return list
	}
	if single == "" {
		return nil
	}
	return []LangText{{"eng", "", string(single)}}
}

func collapse(list []LangText) (Text, []LangText) {
	if len(list) == 1 && list[0].Description == "" && list[0].Language == "eng" {
		return Text(list[0].Text), nil
	}
	return "", list
}

// TIPL/TMCL bodies are alternating role/name strings separated by NUL.
// Each person is their own pair, so repeated roles keep every name.
func writePeople(m map[string]People) string {
	roles := make([]string, 0, len(m))
	for r := range m {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	var parts []string
	for _, r := range roles {
		title := titleCase(strings.ReplaceAll(r, "_", " "))
		for _, name := range m[r] {
			parts = append(parts, title, name)
		}
	}
	return strings.Join(parts, "\x00")
}

func readPeople(s string) map[string]People {
	parts := strings.Split(s, "\x00")
	var m map[string]People
	for i := 0; i+1 < len(parts); i += 2 {
		role := strings.ReplaceAll(strings.ToLower(parts[i]), " ", "_")
		for _, name := range strings.Split(parts[i+1], "/") {
			m = addPerson(m, role, strings.TrimSpace(name))
		}
	}
	return m
}

func addPerson(m map[string]People, role, name string) map[string]People {
	if name == "" {
		return m
	}
	if m == nil {
		m = map[string]People{}
	}
	m[role] = append(m[role], name)
	return m
}

func titleCase(s string) string {
	f := strings.Fields(s)
	for i, w := range f {
		f[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(f, " ")
}
