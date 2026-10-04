package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/peter7775/opensearch-query-gateway/internal/dslbuilder"
	"github.com/peter7775/opensearch-query-gateway/internal/parser"
	"github.com/peter7775/opensearch-query-gateway/internal/rules"
)

type searchRequest struct {
	Query   string   `json:"query"`
	Index   string   `json:"index,omitempty"`
	Size    *int     `json:"size,omitempty"`
	From    int      `json:"from,omitempty"`
	Sort    []string `json:"sort,omitempty"`
	Explain bool     `json:"explain,omitempty"`
}

// Normalization zaznamenává přepis názvu pole rule enginem (pro explain).
type Normalization struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Translation je výsledek pipeline parse → normalize/validate → build.
type Translation struct {
	Index      string                 `json:"index"`
	Query      string                 `json:"query"`
	DSL        map[string]interface{} `json:"dsl"`
	Normalized []Normalization        `json:"normalized_fields"`
}

// translate provede celou pipeline kromě samotného dotazu na cluster.
func (s *Server) translate(ctx context.Context, req searchRequest) (*Translation, error) {
	index := req.Index
	if index == "" {
		index = s.os.Index
	}
	if !s.os.IndexAllowed(index) {
		return nil, &apiError{status: http.StatusForbidden, code: "index_not_allowed", msg: fmt.Sprintf("index %q is not allowed", index)}
	}

	page, err := s.page(ctx, req)
	if err != nil {
		return nil, err
	}

	ast, err := s.parser.Parse(req.Query)
	if err != nil {
		if errors.Is(err, parser.ErrEmptyQuery) {
			return nil, badRequest("empty_query", "query must not be empty")
		}
		return nil, badRequest("invalid_query", "invalid query syntax: "+err.Error())
	}

	tr := &Translation{Index: index, Query: req.Query, Normalized: []Normalization{}}

	// WalkClauses prochází celý AST včetně vnořených závorek a všech
	// booleovských kombinací — normalizace/validace je tak nezávislá na tom,
	// jak složitě je dotaz strukturovaný.
	var walkErr error
	ast.WalkClauses(func(c *parser.Clause) {
		if walkErr != nil {
			return
		}
		walkErr = s.checkClause(ctx, c, tr)
	})
	if walkErr != nil {
		return nil, walkErr
	}

	tr.DSL = s.builder.BuildSearch(ast, page)
	return tr, nil
}

func (s *Server) checkClause(ctx context.Context, c *parser.Clause, tr *Translation) error {
	// Relativní čas (last:15m) se validuje jako rozsah nad časovým polem.
	if c.IsRelativeTime() {
		d, ok := c.Duration()
		if !ok {
			return badRequest("invalid_query", fmt.Sprintf("invalid duration %q for %s: use e.g. 15m, 2h, 7d", c.RawValue(), parser.RelativeTimeField))
		}
		return s.validate(ctx, s.builder.TimeField(), "range", d)
	}

	canonical, err := s.rules.NormalizeField(ctx, c.Field)
	if err != nil {
		return &apiError{status: http.StatusInternalServerError, code: "rule_error", msg: "rule evaluation failed: " + err.Error()}
	}
	if canonical != c.Field {
		tr.Normalized = append(tr.Normalized, Normalization{From: c.Field, To: canonical})
		c.Field = canonical
	}
	return s.validate(ctx, c.Field, c.Op(), c.RawValue())
}

func (s *Server) validate(ctx context.Context, field, op, value string) error {
	err := s.rules.Validate(ctx, field, op, value)
	if err == nil {
		return nil
	}
	if errors.Is(err, rules.ErrRejected) {
		return badRequest("clause_rejected", err.Error())
	}
	return &apiError{status: http.StatusInternalServerError, code: "rule_error", msg: "rule evaluation failed: " + err.Error()}
}

// page ověří a sestaví stránkování a řazení.
func (s *Server) page(ctx context.Context, req searchRequest) (dslbuilder.Page, error) {
	p := dslbuilder.Page{Size: s.search.DefaultSize, From: req.From, Track: s.search.TrackTotalHits}
	if req.Size != nil {
		p.Size = *req.Size
	}
	if p.Size < 0 || p.Size > s.search.MaxSize {
		return p, badRequest("invalid_size", fmt.Sprintf("size must be between 0 and %d", s.search.MaxSize))
	}
	if p.From < 0 || p.From+p.Size > s.search.MaxFrom {
		return p, badRequest("invalid_from", fmt.Sprintf("from+size must not exceed %d; use a narrower query or search_after-based paging", s.search.MaxFrom))
	}

	for _, raw := range req.Sort {
		field, dir, _ := strings.Cut(strings.TrimSpace(raw), ":")
		sf := dslbuilder.SortField{Field: field}
		switch strings.ToLower(dir) {
		case "", "asc":
		case "desc":
			sf.Desc = true
		default:
			return p, badRequest("invalid_sort", fmt.Sprintf("invalid sort direction %q (use asc or desc)", dir))
		}
		if field == "" {
			return p, badRequest("invalid_sort", "empty sort field")
		}
		if field != "_score" {
			canonical, err := s.rules.NormalizeField(ctx, field)
			if err != nil {
				return p, &apiError{status: http.StatusInternalServerError, code: "rule_error", msg: err.Error()}
			}
			known, err := s.rules.KnownField(ctx, canonical)
			if err != nil {
				return p, &apiError{status: http.StatusInternalServerError, code: "rule_error", msg: err.Error()}
			}
			if !known {
				return p, badRequest("invalid_sort", fmt.Sprintf("cannot sort by unknown field %q", field))
			}
			sf.Field = canonical
		}
		p.Sort = append(p.Sort, sf)
	}
	return p, nil
}

// Search: POST /v1/search — přeloží dotaz a provede ho proti clusteru.
// S "explain": true vrátí vedle výsledku i vygenerované DSL a normalizace.
func (s *Server) Search(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := s.decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}

	tr, err := s.translate(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}

	result, err := s.backend.Search(r.Context(), tr.Index, tr.DSL)
	if err != nil {
		writeErr(w, err)
		return
	}

	if req.Explain {
		writeJSON(w, http.StatusOK, map[string]interface{}{"explain": tr, "result": result})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Translate: POST /v1/translate — dry-run, vrátí OpenSearch DSL bez
// dotazu na cluster. Hodí se pro ladění dotazů a pro UI/query builder.
func (s *Server) Translate(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := s.decode(w, r, &req); err != nil {
		writeErr(w, err)
		return
	}
	tr, err := s.translate(r.Context(), req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}
