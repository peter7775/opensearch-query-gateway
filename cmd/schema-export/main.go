// schema-export převede OpenSearch mapping na Prolog fakta (pro rule engine)
// nebo na lidsky čitelný JSON přehled.
//
// Mapping lze načíst ze souboru (-in) nebo přímo z clusteru (-url + -index):
//
//	go run ./cmd/schema-export -in mapping.json -out schema.pl -root logs
//	go run ./cmd/schema-export -url http://localhost:9200 -index logs -out schema.pl
//	go run ./cmd/schema-export -in mapping.json -format json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/peter7775/opensearch-query-gateway/internal/config"
	"github.com/peter7775/opensearch-query-gateway/internal/executor"
	"github.com/peter7775/opensearch-query-gateway/internal/schema"
)

func main() {
	inPath := flag.String("in", "", "input JSON mapping file")
	outPath := flag.String("out", "", "output file (default stdout)")
	root := flag.String("root", "", "index name used in generated facts (default: -index or \"logs\")")
	url := flag.String("url", "", "OpenSearch URL(s), comma separated — fetch mapping from a live cluster")
	index := flag.String("index", "", "index to fetch from the cluster (with -url)")
	user := flag.String("user", os.Getenv("OPENSEARCH_USERNAME"), "OpenSearch username (env OPENSEARCH_USERNAME)")
	pass := flag.String("pass", os.Getenv("OPENSEARCH_PASSWORD"), "OpenSearch password (env OPENSEARCH_PASSWORD)")
	insecure := flag.Bool("insecure", false, "skip TLS certificate verification")
	format := flag.String("format", "prolog", "output format: prolog | json")
	purpose := flag.String("purpose", "", "optional index purpose for -format json")
	flag.Parse()

	if err := run(*inPath, *outPath, *root, *url, *index, *user, *pass, *insecure, *format, *purpose); err != nil {
		fmt.Fprintln(os.Stderr, "schema-export:", err)
		os.Exit(1)
	}
}

func run(inPath, outPath, root, url, index, user, pass string, insecure bool, format, purpose string) error {
	var raw []byte
	switch {
	case inPath != "" && url != "":
		return fmt.Errorf("use either -in or -url, not both")
	case inPath != "":
		var err error
		if raw, err = os.ReadFile(inPath); err != nil {
			return err
		}
	case url != "":
		if index == "" {
			return fmt.Errorf("-url requires -index")
		}
		client, err := executor.NewClient(config.OpenSearchConfig{
			Addresses:          strings.Split(url, ","),
			Username:           user,
			Password:           pass,
			Index:              index,
			InsecureSkipVerify: insecure,
		})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if raw, err = client.GetMapping(ctx, index); err != nil {
			return err
		}
	default:
		return fmt.Errorf("missing -in or -url")
	}

	if root == "" {
		root = index
	}
	if root == "" {
		root = "logs"
	}

	sch, err := schema.NewMapper().FromJSON(root, raw)
	if err != nil {
		return err
	}

	var out string
	switch format {
	case "prolog":
		out = schema.NewPrologWriter().Write(sch)
	case "json":
		b, err := json.MarshalIndent(schema.Analyze(sch, purpose), "", "  ")
		if err != nil {
			return err
		}
		out = string(b) + "\n"
	default:
		return fmt.Errorf("unknown -format %q (use prolog or json)", format)
	}

	if outPath != "" {
		return os.WriteFile(outPath, []byte(out), 0o644)
	}
	fmt.Print(out)
	return nil
}
