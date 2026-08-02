package api

import (
	"encoding/json"
	"net/http"

	"github.com/example/opensearch-query-gateway/internal/dslbuilder"
	"github.com/example/opensearch-query-gateway/internal/executor"
	"github.com/example/opensearch-query-gateway/internal/parser"
	"github.com/example/opensearch-query-gateway/internal/rules"
)

// SearchHandler drží referenci na všechny stupně pipeline a provádí je
// v pevném pořadí pro každý příchozí request: parse → normalize/validate → build → execute.
type SearchHandler struct {
	parser  *parser.Parser
	rules   *rules.Engine
	builder *dslbuilder.Builder
	client  *executor.Client
}

func NewSearchHandler(p *parser.Parser, re *rules.Engine, b *dslbuilder.Builder, c *executor.Client) *SearchHandler {
	return &SearchHandler{parser: p, rules: re, builder: b, client: c}
}

type searchRequest struct {
	Query string `json:"query"`
}

func (h *SearchHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ast, err := h.parser.Parse(req.Query)
	if err != nil {
		http.Error(w, "invalid query syntax: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// WalkClauses prochází celý AST včetně vnořených závorek a všech
	// booleovských kombinací — normalizace/validace je tak nezávislá na tom,
	// jak složitě je dotaz strukturovaný.
	var internalErr, rejectErr error
	ast.WalkClauses(func(c *parser.Clause) {
		if internalErr != nil || rejectErr != nil {
			return
		}

		canonical, err := h.rules.NormalizeField(ctx, c.Field)
		if err != nil {
			internalErr = err
			return
		}
		c.Field = canonical

		if err := h.rules.Validate(ctx, c.Field, c.Op(), c.RawValue()); err != nil {
			rejectErr = err
			return
		}
	})

	if internalErr != nil {
		http.Error(w, "rule evaluation failed: "+internalErr.Error(), http.StatusInternalServerError)
		return
	}
	if rejectErr != nil {
		http.Error(w, rejectErr.Error(), http.StatusBadRequest)
		return
	}

	body := h.builder.Build(ast)

	result, err := h.client.Search(ctx, body)
	if err != nil {
		http.Error(w, "search failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
