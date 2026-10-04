package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/peter7775/opensearch-query-gateway/internal/schema"
)

func (s *Server) schemaFor(w http.ResponseWriter, r *http.Request) (*schema.IndexSchema, error) {
	index := r.URL.Query().Get("index")
	if index == "" {
		index = s.os.Index
	}

	var raw []byte
	if r.Method == http.MethodPost {
		// Offline analýza: klient pošle mapping v těle (libovolný ze tvarů,
		// které umí schema.Mapper), cluster se nevolá.
		var err error
		raw, err = io.ReadAll(http.MaxBytesReader(w, r.Body, s.maxBody))
		if err != nil {
			return nil, &apiError{status: http.StatusRequestEntityTooLarge, code: "body_too_large", msg: err.Error()}
		}
	} else {
		if !s.os.IndexAllowed(index) {
			return nil, &apiError{status: http.StatusForbidden, code: "index_not_allowed", msg: fmt.Sprintf("index %q is not allowed", index)}
		}
		var err error
		raw, err = s.backend.GetMapping(r.Context(), index)
		if err != nil {
			return nil, err
		}
	}

	sch, err := schema.NewMapper().FromJSON(index, raw)
	if err != nil {
		return nil, badRequest("invalid_mapping", "cannot read mapping: "+err.Error())
	}
	return sch, nil
}

// Introspect: GET /v1/schema/introspect?index=… — lidsky čitelný přehled
// schématu z živého clusteru; POST se stejnou cestou analyzuje mapping z těla.
func (s *Server) Introspect(w http.ResponseWriter, r *http.Request) {
	sch, err := s.schemaFor(w, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	purpose := ""
	if sch.Name == s.os.Index {
		purpose = s.os.Purpose
	}
	writeJSON(w, http.StatusOK, schema.Analyze(sch, purpose))
}

// PrologFacts: GET|POST /v1/schema/prolog — schéma jako Prolog fakta
// (stejný výstup jako CLI schema-export), použitelné v rules.schema_files.
func (s *Server) PrologFacts(w http.ResponseWriter, r *http.Request) {
	sch, err := s.schemaFor(w, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, schema.NewPrologWriter().Write(sch))
}
