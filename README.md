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

## Repository layout

```text
bin/
cmd/
  gateway/
  schema-export/
configs/
deploy/
internal/
  api/
  config/
  dcg/
  dslbuilder/
  executor/
  parser/
  rules/
  schema/
opensearch-query-gateway/
README.md
```

### `cmd/gateway`

Main HTTP service.

### `cmd/schema-export`

CLI tool that exports OpenSearch mappings into Prolog facts.

### `internal/api`

HTTP handlers, middleware, and router.

### `internal/dcg`

Lexer, parser, AST, and generator for DCG-like query syntax.

### `internal/dslbuilder`

Builds OpenSearch DSL from the internal query representation.

### `internal/parser`

A more general parser / grammar layer for query input.

### `internal/rules`

Prolog bootstrap and rule engine.

### `internal/schema`

OpenSearch mapping mapper, Prolog writer, and schema model.

### `internal/executor`

Communication with the OpenSearch cluster.

---

## Query processing flow

```text
Input from API / UI
→ DCG or Prolog-like syntax
→ parser
→ AST
→ rules / validation
→ DSL builder
→ OpenSearch Query DSL
→ OpenSearch cluster
→ response
```

---

## Schema analyzer

The schema analyzer reads OpenSearch mappings and converts them into:

- readable JSON for humans,
- Prolog facts for internal logic,
- metadata for autocomplete and explain mode.

The typical output includes:

- index name,
- total fields,
- searchable fields,
- aggregatable fields,
- object and nested structures,
- recommended field usage.

---

## Example use cases

### Log search

- errors in the last 15 minutes,
- filtering by service, severity, or tenant,
- full-text search over the message field.

### Audit and security logs

- finding changes by users,
- tracing request paths and outcomes,
- reviewing who did what and when.

### Schema introspection

- showing which fields an index contains,
- identifying fields suitable for filtering and aggregation,
- connecting to a visual builder or documentation layer.

---

## CLI tools

### `schema-export`

Exports OpenSearch mappings into Prolog facts.

Example:

```bash
go run ./cmd/schema-export -in mapping.json -out schema.pl -root logs
```

---

## Endpoints

### `/search`

Main query endpoint.

### `/schema/introspect`

Returns a human-readable JSON overview of an index schema.

---

## Example schema introspection response

```json
{
  "index": "logs",
  "purpose": "Application and audit log search",
  "summary": {
    "fields_total": 6,
    "searchable": 4,
    "aggregatable": 5,
    "object_fields": 2,
    "nested_fields": 0
  },
  "highlights": [
    "Time-based filtering is supported.",
    "Service and severity are ideal for filters and grouping.",
    "Message supports full-text search."
  ],
  "fields": [
    {
      "path": "@timestamp",
      "type": "date",
      "searchable": true,
      "aggregatable": true,
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

