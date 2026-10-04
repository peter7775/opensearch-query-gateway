package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config drží kompletní konfiguraci služby, načtenou z YAML souboru
// a volitelně přepsanou proměnnými prostředí (GATEWAY_*).
type Config struct {
	Server struct {
		Addr string `yaml:"addr"`
		// TrustProxyHeaders zapne čtení X-Forwarded-For / X-Real-IP pro
		// určení IP klienta. Zapínejte jen za důvěryhodnou reverzní proxy,
		// jinak si klient může IP pro rate limiting podvrhnout.
		TrustProxyHeaders bool  `yaml:"trust_proxy_headers"`
		MaxBodyBytes      int64 `yaml:"max_body_bytes"`
	} `yaml:"server"`

	OpenSearch OpenSearchConfig `yaml:"opensearch"`

	Rules struct {
		BootstrapFile string `yaml:"bootstrap_file"`
		// SchemaFiles jsou Prolog fakta vygenerovaná nástrojem schema-export;
		// pole z nich se automaticky stávají známými (known_field/1).
		SchemaFiles []string      `yaml:"schema_files"`
		Timeout     time.Duration `yaml:"timeout"`
	} `yaml:"rules"`

	Search SearchConfig `yaml:"search"`

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
	// Index je výchozí index (nebo alias / pattern) pro vyhledávání.
	Index string `yaml:"index"`
	// AllowedIndices jsou další indexy, které smí klient zvolit parametrem
	// "index". Prázdný seznam = povolený jen Index.
	AllowedIndices     []string      `yaml:"allowed_indices"`
	InsecureSkipVerify bool          `yaml:"insecure_skip_verify"`
	RequestTimeout     time.Duration `yaml:"request_timeout"`
	// TimeField je časové pole pro relativní filtr last:<duration>.
	TimeField string `yaml:"time_field"`
	// Purpose je volitelný popis indexu vracený v /v1/schema/introspect.
	Purpose string `yaml:"purpose"`
}

// SearchConfig omezuje stránkování — u indexů s miliardami dokumentů je
// hluboké stránkování přes from/size drahé, proto ho držíme v mezích.
type SearchConfig struct {
	DefaultSize    int  `yaml:"default_size"`
	MaxSize        int  `yaml:"max_size"`
	MaxFrom        int  `yaml:"max_from"`
	TrackTotalHits bool `yaml:"track_total_hits"`
}

// IndexAllowed vrátí true, pokud klient smí dotazovat zadaný index.
func (c OpenSearchConfig) IndexAllowed(index string) bool {
	if index == c.Index {
		return true
	}
	for _, i := range c.AllowedIndices {
		if i == index {
			return true
		}
	}
	return false
}

// Load načte konfiguraci ze souboru. Pokud path je prázdný, použije se
// výchozí umístění configs/config.yaml relativně k pracovnímu adresáři.
// Poté se aplikují přepisy z prostředí a výchozí hodnoty.
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

	if err := cfg.applyEnv(os.LookupEnv); err != nil {
		return nil, err
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (cfg *Config) applyDefaults() {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.MaxBodyBytes <= 0 {
		cfg.Server.MaxBodyBytes = 1 << 20
	}
	if cfg.Rules.Timeout == 0 {
		cfg.Rules.Timeout = 200 * time.Millisecond
	}
	if cfg.OpenSearch.RequestTimeout == 0 {
		cfg.OpenSearch.RequestTimeout = 10 * time.Second
	}
	if cfg.OpenSearch.TimeField == "" {
		cfg.OpenSearch.TimeField = "@timestamp"
	}
	if cfg.Search.DefaultSize <= 0 {
		cfg.Search.DefaultSize = 10
	}
	if cfg.Search.MaxSize <= 0 {
		cfg.Search.MaxSize = 100
	}
	if cfg.Search.MaxFrom <= 0 {
		cfg.Search.MaxFrom = 10000
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
}

// Validate zkontroluje povinné hodnoty.
func (cfg *Config) Validate() error {
	if len(cfg.OpenSearch.Addresses) == 0 {
		return fmt.Errorf("opensearch.addresses must not be empty")
	}
	if cfg.OpenSearch.Index == "" {
		return fmt.Errorf("opensearch.index must not be empty")
	}
	if cfg.Search.DefaultSize > cfg.Search.MaxSize {
		return fmt.Errorf("search.default_size (%d) exceeds search.max_size (%d)", cfg.Search.DefaultSize, cfg.Search.MaxSize)
	}
	return nil
}

// applyEnv přepíše hodnoty z proměnných prostředí. Hodí se hlavně pro
// Docker/Kubernetes, kde nechceme hesla držet v config souboru.
//
//	GATEWAY_SERVER_ADDR, GATEWAY_TRUST_PROXY_HEADERS,
//	GATEWAY_OPENSEARCH_ADDRESSES (čárkami oddělené), GATEWAY_OPENSEARCH_USERNAME,
//	GATEWAY_OPENSEARCH_PASSWORD, GATEWAY_OPENSEARCH_INDEX,
//	GATEWAY_OPENSEARCH_INSECURE_SKIP_VERIFY, GATEWAY_OPENSEARCH_TIME_FIELD,
//	GATEWAY_RULES_BOOTSTRAP_FILE, GATEWAY_RULES_SCHEMA_FILES (čárkami oddělené)
func (cfg *Config) applyEnv(lookup func(string) (string, bool)) error {
	str := func(key string, dst *string) {
		if v, ok := lookup(key); ok {
			*dst = v
		}
	}
	list := func(key string, dst *[]string) {
		if v, ok := lookup(key); ok {
			var out []string
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
			*dst = out
		}
	}
	boolean := func(key string, dst *bool) error {
		if v, ok := lookup(key); ok {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			*dst = b
		}
		return nil
	}

	str("GATEWAY_SERVER_ADDR", &cfg.Server.Addr)
	if err := boolean("GATEWAY_TRUST_PROXY_HEADERS", &cfg.Server.TrustProxyHeaders); err != nil {
		return err
	}
	list("GATEWAY_OPENSEARCH_ADDRESSES", &cfg.OpenSearch.Addresses)
	str("GATEWAY_OPENSEARCH_USERNAME", &cfg.OpenSearch.Username)
	str("GATEWAY_OPENSEARCH_PASSWORD", &cfg.OpenSearch.Password)
	str("GATEWAY_OPENSEARCH_INDEX", &cfg.OpenSearch.Index)
	str("GATEWAY_OPENSEARCH_TIME_FIELD", &cfg.OpenSearch.TimeField)
	if err := boolean("GATEWAY_OPENSEARCH_INSECURE_SKIP_VERIFY", &cfg.OpenSearch.InsecureSkipVerify); err != nil {
		return err
	}
	str("GATEWAY_RULES_BOOTSTRAP_FILE", &cfg.Rules.BootstrapFile)
	list("GATEWAY_RULES_SCHEMA_FILES", &cfg.Rules.SchemaFiles)
	return nil
}
