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
	"strconv"
	"strings"
	"time"
)

const (
	requestTimeout = 60 * time.Second
	maxAttempts    = 5
	firstBackoff   = time.Second
	maxBackoff     = 30 * time.Second
	// A longer Retry-After means the hourly budget is used up; failing beats a CI job that sleeps for up to an hour.
	maxRetryAfter = time.Minute
)

// Client calls the REST API of one Weblate instance.
type Client struct {
	apiURL *url.URL
	token  string
	http   *http.Client
	sleep  func(context.Context, time.Duration) error
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
		http:  &http.Client{Timeout: requestTimeout},
		sleep: sleep,
	}, nil
}

// Do sends a request to ref and decodes the JSON response into out, unless out is nil. ref is either a path relative
// to the API root, such as "projects/docs/", or an absolute URL taken from a Weblate response.
//
// Do retries a 429 response for every method, and 502, 503, 504 and connection errors for every method except POST,
// because a repeated POST could create an object twice.
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

	for attempt := 1; ; attempt++ {
		resp, data, err := c.send(ctx, method, target, payload)
		var wait time.Duration
		retry := false
		if err != nil {
			retry = method != http.MethodPost && ctx.Err() == nil
		} else {
			err = decode(method, target, resp, data, out)
			switch resp.StatusCode {
			case http.StatusTooManyRequests:
				wait = retryAfter(resp.Header)
				retry = wait <= maxRetryAfter
			case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				retry = method != http.MethodPost
			}
		}
		if !retry || attempt == maxAttempts {
			return err
		}
		if wait == 0 {
			wait = min(firstBackoff<<(attempt-1), maxBackoff)
		}
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, method, target string, payload []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return resp, data, err
}

func decode(method, target string, resp *http.Response, data []byte, out any) error {
	jsonBody := isJSON(resp.Header.Get("Content-Type"))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !success || (out != nil && !jsonBody) {
		apiErr := &APIError{Method: method, URL: target, StatusCode: resp.StatusCode, NotJSON: !jsonBody}
		if jsonBody {
			apiErr.Body = string(data)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			apiErr.RetryAfter = retryAfter(resp.Header)
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

// retryAfter reads Retry-After in seconds, the form Weblate sends. It returns 0 when the header is missing.
func retryAfter(h http.Header) time.Duration {
	seconds, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) resolve(ref string) (string, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		// Weblate builds absolute URLs from its site domain, which can differ from the URL the provider was given.
		u.Scheme, u.Host, u.User = c.apiURL.Scheme, c.apiURL.Host, nil
		return u.String(), nil
	}
	return c.apiURL.ResolveReference(u).String(), nil
}

type page[T any] struct {
	Next    string `json:"next"`
	Results []T    `json:"results"`
}

// List fetches every page of the list endpoint at ref.
func List[T any](ctx context.Context, c *Client, ref string) ([]T, error) {
	var all []T
	for {
		var p page[T]
		if err := c.Do(ctx, http.MethodGet, ref, nil, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		if p.Next == "" {
			return all, nil
		}
		ref = p.Next
	}
}
