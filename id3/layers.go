package id3

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Resolve loads layer files (band, album, song, ... in that order), merges
// them with later layers winning, and decodes the result into a Doc.
// A layer that is missing or does not parse is an error, never skipped.
func Resolve(paths ...string) (Doc, error) {
	layers := make([]map[string]any, 0, len(paths))
	for _, p := range paths {
		layer, err := LoadLayer(p)
		if err != nil {
			return Doc{}, err
		}
		layers = append(layers, layer)
	}
	return ResolveLayers(layers...)
}

// ResolveLayers merges already-loaded layers (e.g. JSON request bodies or
// Firestore docs) the same way Resolve merges files.
func ResolveLayers(layers ...map[string]any) (Doc, error) {
	merged := map[string]any{}
	for _, l := range layers {
		inner, err := unwrap(l)
		if err != nil {
			return Doc{}, err
		}
		merged = Merge(merged, inner)
	}
	return DocFromMap(merged)
}

// unwrap returns the `audio_metadata:` envelope's contents, or m without one.
// An envelope that isn't a single mapping (e.g. pasted twice, which collects
// into a list) is an error rather than an empty layer.
func unwrap(m map[string]any) (map[string]any, error) {
	v, ok := m["audio_metadata"]
	if !ok {
		return m, nil
	}
	switch inner := v.(type) {
	case map[string]any:
		return inner, nil
	case []any:
		return nil, fmt.Errorf("audio_metadata appears %d times; merge them into one block", len(inner))
	}
	return nil, fmt.Errorf("audio_metadata is %T, want a mapping", v)
}

// LoadLayer reads one YAML or JSON layer (JSON is YAML) into a map,
// unwrapping the `audio_metadata:` envelope when present.
func LoadLayer(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	v, err := nodeValue(&root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}, nil
	}
	inner, err := unwrap(m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return inner, nil
}

// nodeValue converts a YAML node to plain values. Unlike a straight decode it
// keeps repeated mapping keys (engineer: A / engineer: B) by collecting them
// into a list, and unwraps the mutagen `!!python/object/new` tags found in
// the Python tool's read-back files.
func nodeValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeValue(n.Content[0])
	case yaml.AliasNode:
		return nodeValue(n.Alias)
	case yaml.SequenceNode:
		if strings.Contains(n.Tag, "python/object") && len(n.Content) == 1 {
			return nodeValue(n.Content[0])
		}
		list := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeValue(c)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		return list, nil
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			v, err := nodeValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			if prev, dup := m[k]; dup {
				list, isList := prev.([]any)
				if !isList {
					list = []any{prev}
				}
				v = append(list, v)
			}
			m[k] = v
		}
		return m, nil
	}
	// Digit strings that aren't really numbers (UPCs, IDs) keep their exact
	// text: a leading zero would read as octal, and long ones lose precision.
	if (n.Tag == "!!int" || n.Tag == "!!float") && allDigits(n.Value) && ((len(n.Value) > 1 && n.Value[0] == '0') || len(n.Value) > 15) {
		return n.Value, nil
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return n.Value, nil
	}
	if _, isTime := v.(time.Time); isTime {
		return n.Value, nil
	}
	return v, nil
}

// Merge is yaml-merge.py's deep_merge: maps merge recursively, everything
// else (scalars, lists) is replaced by the later layer.
func Merge(dst, src map[string]any) map[string]any {
	for k, v := range src {
		sm, srcIsMap := v.(map[string]any)
		dm, dstIsMap := dst[k].(map[string]any)
		if srcIsMap && dstIsMap {
			dst[k] = Merge(dm, sm)
			continue
		}
		dst[k] = v
	}
	return dst
}

func DocFromMap(m map[string]any) (Doc, error) {
	var d Doc
	b, err := json.Marshal(m)
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal(b, &d)
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
