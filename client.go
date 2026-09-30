// Package avelin is an unofficial Go client for the AVELIN AI API
// (https://api.avelin.ai).
//
// It covers the OpenAI-compatible chat completions endpoint, the
// Anthropic-compatible messages endpoint, model listing, embeddings and the
// public model catalog. It uses only the standard library.
package avelin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the API root. Endpoint paths such as /v1/models are
	// appended to it, so it must not end in /v1.
	DefaultBaseURL = "https://api.avelin.ai"
	// DefaultMaxRetries is how many times a request is retried after a 429
	// or 5xx response.
	DefaultMaxRetries = 2
	// DefaultTimeout bounds each HTTP attempt. See WithTimeout.
	DefaultTimeout = 10 * time.Minute

	userAgent     = "avelin-go/0.1.0"
	maxBackoff    = 8 * time.Second
	maxRetryAfter = 60 * time.Second
)

// ErrMissingAPIKey is returned, without sending a request, when an endpoint
// that needs a key is called and no key is configured.
var ErrMissingAPIKey = errors.New("avelin: no API key: set AVELIN_API_KEY or use WithAPIKey")

// Client calls the AVELIN API. It is safe for concurrent use.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	timeout    time.Duration
	retryDelay time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key. It overrides the AVELIN_API_KEY environment
// variable.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithBaseURL sets the API root, for example a local mock server. It
// overrides the AVELIN_BASE_URL environment variable. Do not include /v1.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(url, "/") }
}

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithMaxRetries sets how many times a request is retried after a 429 or
// 5xx response. Zero disables retries.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = max(n, 0) }
}

// WithTimeout bounds each HTTP attempt. For regular calls it covers the whole
// response; for streams it covers only the wait for response headers, so a
// long stream is limited by its context alone. Zero disables the timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = max(d, 0) }
}

// NewClient returns a client. The API key defaults to the AVELIN_API_KEY
// environment variable and the base URL to AVELIN_BASE_URL, falling back to
// DefaultBaseURL.
func NewClient(opts ...Option) *Client {
	c := &Client{
		apiKey:     os.Getenv("AVELIN_API_KEY"),
		baseURL:    DefaultBaseURL,
		httpClient: http.DefaultClient,
		maxRetries: DefaultMaxRetries,
		timeout:    DefaultTimeout,
		retryDelay: 500 * time.Millisecond,
	}
	if u := os.Getenv("AVELIN_BASE_URL"); u != "" {
		c.baseURL = strings.TrimRight(u, "/")
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Meta is the HTTP metadata of the response that produced a result. AVELIN
// documents X-RateLimit-* headers; any other headers it sends, such as
// routing details, are visible here too.
type Meta struct {
	StatusCode int
	Header     http.Header
}

// RequestID returns the request ID header if the response had one. AVELIN
// does not document a header name, so this checks X-Request-Id, then
// Request-Id.
func (m Meta) RequestID() string {
	return requestID(m.Header)
}

func requestID(h http.Header) string {
	if id := h.Get("X-Request-Id"); id != "" {
		return id
	}
	return h.Get("Request-Id")
}

type request struct {
	method string
	path   string
	body   any
	header map[string]string
	public bool
	stream bool
}

// do sends r and decodes the JSON response body into out.
func (c *Client) do(ctx context.Context, r request, out any) (Meta, json.RawMessage, error) {
	resp, err := c.send(ctx, r)
	if err != nil {
		return Meta{}, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Meta{}, nil, fmt.Errorf("avelin: read response: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return Meta{}, nil, fmt.Errorf("avelin: decode response: %w", err)
	}
	return Meta{StatusCode: resp.StatusCode, Header: resp.Header}, data, nil
}

// send performs r, retrying 429 and 5xx responses, and returns the first 2xx
// response. The caller must close its body.
func (c *Client) send(ctx context.Context, r request) (*http.Response, error) {
	if !r.public && c.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return nil, fmt.Errorf("avelin: encode request: %w", err)
		}
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.attempt(ctx, r, payload)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		apiErr := newAPIError(resp)
		if attempt >= c.maxRetries || !retryable(resp.StatusCode) {
			return nil, apiErr
		}
		wait, ok := c.backoff(attempt, resp.Header)
		if !ok {
			return nil, apiErr
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) attempt(ctx context.Context, r request, payload []byte) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	actx, cancel := context.WithCancelCause(ctx)
	req, err := http.NewRequestWithContext(actx, r.method, c.baseURL+r.path, body)
	if err != nil {
		cancel(nil)
		return nil, fmt.Errorf("avelin: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if r.stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !r.public {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}

	var timer *time.Timer
	if c.timeout > 0 {
		d := c.timeout
		timer = time.AfterFunc(d, func() {
			cancel(fmt.Errorf("avelin: request timed out after %s: %w", d, context.DeadlineExceeded))
		})
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if timer != nil {
			timer.Stop()
		}
		if actx.Err() != nil && ctx.Err() == nil {
			err = context.Cause(actx)
		}
		cancel(nil)
		return nil, fmt.Errorf("avelin: %w", err)
	}
	if r.stream && timer != nil {
		timer.Stop()
	}
	resp.Body = &attemptBody{ReadCloser: resp.Body, parent: ctx, ctx: actx, cancel: cancel, timer: timer}
	return resp, nil
}

// attemptBody ties an attempt's context and timeout to the body's lifetime
// and reports a timeout as such rather than as a plain cancellation.
type attemptBody struct {
	io.ReadCloser
	parent context.Context
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
}

func (b *attemptBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF && b.ctx.Err() != nil && b.parent.Err() == nil {
		err = context.Cause(b.ctx)
	}
	return n, err
}

func (b *attemptBody) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// backoff returns how long to wait before retry number attempt+1. It honors
// Retry-After and reports false when the server asks for a longer wait than
// maxRetryAfter.
func (c *Client) backoff(attempt int, h http.Header) (time.Duration, bool) {
	if d, ok := parseRetryAfter(h.Get("Retry-After"), time.Now()); ok {
		return d, d <= maxRetryAfter
	}
	d := maxBackoff
	if attempt < 10 {
		d = min(c.retryDelay<<attempt, maxBackoff)
	}
	return d/2 + rand.N(d/2+1), true
}

func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(max(secs, 0)) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}
