// Package client is the hand-written Weblate REST API client (ADR 0001).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const requestTimeout = 60 * time.Second

// Client calls the REST API of one Weblate instance.
type Client struct {
	apiURL *url.URL
	token  string
	http   *http.Client
}

// New returns a client for the Weblate instance at baseURL, such as https://weblate.example.com or
// https://example.com/weblate. The client appends /api/ itself.
func New(baseURL, token string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("the URL must start with http:// or https://")
	}
	if u.Host == "" {
		return nil, errors.New("the URL has no host")
	}
	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/api") {
		return nil, errors.New("the URL must not end in /api: the provider appends /api/ itself")
	}
	u.Path = path + "/api/"
	return &Client{
		apiURL: u,
		token:  token,
		// The zero Transport is http.DefaultTransport, which takes the proxy from HTTPS_PROXY and NO_PROXY.
		http: &http.Client{Timeout: requestTimeout},
	}, nil
}

// Do sends a request to ref and decodes the JSON response into out, unless out is nil. ref is either a path relative
// to the API root, such as "projects/docs/", or an absolute URL taken from a Weblate response.
func (c *Client) Do(ctx context.Context, method, ref string, body, out any) error {
	target, err := c.resolve(ref)
	if err != nil {
		return err
	}
	var payload []byte
	if body != nil {
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	jsonBody := isJSON(resp.Header.Get("Content-Type"))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !success || (out != nil && !jsonBody) {
		apiErr := &APIError{Method: method, URL: target, StatusCode: resp.StatusCode, NotJSON: !jsonBody}
		if jsonBody {
			apiErr.Body = string(data)
		}
		return apiErr
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding the response of %s %s: %w", method, target, err)
		}
	}
	return nil
}

func (c *Client) resolve(ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return c.apiURL.ResolveReference(u).String(), nil
}
