package schema

// IndexSchema je zploštělý přehled mappingu jednoho indexu.
type IndexSchema struct {
	Name   string
	Fields []Field
}

// Field popisuje jedno pole mappingu (včetně vnořených a multi-field
// podpolí, např. title.keyword).
type Field struct {
	Path        string
	Type        string
	Indexed     *bool
	DocValues   *bool
	Fielddata   bool
	Analyzer    string
	Format      string
	IgnoreAbove *int
	IsObject    bool
	IsNested    bool
	// Parent je vyplněný u multi-field podpolí (title.keyword → title).
	Parent string
}
