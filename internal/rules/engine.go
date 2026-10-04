package rules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ichiban/prolog"

	"github.com/peter7775/opensearch-query-gateway/internal/dcg"
)

// ErrRejected označuje klauzuli, kterou rule base odmítla (uživatelská chyba,
// typicky HTTP 400). Ostatní chyby enginu jsou interní (timeout, chyba
// v pravidlech…) a mapují se na HTTP 500.
var ErrRejected = errors.New("clause rejected by rule base")

// RejectionError nese důvod odmítnutí klauzule.
type RejectionError struct {
	Field  string
	Op     string
	Value  string
	Reason string
}

func (e *RejectionError) Error() string {
	return fmt.Sprintf("%s: %s (field=%s op=%s value=%q)", ErrRejected, e.Reason, e.Field, e.Op, e.Value)
}

func (e *RejectionError) Unwrap() error { return ErrRejected }

// Engine obaluje embedded Prolog interpreter (ichiban/prolog) použitý
// k normalizaci a validaci AST před tím, než se dostane do dslbuilder.
//
// Rozhraní je záměrně úzké, aby šlo implementaci později vyměnit (např. za
// trealla-prolog/go) nebo evaluaci přesunout mimo proces bez zásahu do zbytku
// pipeline.
//
// Interpreter není dokumentovaně bezpečný pro souběžné dotazy, proto jsou
// všechna volání serializovaná mutexem. Jednotlivé dotazy jsou krátké
// (lookup faktů), takže to v praxi není úzké hrdlo; timeout chrání před
// zacyklenými pravidly.
type Engine struct {
	mu      sync.Mutex
	interp  *prolog.Interpreter
	timeout time.Duration
}

// New vytvoří engine a nahraje do něj rule base ze zadaného souboru
// a volitelně další soubory s fakty (např. výstup schema-export) nebo
// DCG gramatikami (přípona .dcg).
func New(bootstrapFile string, timeout time.Duration, extraFiles ...string) (*Engine, error) {
	interp := prolog.New(nil, io.Discard)

	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	e := &Engine{interp: interp, timeout: timeout}

	// Fakta generovaná ze schématu jsou volitelná, proto je deklarujeme
	// jako dynamická — jinak by dotaz na neexistující predikát skončil
	// existence_error místo prostého neúspěchu.
	if err := e.Load(prelude); err != nil {
		return nil, fmt.Errorf("load prelude: %w", err)
	}

	files := append([]string{}, extraFiles...)
	if bootstrapFile != "" {
		files = append([]string{bootstrapFile}, files...)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read rules %s: %w", f, err)
		}
		text := string(src)
		// Soubory .dcg obsahují gramatiku v DCG notaci (head --> body.),
		// kterou nejdřív přeložíme na Prolog klauzule (balík internal/dcg).
		if strings.HasSuffix(f, ".dcg") {
			if text, err = dcg.Compile(text); err != nil {
				return nil, fmt.Errorf("compile grammar %s: %w", f, err)
			}
		}
		if err := e.Load(text); err != nil {
			return nil, fmt.Errorf("load rules %s: %w", f, err)
		}
	}

	return e, nil
}

const prelude = `
:- set_prolog_flag(double_quotes, atom).
:- dynamic(index/1).
:- dynamic(field/2).
:- dynamic(field_type/3).
:- dynamic(object/2).
:- dynamic(nested/2).
:- dynamic(analyzer/3).
:- dynamic(format/3).
:- dynamic(ignore_above/3).
:- dynamic(indexed/3).
:- dynamic(doc_values/3).
:- dynamic(field_alias/2).
:- dynamic(known_field/1).
:- dynamic(numeric_field/1).
:- dynamic(date_field/1).
`

// Load nahraje do enginu další Prolog program (pravidla nebo fakta).
func (e *Engine) Load(src string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.interp.Exec(src)
}

// first vrátí první řešení dotazu do out; ok=false znamená, že dotaz neuspěl.
func (e *Engine) first(ctx context.Context, query string, out interface{}, args ...interface{}) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	e.mu.Lock()
	defer e.mu.Unlock()

	// Go stringy se díky double_quotes=atom (viz prelude) převádějí na atomy,
	// takže argumenty jsou bezpečně předané jako data — nikdy se neinterpolují
	// do zdrojového textu dotazu (žádná "Prolog injection").
	sols, err := e.interp.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer sols.Close()

	if !sols.Next() {
		if err := sols.Err(); err != nil {
			return false, err
		}
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("rule evaluation: %w", err)
		}
		return false, nil
	}
	if out != nil {
		if err := sols.Scan(out); err != nil {
			return false, err
		}
	}
	return true, nil
}

// NormalizeField se zeptá rule base, jestli syrový název pole odpovídá
// nějakému aliasu (field_alias/2). Pokud ne, vrátí vstup beze změny —
// je považován za už kanonický.
func (e *Engine) NormalizeField(ctx context.Context, raw string) (string, error) {
	var out struct{ Canonical string }
	ok, err := e.first(ctx, `field_alias(?, Canonical).`, &out, raw)
	if err != nil {
		return raw, err
	}
	if !ok || out.Canonical == "" {
		return raw, nil
	}
	return out.Canonical, nil
}

// KnownField ověří, že pole je v allow-listu (known_field/1).
func (e *Engine) KnownField(ctx context.Context, field string) (bool, error) {
	return e.first(ctx, `known_field(?).`, nil, field)
}

// Validate ověří klauzuli proti guard pravidlům (valid_clause/3 v bootstrap.pl),
// například proti allow-listu polí nebo typové kontrole u rozsahových dotazů.
// Odmítnutí vrací *RejectionError (errors.Is(err, ErrRejected) == true).
func (e *Engine) Validate(ctx context.Context, field, op, value string) error {
	ok, err := e.first(ctx, `valid_clause(?, ?, ?).`, nil, field, op, value)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	rej := &RejectionError{Field: field, Op: op, Value: value}

	// Pokus o lidsky čitelný důvod z rule base (rejection_reason/4),
	// jinak obecná zpráva podle toho, jestli pole vůbec známe.
	var reason struct{ Reason string }
	if found, rerr := e.first(ctx, `catch(rejection_reason(?, ?, ?, Reason), _, fail).`, &reason, field, op, value); rerr == nil && found && reason.Reason != "" {
		rej.Reason = reason.Reason
		return rej
	}
	known, kerr := e.KnownField(ctx, field)
	switch {
	case kerr != nil:
		return kerr
	case !known:
		rej.Reason = "unknown field"
	default:
		rej.Reason = fmt.Sprintf("operation %q is not allowed on this field", op)
	}
	return rej
}

// Close uvolní případné prostředky enginu (v základní podobě není potřeba,
// ale rozhraní na to počítá pro budoucí implementace se sub-procesem).
func (e *Engine) Close() error {
	return nil
}
