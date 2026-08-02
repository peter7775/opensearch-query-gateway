package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config drží kompletní konfiguraci služby, načtenou z YAML souboru.
type Config struct {
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`

	OpenSearch OpenSearchConfig `yaml:"opensearch"`

	Rules struct {
		BootstrapFile string        `yaml:"bootstrap_file"`
		Timeout       time.Duration `yaml:"timeout"`
	} `yaml:"rules"`

	RateLimit struct {
		RPS   float64       `yaml:"rps"`
		Burst int           `yaml:"burst"`
		TTL   time.Duration `yaml:"ttl"`
	} `yaml:"rate_limit"`
}

// OpenSearchConfig obsahuje připojovací údaje k OpenSearch clusteru.
type OpenSearchConfig struct {
	Addresses []string `yaml:"addresses"`
	Username  string   `yaml:"username"`
	Password  string   `yaml:"password"`
	Index     string   `yaml:"index"`
}

// Load načte konfiguraci ze souboru. Pokud path je prázdný, použije se
// výchozí umístění configs/config.yaml relativně k pracovnímu adresáři.
func Load(path string) (*Config, error) {
	if path == "" {
		path = "configs/config.yaml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Rules.Timeout == 0 {
		cfg.Rules.Timeout = 200 * time.Millisecond
	}
	if cfg.RateLimit.RPS == 0 {
		cfg.RateLimit.RPS = 5
	}
	if cfg.RateLimit.Burst == 0 {
		cfg.RateLimit.Burst = 10
	}
	if cfg.RateLimit.TTL == 0 {
		cfg.RateLimit.TTL = 10 * time.Minute
	}

	return &cfg, nil
}
