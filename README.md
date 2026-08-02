# opensearch-query-gateway

A gateway service that accepts queries in its own query language,
parses, normalises and validates them using an embedded Prolog rule engine,
converts them into OpenSearch Query DSL and executes the query.

The architecture consists of a single service, internally divided into clearly separated packages
(not microservices) — see `internal/`. The reason is explained in the README sections
below and in the accompanying discussion: parsing → rules → build → execute is a single
synchronous pipeline per request; splitting this into network services would simply
add latency without providing any benefit.

## Structure

```
cmd/gateway/ – entry point, wiring of all components
internal/api/ – HTTP layer (router, handler)
internal/config/ – loading configuration from YAML
internal/parser/ – lexer/grammar for the custom query language (participle) → AST
internal/rules/ – embedded Prolog rule engine (ichiban/prolog) on top of the AST
internal/dslbuilder/ – conversion of the normalised AST to OpenSearch Query DSL
internal/executor/ – client built on top of opensearch-go, executes the query itself
configs/ – config.yaml
deploy/ – Dockerfile, Docker Compose for local OpenSearch + gateway
```

## Single-request pipeline

```
POST /v1/search {‘query’: ‘tag:golang AND published_at:[2024-01-01 TO 2024-12-31]’}
      │
      ▼
parser.Parse → AST (Clauses)
      │
      ▼
rules.NormalizeField → field aliases (field_alias/2 in bootstrap.pl)
rules.Validate → guards over the AST (valid_clause/3 in bootstrap.pl)
      │
      ▼
dslbuilder.Build → map[string]interface{} (OpenSearch query DSL)
      │
      ▼
executor.Search → executes the query via opensearch-go
      │
      ▼
JSON response to the client
```

## Running locally

```bash
docker compose -f deploy/docker-compose.yaml up -d opensearch
go run ./cmd/gateway
```

## Query language

The grammar (`internal/parser/ast.go`, `grammar.go`) is a recursive expression with
the following precedence: `OR` < `AND` < `NOT` and support for nested parentheses:

```
tag:golang → match
title:jarn* → wildcard (* = any number of characters)
code:a?c → wildcard (? = one character)
title:‘hello world’ → match_phrase
published_at:[2024-01-01 TO 2024-12-31] → range
tag:golang status:published → implicit AND
 
(a space is sufficient)
tag:golang AND status:published → explicit AND, same effect as above
tag:golang OR tag:go → bool.should, minimum_should_match: 1
tag:golang NOT status:archived → AND NOT, ‘AND’ can be omitted
(tag:golang OR tag:go) AND NOT status:archived → arbitrary nesting within brackets
tag:golang^2.5 → match with boost 2.5 (relevance weight)
title:jarnik~2 → fuzzy query, edit distance 2
title:jarnik~ → fuzzy query, fuzziness ‘AUTO’
title:‘hello world’~3 → match_phrase with a tolerance of 3 (proximity)
title:jarnik~2^1.5 → combined fuzzy + boost
```

### Boost (^N) and fuzzy/proximity (~N)

Both modifiers are optional and can be chained together (`~` before `^`,
as in Lucene) with any type of value:

- `^N` — boost, relevance weight. Works on words, phrases, wildcards and range
  queries; if an error occurs or the number is invalid, 1 (neutral) is used.
- `~N` on a word → fuzzy query (editing distance, typo tolerance).
  
Without a number (`~` without a value), `‘AUTO’` is used — OpenSearch selects
  the distance based on the term’s length.
- `~N` on a phrase → slop for `match_phrase` (how many extra shifts/spaces
  between tokens are tolerated). Without a number, the default slop of 2 is used.
- `~N` on a wildcard value (containing `*`/`?`) is ignored — OpenSearch
does not combine fuzzy wildcards, but the boost is still applied.
- `~N` on a range value is ignored (it makes no sense), but the boost is applied.

Detection of which value type the modifier belongs to is handled in `internal/dslbuilder/builder.go`
within the `clause()` function. Group-level boost (`(expr)^2`) is not implemented —
boost/fuzzy applies only to individual field:value clauses.

Wildcard values are detected in `dslbuilder` based on the presence of `*`/`?` and
translated to an OpenSearch `wildcard` query — the glob syntax is identical, so
it is not translated; only `case_insensitive: true` is added. Note: `wildcard` queries
only work reliably on keyword/not-analysed fields, not on analysed
text — check the mapping of the target field.

Further natural extensions (not yet implemented): fuzzy queries (`word~2`),
boost (`word^2`), proximity phrases (`‘a b’~3`), default fields without the
`field:` prefix. The grammar simply needs to be extended with another alternative in `Value`/`Clause`
without affecting the rest of the pipeline.

Regression tests are in `internal/parser/grammar_test.go` (parsing/precedence)
and `internal/dslbuilder/builder_test.go` (resulting DSL) — run `make test`.

## Notes on further extension
- `internal/rules/bootstrap.pl` is where business rules belong
(field aliases, allow-lists, type validation) — keep them separate from the Go code,
so that you can change them without a rebuild, provided the `rules` package supports
hot-reloading.
- The rule engine API (`ichiban/prolog`) in `internal/rules/engine.go` is written
against the concept of a `database/sql`-like interface for that library; check the exact names
of the methods against the version you lock in `go.mod` — the API varies slightly
between versions.
- The timeout for Prolog evaluation (`rules.Engine.timeout`) is a necessary safeguard
against runaway backtracking on pathological input.

## Middleware

`internal/api/middleware.go` contains two middleware components:

- `LoggingMiddleware` — logs the method, path, status code, duration
and client IP for each request. Applied globally in `NewRouter`.
- `RateLimiter` — a per-client token bucket (`golang.org/x/time/rate`),
  keyed by IP (or `X-Forwarded-For` behind a proxy). Inactive limiters
  are released from memory after `ttl`. Applied only to `/v1/search`, not to
  `/healthz`. Configuration in `configs/config.yaml` under `rate_limit`.

