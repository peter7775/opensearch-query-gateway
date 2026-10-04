package executor

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	opensearch "github.com/opensearch-project/opensearch-go/v2"
	"github.com/opensearch-project/opensearch-go/v2/opensearchapi"

	"github.com/peter7775/opensearch-query-gateway/internal/config"
)

// Error je chyba vrácená OpenSearch clusterem (HTTP status >= 400).
type Error struct {
	Status int
	Type   string
	Reason string
}

func (e *Error) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("opensearch error %d (%s): %s", e.Status, e.Type, e.Reason)
	}
	return fmt.Sprintf("opensearch error %d", e.Status)
}

// Client obaluje opensearch-go a provádí sestavené dotazy proti clusteru.
type Client struct {
	os      *opensearch.Client
	index   string
	timeout time.Duration
}

func NewClient(cfg config.OpenSearchConfig) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicitně zapnuto v konfiguraci
	}

	c, err := opensearch.NewClient(opensearch.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
		Transport: transport,
	})
	if err != nil {
		return nil, err
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{os: c, index: cfg.Index, timeout: timeout}, nil
}

// DefaultIndex vrátí nakonfigurovaný výchozí index.
func (c *Client) DefaultIndex() string { return c.index }

// Search provede dotaz vytvořený dslbuilder proti zadanému indexu
// (prázdný index = výchozí z konfigurace).
func (c *Client) Search(ctx context.Context, index string, body map[string]interface{}) (map[string]interface{}, error) {
	if index == "" {
		index = c.index
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := opensearchapi.SearchRequest{
		Index: []string{index},
		Body:  bytes.NewReader(buf),
	}
	res, err := req.Do(ctx, c.os)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return decode(res)
}

// GetMapping vrátí surový mapping indexu (odpověď GET /<index>/_mapping).
func (c *Client) GetMapping(ctx context.Context, index string) ([]byte, error) {
	if index == "" {
		index = c.index
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req := opensearchapi.IndicesGetMappingRequest{Index: []string{index}}
	res, err := req.Do(ctx, c.os)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, errorFrom(res)
	}
	return io.ReadAll(io.LimitReader(res.Body, 32<<20))
}

// Ping ověří dostupnost clusteru (pro /readyz).
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	res, err := opensearchapi.PingRequest{}.Do(ctx, c.os)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return &Error{Status: res.StatusCode}
	}
	return nil
}

func decode(res *opensearchapi.Response) (map[string]interface{}, error) {
	if res.IsError() {
		return nil, errorFrom(res)
	}
	var out map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// errorFrom vytáhne z chybové odpovědi OpenSearch typ a důvod
// ({"error":{"type":..., "reason":..., "root_cause":[...]}}).
func errorFrom(res *opensearchapi.Response) error {
	e := &Error{Status: res.StatusCode}
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var body struct {
		Error struct {
			Type      string `json:"type"`
			Reason    string `json:"reason"`
			RootCause []struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"root_cause"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil {
		e.Type, e.Reason = body.Error.Type, body.Error.Reason
		if len(body.Error.RootCause) > 0 && body.Error.RootCause[0].Reason != "" {
			e.Type, e.Reason = body.Error.RootCause[0].Type, body.Error.RootCause[0].Reason
		}
	}
	return e
}
