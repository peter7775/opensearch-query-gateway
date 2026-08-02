package schema

type IndexSchema struct {
	Name   string
	Fields []Field
}

type Field struct {
	Path        string
	Type        string
	Indexed     *bool
	DocValues   *bool
	Analyzer    string
	Format      string
	IgnoreAbove *int
	IsObject    bool
	IsNested    bool
}
