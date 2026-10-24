# OpenSearch Query Gateway

**Query OpenSearch without writing complex JSON.**

A Go HTTP gateway that turns readable search expressions into OpenSearch Query DSL, validates fields with Prolog rules, and helps you inspect index mappings.

Use it to give applications and internal tools a consistent search API without duplicating query-building and validation logic in every client.

## From a readable query to OpenSearch DSL

Send a query using the aliases included in the default rules:

```bash
curl -s http://localhost:8080/v1/translate \
  -H 'Content-Type: application/json' \
  -d '{"query": "author:petr AND views:>=100"}'
```

The `dsl` field in the response contains:

```json
{
  "query": {
    "bool": {
      "must": [
        {"match": {"created_by": "petr"}},
        {"range": {"views": {"gte": "100"}}}
      ]
    }
  },
  "size": 10
}
```

The gateway normalizes `author` to `created_by` and validates both clauses before building the DSL. `/v1/translate` does not send a search request to OpenSearch; use `/v1/search` to execute the query.

## Why use it?

- **Write less JSON:** combine field searches, ranges, relative time windows, fuzzy matches, and Boolean expressions in a compact query language.
- **Validate before searching:** reject unknown fields and unsupported range operations using configurable rules.
- **Keep client queries stable:** map friendly field aliases to index field names in one place.
- **Understand your mappings:** inspect field types, supported operators, and schema notes over HTTP, or export mappings as Prolog facts.
- **Centralize request limits:** configure allowed indices, pagination bounds, request body limits, and per-client rate limits.

### When to use this instead of raw Query DSL

Use the gateway when several clients need the same field aliases, validation rules, and readable query format. Compared with passing user input directly to OpenSearch's native `query_string`, it adds a separate normalization and rule-validation layer and exposes the generated DSL for inspection.

Use raw Query DSL when you need the full OpenSearch feature set, such as aggregations or nested queries. This gateway implements a subset of query operations; it is not a drop-in replacement for the OpenSearch API. You do not need to write Prolog to try the bundled examples.

---

## Quick start

```bash
# OpenSearch + sample data + gateway
docker compose -f deploy/docker-compose.yaml up --build -d

# or locally (needs OpenSearch on http://localhost:9200)
./deploy/seed/seed.sh http://localhost:9200
make run
```

```bash
curl -s localhost:8080/v1/search -d '{"query": "author:petr AND views:>=100", "sort": ["published_at:desc"]}'
curl -s localhost:8080/v1/translate -d '{"query": "(tag:golang OR tag:go) and not status:archived"}'
curl -s localhost:8080/v1/schema/introspect
```

---

## Query language

| Syntax | Meaning | OpenSearch DSL |
|---|---|---|
| `title:golang` | match | `match` |
| `title:"query dsl"` | phrase | `match_phrase` |
| `title:"query dsl"~3` | phrase with slop 3 | `match_phrase` + `slop` |
| `tags:go*`, `tags:g?` | wildcard (`*`, `?`) | `wildcard` (case-insensitive) |
| `title:golnag~`, `title:golnag~1` | fuzzy (AUTO or edit distance) | `fuzzy` |
| `title:go^2` | boost (combinable: `go~1^1.5`) | `boost` |
| `views:[100 TO 500]`, `views:[100 TO *]` | inclusive range, `*` = open bound | `range` gte/lte |
| `views:>100`, `>=`, `<`, `<=` | one-sided comparison | `range` gt/gte/lt/lte |
| `published_at:["2024-01-01T00:00:00" TO now]` | quoted bounds for values with `:` | `range` |
| `last:15m` (`s m h H d w M y`) | relative time window on `opensearch.time_field` | `range` gte `now-15m` |
| `a:x AND b:y`, `a:x b:y` | AND (explicit or implicit) | `bool.must` |
| `a:x OR b:y` | OR | `bool.should` + `minimum_should_match: 1` |
| `NOT a:x` | negation | `bool.must_not` |
| `( … )` | grouping, arbitrary nesting | nested `bool` |

Keywords `AND`, `OR`, `NOT`, `TO` are case-insensitive, so the example
`service:gateway and severity:error and last:15m` works as written.
Precedence: `NOT` > `AND` > `OR`.

