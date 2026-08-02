package schema

import (
	"fmt"
	"sort"
	"strings"
)

type PrologWriter struct{}

func NewPrologWriter() *PrologWriter {
	return &PrologWriter{}
}

func (w *PrologWriter) Write(s *IndexSchema) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("index(%s).\n", atom(s.Name)))

	fields := make([]Field, len(s.Fields))
	copy(fields, s.Fields)
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Path < fields[j].Path
	})

	for _, f := range fields {
		b.WriteString(fmt.Sprintf("field(%s, %s).\n", atom(s.Name), atom(f.Path)))
		if f.Type != "" {
			b.WriteString(fmt.Sprintf("field_type(%s, %s, %s).\n", atom(s.Name), atom(f.Path), atom(f.Type)))
		}
		if f.IsObject {
			b.WriteString(fmt.Sprintf("object(%s, %s).\n", atom(s.Name), atom(f.Path)))
		}
		if f.IsNested {
			b.WriteString(fmt.Sprintf("nested(%s, %s).\n", atom(s.Name), atom(f.Path)))
		}
		if f.Analyzer != "" {
			b.WriteString(fmt.Sprintf("analyzer(%s, %s, %s).\n", atom(s.Name), atom(f.Path), atom(f.Analyzer)))
		}
		if f.Format != "" {
			b.WriteString(fmt.Sprintf("format(%s, %s, %s).\n", atom(s.Name), atom(f.Path), atom(f.Format)))
		}
		if f.IgnoreAbove != nil {
			b.WriteString(fmt.Sprintf("ignore_above(%s, %s, %d).\n", atom(s.Name), atom(f.Path), *f.IgnoreAbove))
		}
		if f.Indexed != nil {
			b.WriteString(fmt.Sprintf("indexed(%s, %s, %t).\n", atom(s.Name), atom(f.Path), *f.Indexed))
		}
		if f.DocValues != nil {
			b.WriteString(fmt.Sprintf("doc_values(%s, %s, %t).\n", atom(s.Name), atom(f.Path), *f.DocValues))
		}
	}

	return b.String()
}

func atom(s string) string {
	if s == "" {
		return "''"
	}
	ok := true
	for i, r := range s {
		if i == 0 {
			if !((r >= 'a' && r <= 'z') || r == '_') {
				ok = false
				break
			}
		} else if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.') {
			ok = false
			break
		}
	}
	if ok {
		return s
	}
	return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "\\'"))
}
