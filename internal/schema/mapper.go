package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Mapper struct{}

func NewMapper() *Mapper {
	return &Mapper{}
}

func (m *Mapper) FromJSON(rootName string, raw []byte) (*IndexSchema, error) {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}

	obj, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top-level JSON must be an object")
	}

	s := &IndexSchema{Name: rootName}

	if props, ok := extractProperties(obj); ok {
		m.walkProps(s, "", props)
		return s, nil
	}

	return nil, fmt.Errorf("no properties or mappings.properties found")
}

func extractProperties(obj map[string]any) (map[string]any, bool) {
	if props, ok := obj["properties"].(map[string]any); ok {
		return props, true
	}
	if mappings, ok := obj["mappings"].(map[string]any); ok {
		if props, ok := mappings["properties"].(map[string]any); ok {
			return props, true
		}
	}
	return nil, false
}

func (m *Mapper) walkProps(s *IndexSchema, prefix string, props map[string]any) {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		val, _ := props[k].(map[string]any)
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		f := Field{Path: path}

		if t, ok := val["type"].(string); ok {
			f.Type = t
			if b, ok := val["index"].(bool); ok {
				f.Indexed = &b
			}
			if b, ok := val["doc_values"].(bool); ok {
				f.DocValues = &b
			}
			if a, ok := val["analyzer"].(string); ok {
				f.Analyzer = a
			}
			if fm, ok := val["format"].(string); ok {
				f.Format = fm
			}
			if ia, ok := val["ignore_above"].(float64); ok {
				v := int(ia)
				f.IgnoreAbove = &v
			}
			if t == "object" {
				f.IsObject = true
			}
			if t == "nested" {
				f.IsNested = true
			}
			s.Fields = append(s.Fields, f)
		}

		if child, ok := val["properties"].(map[string]any); ok {
			if f.Type == "" {
				f.IsObject = true
				s.Fields = append(s.Fields, Field{Path: path, Type: "object", IsObject: true})
			}
			m.walkProps(s, path, child)
		}
	}
}

func (s *IndexSchema) HasField(path string) bool {
	for _, f := range s.Fields {
		if f.Path == path {
			return true
		}
	}
	return false
}

func (s *IndexSchema) FieldByPath(path string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Path == path {
			return f, true
		}
	}
	return Field{}, false
}

func (s *IndexSchema) SearchableFields() []string {
	var out []string
	for _, f := range s.Fields {
		if f.Type == "text" || f.Type == "keyword" || f.Type == "date" {
			out = append(out, f.Path)
		}
	}
	return out
}

func (s *IndexSchema) String() string {
	var b strings.Builder
	b.WriteString(s.Name)
	return b.String()
}