---

## Query processing flow

```text
POST /v1/search {"query": "..."}
→ parser (participle grammar)           internal/parser
→ AST
→ rules: normalize field aliases        internal/rules (Prolog: field_alias/2)
→ rules: validate every clause          internal/rules (Prolog: valid_clause/3)
→ DSL builder (+ size/from/sort)        internal/dslbuilder
→ OpenSearch                            internal/executor
→ response
```

Every clause, including those nested deep inside parentheses, goes through
normalization and validation. Field arguments are passed to Prolog as bound
terms, never interpolated into query text.

---

## HTTP API

All errors share one shape: `{"error": {"code": "...", "message": "..."}}`.
Every response carries an `X-Request-ID` header (taken from the request or generated).

### `POST /v1/search`

```json
{
  "query": "author:petr AND tag:go* AND views:>=10",
  "index": "documents",
  "size": 10,
  "from": 0,
  "sort": ["published_at:desc", "_score"],
  "explain": false
}
```

Only `query` is required. `index` must be the configured index or listed in
`opensearch.allowed_indices`. `size`/`from` are bounded by `search.max_size`
and `search.max_from` (deep from/size paging is expensive on large clusters).
Sort fields go through the same alias normalization and allow-list as query
fields. The response is the OpenSearch `_search` response; with
`"explain": true` it is wrapped as `{"explain": {...}, "result": {...}}`.

### `POST /v1/translate`

Dry run — same request body, returns the generated DSL and applied field
normalizations without calling OpenSearch:

```json
{
  "index": "documents",
  "query": "tag:go",
  "dsl": {"query": {"match": {"tags": "go"}}, "size": 10},
  "normalized_fields": [{"from": "tag", "to": "tags"}]
}
```

### `GET /v1/schema/introspect?index=…`

Human-readable overview of an index mapping fetched from the cluster
(summary, highlights, per-field searchable/aggregatable flags, supported
operators and notes). `POST` to the same path analyses a mapping sent in the
request body instead (any of: bare `properties`, create-index body, or
`GET /<index>/_mapping` response).

### `GET /v1/schema/prolog?index=…`

The same schema as Prolog facts (identical to `schema-export` output).
`POST` accepts a mapping in the body.

### `GET /healthz`, `GET /readyz`

Liveness (process up) and readiness (OpenSearch reachable). Not rate limited.

| Status | Code | When |
|---|---|---|
| 400 | `invalid_body`, `empty_query`, `invalid_query` | malformed JSON, unknown JSON fields, syntax errors |
| 400 | `clause_rejected` | rule base rejected a clause (unknown field, range over text…) |
| 400 | `invalid_size`, `invalid_from`, `invalid_sort` | paging/sort limits |
| 400 | `opensearch_rejected` | query passed validation but the cluster refused it |
| 403 | `index_not_allowed` | index not in the allow-list |
| 404 | `index_not_found` | index does not exist |
| 413 | `body_too_large` | request over `server.max_body_bytes` |
| 429 | `rate_limited` | per-client token bucket exhausted |
| 502/504 | `opensearch_error`, `upstream_error`, `timeout` | cluster problems |

---

## Rules (Prolog)

`internal/rules/bootstrap.pl` holds the business rules, loaded at startup:

```prolog
field_alias(author, created_by).          % normalization
known_field(title).                       % allow-list
numeric_field(views).                     % range-capable
date_field(published_at).
valid_clause(Field, eq, _)    :- known_field(Field), !.
valid_clause(Field, range, _) :- known_field(Field), range_field(Field), !.
rejection_reason(Field, _, _, 'unknown field') :- \+ known_field(Field), !.
```

Instead of maintaining the allow-list by hand, generate facts from the real
mapping and list the file in `rules.schema_files`:

```bash
go run ./cmd/schema-export -url http://localhost:9200 -index documents -out configs/schema_documents.pl
```

Fields from those facts become known automatically, numeric/date/ip/keyword
fields allow ranges, and object fields are not directly queryable.

