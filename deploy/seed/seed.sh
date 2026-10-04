#!/bin/sh
# Vytvoří index "documents" s mappingem a nahraje ukázková data.
# Použití: seed.sh [OPENSEARCH_URL]   (výchozí http://localhost:9200)
set -eu
URL="${1:-http://localhost:9200}"
DIR="$(cd "$(dirname "$0")" && pwd)"

if curl -fs -o /dev/null "$URL/documents"; then
  echo "index documents already exists, skipping create"
else
  curl -fsS -X PUT "$URL/documents" -H 'Content-Type: application/json' \
    --data-binary "@$DIR/documents_mapping.json"
  echo
fi

curl -fsS -X POST "$URL/documents/_bulk?refresh=true" -H 'Content-Type: application/x-ndjson' \
  --data-binary "@$DIR/documents.ndjson" | head -c 200
echo
echo "seed done"
