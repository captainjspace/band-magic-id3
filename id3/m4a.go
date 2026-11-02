package id3

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Sorrow446/go-mp4tag"
)

// m4aText maps Doc fields to the iTunes atoms go-mp4tag exposes.
var m4aText = []struct {
	get func(*Doc) *Text
	set func(*mp4tag.MP4Tags) *string
}{
	{func(d *Doc) *Text { return &d.Title }, func(t *mp4tag.MP4Tags) *string { return &t.Title }},
	{func(d *Doc) *Text { return &d.Artist }, func(t *mp4tag.MP4Tags) *string { return &t.Artist }},
	{func(d *Doc) *Text { return &d.Album }, func(t *mp4tag.MP4Tags) *string { return &t.Album }},
	{func(d *Doc) *Text { return &d.AlbumArtist }, func(t *mp4tag.MP4Tags) *string { return &t.AlbumArtist }},
	{func(d *Doc) *Text { return &d.Genre }, func(t *mp4tag.MP4Tags) *string { return &t.CustomGenre }},
	{func(d *Doc) *Text { return &d.Composer }, func(t *mp4tag.MP4Tags) *string { return &t.Composer }},
	{func(d *Doc) *Text { return &d.Copyright }, func(t *mp4tag.MP4Tags) *string { return &t.Copyright }},
	{func(d *Doc) *Text { return &d.Publisher }, func(t *mp4tag.MP4Tags) *string { return &t.Publisher }},
	{func(d *Doc) *Text { return &d.Comment }, func(t *mp4tag.MP4Tags) *string { return &t.Comment }},
	{func(d *Doc) *Text { return &d.Lyrics }, func(t *mp4tag.MP4Tags) *string { return &t.Lyrics }},
	{func(d *Doc) *Text { return &d.TitleSort }, func(t *mp4tag.MP4Tags) *string { return &t.TitleSort }},
	{func(d *Doc) *Text { return &d.ArtistSort }, func(t *mp4tag.MP4Tags) *string { return &t.ArtistSort }},
	{func(d *Doc) *Text { return &d.AlbumSort }, func(t *mp4tag.MP4Tags) *string { return &t.AlbumSort }},
	{func(d *Doc) *Text { return &d.AlbumArtistSort }, func(t *mp4tag.MP4Tags) *string { return &t.AlbumArtistSort }},
	{func(d *Doc) *Text { return &d.ComposerSort }, func(t *mp4tag.MP4Tags) *string { return &t.ComposerSort }},
}

// m4aFreeform holds fields with no iTunes atom; they go in
// ----:com.apple.iTunes:<NAME> atoms, the same place Picard and iTunes use.
var m4aFreeform = []struct {
	name string
	get  func(*Doc) *Text
}{
	{"ISRC", func(d *Doc) *Text { return &d.ISRC }},
	{"LYRICIST", func(d *Doc) *Text { return &d.Lyricist }},
	{"RELEASEDATE", func(d *Doc) *Text { return &d.ReleaseDate }},
	{"ENCODEDBY", func(d *Doc) *Text { return &d.EncodedBy }},
	{"ENCODERSETTINGS", func(d *Doc) *Text { return &d.EncoderSettings }},
	{"GROUPING", func(d *Doc) *Text { return &d.Grouping }},
	{"SUBTITLE", func(d *Doc) *Text { return &d.Subtitle }},
	{"WEBSITE", func(d *Doc) *Text { return &d.Website }},
	{"ALBUMWEBSITE", func(d *Doc) *Text { return &d.AlbumWebsite }},
}

// Credit roles become freeform atoms with these prefixes.
const creditPrefix, musicianPrefix = "CREDIT:", "MUSICIAN:"

// Freeform atoms the encoder writes (iTunSMPB, iTunNORM); never tags.
const encoderPrefix = "iTun"

