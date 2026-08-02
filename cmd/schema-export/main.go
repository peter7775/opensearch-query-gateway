package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/example/opensearch-query-gateway/internal/schema"
)

func main() {
	inPath := flag.String("in", "", "input JSON mapping file")
	outPath := flag.String("out", "", "output Prolog file")
	root := flag.String("root", "logs", "index name")
	flag.Parse()

	if *inPath == "" {
		fmt.Fprintln(os.Stderr, "missing -in")
		os.Exit(1)
	}

	raw, err := os.ReadFile(*inPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mapper := schema.NewMapper()
	sch, err := mapper.FromJSON(*root, raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	writer := schema.NewPrologWriter()
	out := writer.Write(sch)

	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Print(out)
}
