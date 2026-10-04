package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("opensearch:\n  addresses: [\"http://a:9200\"]\n  index: docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GATEWAY_OPENSEARCH_ADDRESSES", "http://x:9200, http://y:9200")
	t.Setenv("GATEWAY_OPENSEARCH_PASSWORD", "secret")
	t.Setenv("GATEWAY_RULES_SCHEMA_FILES", "a.pl,b.pl")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OpenSearch.Addresses) != 2 || cfg.OpenSearch.Addresses[1] != "http://y:9200" {
		t.Errorf("addresses: %v", cfg.OpenSearch.Addresses)
	}
	if cfg.OpenSearch.Password != "secret" || len(cfg.Rules.SchemaFiles) != 2 {
		t.Errorf("env overrides not applied: %+v", cfg)
	}
	if cfg.Server.Addr != ":8080" || cfg.Rules.Timeout != 200*time.Millisecond || cfg.Search.MaxSize != 100 || cfg.OpenSearch.TimeField != "@timestamp" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if !cfg.OpenSearch.IndexAllowed("docs") || cfg.OpenSearch.IndexAllowed("other") {
		t.Error("IndexAllowed")
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	_ = os.WriteFile(path, []byte("opensearch:\n  index: docs\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("expected error for missing addresses")
	}
}
