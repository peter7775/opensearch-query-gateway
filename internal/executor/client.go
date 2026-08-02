package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	opensearch "github.com/opensearch-project/opensearch-go/v2"
	"github.com/opensearch-project/opensearch-go/v2/opensearchapi"

	"github.com/example/opensearch-query-gateway/internal/config"
)

// Client obaluje opensearch-go a provádí sestavené dotazy proti clusteru.
type Client struct {
	os    *opensearch.Client
	index string
}

func NewClient(cfg config.OpenSearchConfig) (*Client, error) {
	c, err := opensearch.NewClient(opensearch.Config{
		Addresses: cfg.Addresses,
		Username:  cfg.Username,
		Password:  cfg.Password,
	})
	if err != nil {
		return nil, err
	}
	return &Client{os: c, index: cfg.Index}, nil
}

// Search provede dotaz vytvořený dslbuilder.Build proti nakonfigurovanému indexu.
func (c *Client) Search(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req := opensearchapi.SearchRequest{
		Index: []string{c.index},
		Body:  bytes.NewReader(buf),
	}

	res, err := req.Do(ctx, c.os)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("opensearch error: %s", res.Status())
	}

	var out map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
