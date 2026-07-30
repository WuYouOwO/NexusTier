package gatewayclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxTopologyResponseBytes = 16 << 20

type Client struct {
	topologyURL *url.URL
	httpClient  *http.Client
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return nil, fmt.Errorf("parse gateway URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("gateway URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("gateway URL must include a host and must not include user info")
	}
	return &Client{
		topologyURL: parsed.ResolveReference(&url.URL{Path: "v1/topology"}),
		httpClient:  &http.Client{Timeout: timeout},
	}, nil
}

func (client *Client) FetchTopology(ctx context.Context) (Snapshot, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.topologyURL.String(), nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("create topology request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return Snapshot{}, fmt.Errorf("fetch gateway topology: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return Snapshot{}, fmt.Errorf("gateway topology returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(io.LimitReader(response.Body, maxTopologyResponseBytes+1))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode topology response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Snapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return Snapshot{}, fmt.Errorf("validate topology response: %w", err)
	}
	return snapshot, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode topology response: unexpected trailing JSON value")
		}
		return fmt.Errorf("decode topology response trailer: %w", err)
	}
	return nil
}
