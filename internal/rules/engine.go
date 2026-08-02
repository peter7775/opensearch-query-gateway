package rules

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ichiban/prolog"
)

// Engine obaluje embedded Prolog interpreter použitý k normalizaci,
// validaci a přepisu AST před tím, než se dostane do dslbuilder.
//
// Rozhraní je záměrně úzké (dvě metody), aby šlo enginu později vyměnit
// implementaci (např. na trealla-prolog/go), nebo evaluaci přesunout mimo
// proces, bez zásahu do zbytku pipeline.
//
// Poznámka: přesné názvy metod ichiban/prolog API (Exec/Query/QueryContext)
// se mezi verzemi knihovny mírně liší — ověřte proti verzi zamknuté v go.mod.
type Engine struct {
	interp  *prolog.Interpreter
	timeout time.Duration
}

// New vytvoří engine a nahraje do něj rule base ze zadaného souboru.
func New(bootstrapFile string, timeout time.Duration) (*Engine, error) {
	interp := prolog.New(nil, os.Stdout)

	if bootstrapFile != "" {
		src, err := os.ReadFile(bootstrapFile)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap rules: %w", err)
		}
		if err := interp.Exec(string(src)); err != nil {
			return nil, fmt.Errorf("load bootstrap rules: %w", err)
		}
	}

	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}

	return &Engine{interp: interp, timeout: timeout}, nil
}

// NormalizeField se zeptá rule base, jestli syrový název pole odpovídá
// nějakému aliasu (field_alias/2 v bootstrap.pl). Pokud ne, vrátí vstup
// beze změny — je považován za už kanonický.
func (e *Engine) NormalizeField(ctx context.Context, raw string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	sols, err := e.interp.QueryContext(ctx, `field_alias(Raw, Canonical).`, map[string]interface{}{
		"Raw": raw,
	})
	if err != nil {
		return raw, err
	}
	defer sols.Close()

	if sols.Next() {
		var out struct{ Canonical string }
		if err := sols.Scan(&out); err != nil {
			return raw, err
		}
		return out.Canonical, nil
	}

	return raw, nil
}

// Validate ověří klauzuli proti guard pravidlům (valid_clause/3 v bootstrap.pl),
// například proti allow-listu polí nebo typové kontrole u rozsahových dotazů.
func (e *Engine) Validate(ctx context.Context, field, op, value string) error {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	sols, err := e.interp.QueryContext(ctx, `valid_clause(Field, Op, Value).`, map[string]interface{}{
		"Field": field,
		"Op":    op,
		"Value": value,
	})
	if err != nil {
		return err
	}
	defer sols.Close()

	if !sols.Next() {
		return fmt.Errorf("clause rejected by rule base: %s %s %s", field, op, value)
	}

	return nil
}

// Close uvolní případné prostředky enginu (v základní podobě není potřeba,
// ale rozhraní na to počítá pro budoucí implementace se sub-procesem).
func (e *Engine) Close() error {
	return nil
}
