package id3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
)

// ID3v2.2 (3-character frame IDs, common in early iTunes rips) isn't read by
// bogem/id3v2. A v2.2 tag is rewritten as v2.3 — same frame bodies, 4-character
// IDs — the way mutagen upgraded it for the Python tool. Frames with no v2.3
// equivalent are dropped.
var v22IDs = map[string]string{
	"TT1": "TIT1", "TT2": "TIT2", "TT3": "TIT3",
	"TP1": "TPE1", "TP2": "TPE2", "TP3": "TPE3", "TP4": "TPE4",
	"TAL": "TALB", "TCO": "TCON", "TCM": "TCOM", "TXT": "TEXT",
	"TYE": "TYER", "TDA": "TDAT", "TIM": "TIME", "TRD": "TRDA", "TOR": "TORY",
	"TRK": "TRCK", "TPA": "TPOS", "TRC": "TSRC", "TCR": "TCOP", "TPB": "TPUB",
	"TEN": "TENC", "TSS": "TSSE", "TBP": "TBPM", "TKE": "TKEY", "TLA": "TLAN",
	"TLE": "TLEN", "TMT": "TMED", "TFT": "TFLT", "TOA": "TOPE", "TOT": "TOAL",
	"TOL": "TOLY", "TOF": "TOFN", "TCP": "TCMP",
	"TST": "TSOT", "TSP": "TSOP", "TSA": "TSOA", "TS2": "TSO2", "TSC": "TSOC",
	"TXX": "TXXX", "WXX": "WXXX", "WAF": "WOAF", "WAR": "WOAR", "WAS": "WOAS",
	"WCM": "WCOM", "WCP": "WCOP", "WPB": "WPUB",
	"COM": "COMM", "ULT": "USLT", "PIC": "APIC", "UFI": "UFID", "IPL": "IPLS",
	"CNT": "PCNT", "POP": "POPM", "GEO": "GEOB",
}

// openMP3 opens path's ID3 tag. A v2.2 tag is upgraded in memory for reads;
// for writes the file is rewritten with the upgraded tag first, then opened.
func openMP3(path string, write bool) (*id3v2.Tag, error) {
	opts := id3v2.Options{Parse: true}
	tag, err := id3v2.Open(path, opts)
	if !errors.Is(err, id3v2.ErrUnsupportedVersion) {
		return tag, err
	}
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, rerr
	}
	up, uerr := upgradeV22(b)
	if uerr != nil {
		return nil, fmt.Errorf("%s: %w", path, uerr)
	}
	if up == nil {
		return nil, err
	}
	if !write {
		return id3v2.ParseReader(bytes.NewReader(up), opts)
	}
	if err := replaceFile(path, up); err != nil {
		return nil, err
	}
	return id3v2.Open(path, opts)
}

// upgradeV22 returns b with its v2.2 tag rewritten as v2.3, or nil if b
// doesn't start with a v2.2 tag.
func upgradeV22(b []byte) ([]byte, error) {
	if len(b) < 10 || string(b[:3]) != "ID3" || b[3] != 2 {
		return nil, nil
	}
	flags := b[5]
	if flags&0x40 != 0 {
		return nil, errors.New("compressed ID3v2.2 tag")
	}
	end := 10 + syncsafe(b[6:10])
	if end > len(b) {
		return nil, errors.New("ID3v2.2 tag runs past the end of the file")
	}
	body := b[10:end]
	if flags&0x80 != 0 { // unsynchronised: FF 00 → FF
		body = bytes.ReplaceAll(body, []byte{0xff, 0x00}, []byte{0xff})
	}

	var frames bytes.Buffer
	for len(body) >= 6 && body[0] != 0 {
		id, n := string(body[:3]), int(body[3])<<16|int(body[4])<<8|int(body[5])
		if 6+n > len(body) {
			break
		}
		data := body[6 : 6+n]
		body = body[6+n:]
		newID, ok := v22IDs[id]
		if !ok {
			continue
		}
		if id == "PIC" {
			data = picToAPIC(data)
		}
		frames.WriteString(newID)
		binary.Write(&frames, binary.BigEndian, uint32(len(data)))
		frames.Write([]byte{0, 0})
		frames.Write(data)
	}

	n := frames.Len()
	out := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	out = append(out, frames.Bytes()...)
	return append(out, b[end:]...), nil
}

// picToAPIC turns a v2.2 PIC body (3-letter image format) into an APIC body
// (MIME type string).
func picToAPIC(d []byte) []byte {
	if len(d) < 4 {
		return d
	}
	mime := map[string]string{"JPG": "image/jpeg", "PNG": "image/png"}[strings.ToUpper(string(d[1:4]))]
	if mime == "" {
		mime = "image/" + strings.ToLower(string(d[1:4]))
	}
	out := append([]byte{d[0]}, mime...)
	out = append(out, 0)
	return append(out, d[4:]...)
}

func syncsafe(b []byte) int {
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3])
}

// replaceFile swaps path's contents via a temp file and rename, keeping its mode.
func replaceFile(path string, b []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
