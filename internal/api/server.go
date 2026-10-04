package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/peter7775/opensearch-query-gateway/internal/config"
	"github.com/peter7775/opensearch-query-gateway/internal/dslbuilder"
	"github.com/peter7775/opensearch-query-gateway/internal/executor"
	"github.com/peter7775/opensearch-query-gateway/internal/parser"
)

// Backend je abstrakce nad OpenSearch clusterem (implementuje executor.Client).
// Rozhraní umožňuje handlery testovat bez běžícího clusteru.
type Backend interface {
	Search(ctx context.Context, index string, body map[string]interface{}) (map[string]interface{}, error)
	GetMapping(ctx context.Context, index string) ([]byte, error)
	Ping(ctx context.Context) error
}

// RuleEngine je abstrakce nad Prolog rule enginem (implementuje rules.Engine).
type RuleEngine interface {
	NormalizeField(ctx context.Context, raw string) (string, error)
	Validate(ctx context.Context, field, op, value string) error
	KnownField(ctx context.Context, field string) (bool, error)
}

// Server drží všechny stupně pipeline a HTTP handlery nad nimi.
type Server struct {
	parser  *parser.Parser
	rules   RuleEngine
	builder *dslbuilder.Builder
	backend Backend
	os      config.OpenSearchConfig
	search  config.SearchConfig
	maxBody int64
}

// Options jsou konfigurovatelné parametry Serveru.
type Options struct {
	OpenSearch   config.OpenSearchConfig
	Search       config.SearchConfig
	MaxBodyBytes int64
}

func NewServer(p *parser.Parser, re RuleEngine, b *dslbuilder.Builder, be Backend, opts Options) *Server {
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 1 << 20
	}
	if opts.Search.DefaultSize <= 0 {
		opts.Search.DefaultSize = 10
	}
	if opts.Search.MaxSize <= 0 {
		opts.Search.MaxSize = 100
	}
	if opts.Search.MaxFrom <= 0 {
		opts.Search.MaxFrom = 10000
	}
	return &Server{
		parser:  p,
		rules:   re,
		builder: b,
		backend: be,
		os:      opts.OpenSearch,
		search:  opts.Search,
		maxBody: opts.MaxBodyBytes,
	}
}

// --- JSON helpers -------------------------------------------------------------

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = msg
	writeJSON(w, status, b)
}

// apiError je chyba s HTTP statusem a strojově čitelným kódem.
type apiError struct {
	status int
	code   string
	msg    string
}

func (e *apiError) Error() string { return e.msg }

func badRequest(code, msg string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: code, msg: msg}
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.msg)
		return
	}
	var oe *executor.Error
	if errors.As(err, &oe) {
		status := http.StatusBadGateway
		code := "opensearch_error"
		switch {
		case oe.Status == http.StatusNotFound:
			status, code = http.StatusNotFound, "index_not_found"
		case oe.Status >= 400 && oe.Status < 500:
			// Dotaz prošel validací, ale cluster ho odmítl (např. typový
			// nesoulad hodnoty a pole) — z pohledu klienta je to stále 400.
			status, code = http.StatusBadRequest, "opensearch_rejected"
		}
		writeError(w, status, code, oe.Error())
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "timeout", err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
}

func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &apiError{status: http.StatusRequestEntityTooLarge, code: "body_too_large", msg: err.Error()}
		}
		return badRequest("invalid_body", "invalid request body: "+err.Error())
	}
	return nil
}

// --- health -------------------------------------------------------------------

// Healthz je liveness probe — proces běží.
func (s *Server) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz je readiness probe — cluster je dostupný.
func (s *Server) Readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.backend.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "opensearch_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
