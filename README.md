# OpenSearch Query Gateway

A gateway for querying OpenSearch with support for multiple query layers, including a DCG-based parser, DSL builder, rule engine, and schema analysis.

The project is designed for large-scale search in OpenSearch, including environments with billions of records, where query clarity, validation, performance, and explainable schema inspection are important.

---

## What the project solves

The goal of this project is to make OpenSearch easier to work with so that users and other systems do not need to manually assemble complex JSON queries.

The project provides:

- a **DCG parser** for readable query syntax,
- a **DSL builder** that converts queries into OpenSearch Query DSL,
- a **rules engine** for transformations and validations,
- a **schema analyzer** for index introspection and human-readable field overviews,
- an **API gateway** for exposing the functionality over HTTP.

---

## Core concepts

### 1. DCG

DCG is used as a readable query format that can be translated into an internal structure.

Example:

```prolog
service:gateway and severity:error and last:15m
```

### 2. Prolog rules

Prolog acts as the logical layer for rules, validation, and relationship inference.

Internally it is used for:
- query transformation,
- schema validation,
- explain mode,
- inference over fields and index relations.

### 3. OpenSearch DSL

The final query is generated as standard OpenSearch Query DSL.

### 4. Schema analyzer

The schema analyzer reads OpenSearch mappings and returns a human-readable overview of fields, types, relationships, and recommendations.

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

## Why it fits large OpenSearch clusters

This project is suitable for environments with billions of documents because it:

- simplifies query creation and validation,
- supports readable query syntax,
- allows automatic rule-based rewriting,
- provides a human-readable view of schema,
- can grow into autocomplete, explain mode, and a visual builder.

---

## Short summary

This project is a gateway and logic layer over OpenSearch that combines:

- readable query syntax,
- parser and DSL builder,
- rule engine,
- schema analysis,
- API for large datasets.

It is designed to be practical for production while still being understandable for a team that does not work with Prolog.

---

## License

This project is free for non-commercial use. Commercial use requires a paid
license. See [LICENSE](LICENSE) for full terms, or contact
petrstepanek99@proton.me to obtain a commercial license.

