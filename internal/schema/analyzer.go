package schema

import (
	"fmt"
	"sort"
	"strings"
)

// Introspection je lidsky čitelný přehled schématu indexu — vrací ho
// endpoint /v1/schema/introspect a slouží jako podklad pro autocomplete,
// dokumentaci nebo vizuální query builder.
type Introspection struct {
	Index      string         `json:"index"`
	Purpose    string         `json:"purpose,omitempty"`
	Summary    Summary        `json:"summary"`
	Highlights []string       `json:"highlights"`
	Fields     []FieldSummary `json:"fields"`
}

type Summary struct {
	FieldsTotal  int `json:"fields_total"`
	Searchable   int `json:"searchable"`
	Aggregatable int `json:"aggregatable"`
	ObjectFields int `json:"object_fields"`
	NestedFields int `json:"nested_fields"`
}

type FieldSummary struct {
	Path         string   `json:"path"`
	Type         string   `json:"type"`
	Searchable   bool     `json:"searchable"`
	Aggregatable bool     `json:"aggregatable"`
	Operators    []string `json:"operators,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

var numericTypes = map[string]bool{
	"long": true, "integer": true, "short": true, "byte": true,
	"double": true, "float": true, "half_float": true, "scaled_float": true,
	"unsigned_long": true,
}

func isNumeric(t string) bool { return numericTypes[t] }
func isDate(t string) bool    { return t == "date" || t == "date_nanos" }

// Searchable říká, jestli lze nad polem filtrovat/vyhledávat.
func (f Field) Searchable() bool {
	if f.IsObject || f.IsNested {
		return false
	}
	if f.Indexed != nil && !*f.Indexed {
		return false
	}
	switch {
	case f.Type == "text", f.Type == "match_only_text", f.Type == "keyword",
		f.Type == "constant_keyword", f.Type == "wildcard",
		f.Type == "boolean", f.Type == "ip", isNumeric(f.Type), isDate(f.Type):
		return true
	}
	return false
}

// Aggregatable říká, jestli lze nad polem agregovat a řadit (doc_values).
func (f Field) Aggregatable() bool {
	if f.IsObject || f.IsNested {
		return false
	}
	if f.Type == "text" {
		return f.Fielddata
	}
	if f.DocValues != nil && !*f.DocValues {
		return false
	}
	switch {
	case f.Type == "keyword", f.Type == "constant_keyword", f.Type == "boolean",
		f.Type == "ip", isNumeric(f.Type), isDate(f.Type):
		return true
	}
	return false
}

// Operators vrátí operátory dotazovacího jazyka, které má u pole smysl použít.
func (f Field) Operators() []string {
	if !f.Searchable() {
		return nil
	}
	switch {
	case f.Type == "text" || f.Type == "match_only_text":
		return []string{"match", "phrase", "fuzzy"}
	case f.Type == "keyword" || f.Type == "wildcard" || f.Type == "constant_keyword":
		return []string{"match", "wildcard", "fuzzy"}
	case isNumeric(f.Type) || f.Type == "ip":
		return []string{"match", "range", "compare"}
	case isDate(f.Type):
		return []string{"match", "range", "compare", "relative"}
	case f.Type == "boolean":
		return []string{"match"}
	}
	return nil
}

func (f Field) notes() string {
	var n []string
	switch {
	case isDate(f.Type):
		if f.Path == "@timestamp" || strings.HasSuffix(f.Path, "timestamp") {
			n = append(n, "Primary time filter")
		} else {
			n = append(n, "Date — use ranges or last:<duration>")
		}
		if f.Format != "" {
			n = append(n, "format "+f.Format)
		}
	case f.Type == "text":
		n = append(n, "Full-text (analyzed)")
		if f.Analyzer != "" {
			n = append(n, "analyzer "+f.Analyzer)
		}
	case f.Type == "keyword":
		if f.Parent != "" {
			n = append(n, "Exact-match variant of "+f.Parent+" — use for filters, sorting and aggregations")
		} else {
			n = append(n, "Exact match, ideal for filters and grouping")
		}
		if f.IgnoreAbove != nil {
			n = append(n, fmt.Sprintf("values longer than %d chars are not indexed", *f.IgnoreAbove))
		}
	case f.IsNested:
		n = append(n, "Nested — inner fields require nested queries")
	case f.IsObject:
		n = append(n, "Object — query its sub-fields")
	case isNumeric(f.Type):
		n = append(n, "Numeric — supports ranges and comparisons")
	}
	if f.Indexed != nil && !*f.Indexed {
		n = append(n, "not indexed (stored only)")
	}
	return strings.Join(n, "; ")
}

// Analyze vytvoří lidsky čitelný přehled schématu.
func Analyze(s *IndexSchema, purpose string) Introspection {
	out := Introspection{Index: s.Name, Purpose: purpose, Highlights: []string{}, Fields: []FieldSummary{}}

	fields := make([]Field, len(s.Fields))
	copy(fields, s.Fields)
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })

	var dates, fullText, filters, nested []string
	for _, f := range fields {
		out.Summary.FieldsTotal++
		if f.Searchable() {
			out.Summary.Searchable++
		}
		if f.Aggregatable() {
			out.Summary.Aggregatable++
		}
		if f.IsObject {
			out.Summary.ObjectFields++
		}
		if f.IsNested {
			out.Summary.NestedFields++
			nested = append(nested, f.Path)
		}
		switch {
		case isDate(f.Type) && f.Searchable():
			dates = append(dates, f.Path)
		case f.Type == "text" && f.Searchable():
			fullText = append(fullText, f.Path)
		case f.Type == "keyword" && f.Aggregatable() && f.Parent == "":
			filters = append(filters, f.Path)
		}

		out.Fields = append(out.Fields, FieldSummary{
			Path:         f.Path,
			Type:         f.Type,
			Searchable:   f.Searchable(),
			Aggregatable: f.Aggregatable(),
			Operators:    f.Operators(),
			Notes:        f.notes(),
		})
	}

	if len(dates) > 0 {
		out.Highlights = append(out.Highlights, fmt.Sprintf("Time-based filtering is supported (%s).", strings.Join(dates, ", ")))
	}
	if len(filters) > 0 {
		out.Highlights = append(out.Highlights, fmt.Sprintf("%s %s ideal for filters and grouping.", joinHuman(filters), plural(len(filters), "is", "are")))
	}
	if len(fullText) > 0 {
		out.Highlights = append(out.Highlights, fmt.Sprintf("%s %s full-text search.", joinHuman(fullText), plural(len(fullText), "supports", "support")))
	}
	if len(nested) > 0 {
		out.Highlights = append(out.Highlights, fmt.Sprintf("Nested structures (%s) need nested queries.", strings.Join(nested, ", ")))
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func joinHuman(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	if len(items) > 5 {
		return strings.Join(items[:5], ", ") + fmt.Sprintf(" and %d more", len(items)-5)
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