Files ending in `.dcg` are compiled from DCG notation (`head --> body.`) into
Prolog by `internal/dcg` before loading, so you can add grammar-based checks
(e.g. allowed value vocabularies) to the rule base.

---

## Configuration

`configs/config.yaml` (path override: `GATEWAY_CONFIG`). Environment overrides:

| Variable | Config key |
|---|---|
| `GATEWAY_SERVER_ADDR` | `server.addr` |
| `GATEWAY_TRUST_PROXY_HEADERS` | `server.trust_proxy_headers` |
| `GATEWAY_OPENSEARCH_ADDRESSES` (comma separated) | `opensearch.addresses` |
| `GATEWAY_OPENSEARCH_USERNAME` / `_PASSWORD` | `opensearch.username` / `password` |
| `GATEWAY_OPENSEARCH_INDEX` | `opensearch.index` |
| `GATEWAY_OPENSEARCH_INSECURE_SKIP_VERIFY` | `opensearch.insecure_skip_verify` |
| `GATEWAY_OPENSEARCH_TIME_FIELD` | `opensearch.time_field` |
| `GATEWAY_RULES_BOOTSTRAP_FILE` | `rules.bootstrap_file` |
| `GATEWAY_RULES_SCHEMA_FILES` (comma separated) | `rules.schema_files` |

`server.trust_proxy_headers` makes the rate limiter key on the first
`X-Forwarded-For` address — enable it only behind a trusted reverse proxy.

---

## Repository layout

```text
cmd/
  gateway/         HTTP service
  schema-export/   mapping → Prolog facts / JSON overview (file or live cluster)
configs/           config.yaml
deploy/            Dockerfile, docker-compose, seed data
internal/
  api/             handlers, middleware (rate limit, logging, request ID, recover), router
  config/          YAML + env configuration
  dcg/             DCG lexer, parser and Prolog generator
  dslbuilder/      AST → OpenSearch Query DSL
  executor/        OpenSearch client (search, mapping, ping, error decoding)
  parser/          query language grammar and AST
  rules/           Prolog rule engine + bootstrap.pl
  schema/          mapping mapper, analyzer (introspection), Prolog writer
```

---

## Development

```bash
make test        # go test ./...
make test-race
make lint        # go vet + gofmt check
make build       # bin/gateway, bin/schema-export
```

---

## CLI tools

### `schema-export`

```bash
go run ./cmd/schema-export -in mapping.json -out schema.pl -root logs
go run ./cmd/schema-export -url http://localhost:9200 -index logs -out schema.pl
go run ./cmd/schema-export -in mapping.json -format json -purpose "Application logs"
```

Credentials: `-user`/`-pass` or `OPENSEARCH_USERNAME`/`OPENSEARCH_PASSWORD`;
`-insecure` skips TLS verification.

---

## Example schema introspection response

```json
{
  "index": "logs",
  "purpose": "Application and audit log search",
  "summary": {
    "fields_total": 10,
    "searchable": 7,
    "aggregatable": 6,
    "object_fields": 1,
    "nested_fields": 1
  },
  "highlights": [
    "Time-based filtering is supported (@timestamp).",
    "service, severity and tags.name are ideal for filters and grouping.",
    "message supports full-text search.",
    "Nested structures (tags) need nested queries."
  ],
  "fields": [
    {
      "path": "@timestamp",
      "type": "date",
      "searchable": true,
      "aggregatable": true,
      "operators": ["match", "range", "compare", "relative"],
      "notes": "Primary time filter"
    }
  ]
}
```

---

## Scope and performance

The gateway builds and validates queries; OpenSearch executes them. Search performance still depends on your mappings, query patterns, shard layout, hardware, and data volume. No large-cluster benchmark results are provided here.

Pagination bounds and request rate limits help constrain client requests, but do not guarantee that every accepted query is inexpensive. Benchmark representative workloads against your own cluster before production use.

The user-facing query parser is written in Go with participle. Prolog handles field aliases and validation rules; optional `.dcg` files are compiled into Prolog for additional grammar-based rules.

---

## License

This project is free for non-commercial use. Commercial use requires a paid
license. See [LICENSE](LICENSE) for full terms, or contact
petrstepanek99@proton.me to obtain a commercial license.