// WriteM4A writes d to an M4A's iTunes metadata. The file needs an existing
// ilst box, which Logic and afconvert bounces have.
func WriteM4A(path string, d Doc, clear bool) error {
	f, err := mp4tag.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	f.UpperCustom(false)

	t := &mp4tag.MP4Tags{Custom: map[string]string{}}
	for _, m := range m4aText {
		*m.set(t) = string(*m.get(&d))
	}
	if t.Comment == "" && len(d.Comments) > 0 {
		t.Comment = d.Comments[0].Text
	}
	if t.Lyrics == "" && len(d.LyricsSections) > 0 {
		t.Lyrics = d.LyricsSections[0].Text
	}
	for _, m := range m4aFreeform {
		t.Custom[m.name] = string(*m.get(&d))
	}
	for k, v := range d.CustomTags {
		t.Custom[k] = string(v)
	}
	for prefix, people := range map[string]map[string]People{creditPrefix: d.InvolvedPeople, musicianPrefix: d.Musicians} {
		for role, names := range people {
			t.Custom[prefix+titleCase(strings.ReplaceAll(role, "_", " "))] = strings.Join(names, ", ")
		}
	}

	t.Year = int32(leadingInt(string(d.Year)))
	if t.Year == 0 {
		t.Year = int32(leadingInt(string(d.ReleaseDate)))
	}
	bpm, _ := strconv.Atoi(string(d.BPM))
	t.BPM = int16(bpm)
	t.TrackNumber, t.TrackTotal = int16(d.TrackNumber), int16(d.TrackTotal)
	t.DiscNumber, t.DiscTotal = int16(d.DiscNumber), int16(d.DiscTotal)
	for _, p := range d.CoverArt {
		format := mp4tag.ImageTypeJPEG
		if p.MimeType == "image/png" {
			format = mp4tag.ImageTypePNG
		}
		t.Pictures = append(t.Pictures, &mp4tag.MP4Picture{Format: format, Data: p.Data})
	}

	del := []string{"allpictures"}
	if clear {
		del = append(del, "alltags", "allcustom", "allothercustom")
		// Keep the encoder's iTunSMPB/iTunNORM: gapless playback and volume
		// data from the bounce, not tags.
		old, err := f.Read()
		if err != nil {
			return err
		}
		for k, v := range old.Custom {
			if strings.HasPrefix(k, encoderPrefix) {
				t.Custom[k] = v
			}
		}
	}
	return f.Write(t, del)
}

// ReadM4A is WriteM4A in reverse, except sort fields: go-mp4tag writes the
// sort atoms (soar, sonm, ...) but its reader doesn't return them.
func ReadM4A(path string) (Doc, error) {
	f, err := mp4tag.Open(path)
	if err != nil {
		return Doc{}, err
	}
	defer f.Close()
	f.UpperCustom(false)
	t, err := f.Read()
	if err != nil {
		return Doc{}, err
	}

	var d Doc
	for _, m := range m4aText {
		*m.get(&d) = Text(*m.set(t))
	}
	freeform := map[string]bool{}
	for _, m := range m4aFreeform {
		*m.get(&d) = Text(t.Custom[m.name])
		freeform[m.name] = true
	}
	keys := make([]string, 0, len(t.Custom))
	for k := range t.Custom {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := t.Custom[k]
		var names People
		names.UnmarshalJSON([]byte(strconv.Quote(v)))
		switch {
		case freeform[k] || v == "" || strings.HasPrefix(k, encoderPrefix):
		case strings.HasPrefix(k, creditPrefix):
			d.InvolvedPeople = addPeople(d.InvolvedPeople, strings.TrimPrefix(k, creditPrefix), names)
		case strings.HasPrefix(k, musicianPrefix):
			d.Musicians = addPeople(d.Musicians, strings.TrimPrefix(k, musicianPrefix), names)
		default:
			if d.CustomTags == nil {
				d.CustomTags = map[string]Text{}
			}
			d.CustomTags[k] = Text(v)
		}
	}
	if t.Year > 0 {
		d.Year = Text(strconv.Itoa(int(t.Year)))
	}
	if t.BPM > 0 {
		d.BPM = Text(strconv.Itoa(int(t.BPM)))
	}
	d.TrackNumber, d.TrackTotal = Int(t.TrackNumber), Int(t.TrackTotal)
	d.DiscNumber, d.DiscTotal = Int(t.DiscNumber), Int(t.DiscTotal)
	for _, p := range t.Pictures {
		mime := "image/jpeg"
		if p.Format == mp4tag.ImageTypePNG {
			mime = "image/png"
		}
		d.CoverArt = append(d.CoverArt, Picture{3, mime, "", p.Data})
	}
	return d, nil
}

func addPeople(m map[string]People, role string, names People) map[string]People {
	for _, n := range names {
		m = addPerson(m, strings.ReplaceAll(strings.ToLower(role), " ", "_"), n)
	}
	return m
}

func leadingInt(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}
