package avelin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayush00git/avelin-go/internal/mockapi"
)

// newTestClient returns a client for srv with a test key and fast backoff.
func newTestClient(srv *httptest.Server, opts ...Option) *Client {
	c := NewClient(append([]Option{WithAPIKey("sk-avelin-test"), WithBaseURL(srv.URL)}, opts...)...)
	c.retryDelay = time.Millisecond
	return c
}

// recorder captures the last request a test server received.
type recorder struct {
	mu     sync.Mutex
	header http.Header
	body   []byte
}

func (rec *recorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.header, rec.body = r.Header.Clone(), body
		rec.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

func (rec *recorder) last() (http.Header, []byte) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.header, rec.body
}

// mockServer serves testdata through internal/mockapi and records requests.
func mockServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(mockapi.New(os.DirFS("testdata"), 0)))
	t.Cleanup(srv.Close)
	return srv, rec
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// assertJSON fails unless got and want are equal JSON values.
func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("invalid JSON %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("invalid want JSON: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch\n got: %s\nwant: %s", got, want)
	}
}

// sequence serves the given handlers in order, repeating the last one, and
// counts calls.
func sequence(calls *atomic.Int32, handlers ...http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		handlers[min(n, len(handlers)-1)](w, r)
	}
}

func status(code int, body string, header ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i+1 < len(header); i += 2 {
			w.Header().Set(header[i], header[i+1])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		io.WriteString(w, body)
	}
}

var okBody = status(200, `{"ok":true}`)

// waitForClient blocks until the client disconnects. The server notices a
// disconnect only after the request body has been read, so it drains it.
func waitForClient(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	<-r.Context().Done()
}

func get(ctx context.Context, c *Client) error {
	var out map[string]any
	_, _, err := c.do(ctx, request{method: http.MethodGet, path: "/v1/models"}, &out)
	return err
}

func TestNewClientConfig(t *testing.T) {
	t.Setenv("AVELIN_API_KEY", "sk-avelin-env")
	t.Setenv("AVELIN_BASE_URL", "http://env.example/")
	c := NewClient()
	if c.apiKey != "sk-avelin-env" || c.baseURL != "http://env.example" {
		t.Fatalf("env defaults not applied: key=%q base=%q", c.apiKey, c.baseURL)
	}
	if c.maxRetries != DefaultMaxRetries || c.timeout != DefaultTimeout {
		t.Fatalf("defaults: retries=%d timeout=%s", c.maxRetries, c.timeout)
	}
	hc := &http.Client{}
	c = NewClient(WithAPIKey("k"), WithBaseURL("http://opt.example/"), WithHTTPClient(hc),
		WithMaxRetries(-1), WithTimeout(time.Second))
	if c.apiKey != "k" || c.baseURL != "http://opt.example" || c.httpClient != hc || c.maxRetries != 0 || c.timeout != time.Second {
		t.Fatalf("options not applied: %+v", c)
	}
}

func TestRequestHeaders(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(okBody))
	defer srv.Close()
	if err := get(context.Background(), newTestClient(srv)); err != nil {
		t.Fatal(err)
	}
	got, _ := rec.last()
	if got.Get("Authorization") != "Bearer sk-avelin-test" {
		t.Errorf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("User-Agent") != userAgent || got.Get("Accept") != "application/json" {
		t.Errorf("User-Agent = %q, Accept = %q", got.Get("User-Agent"), got.Get("Accept"))
	}
}

func TestMissingAPIKeySendsNothing(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls, okBody))
	defer srv.Close()
	t.Setenv("AVELIN_API_KEY", "")
	c := NewClient(WithBaseURL(srv.URL))
	if err := get(context.Background(), c); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("server was called %d times", calls.Load())
	}
}

func TestAPIErrorShapes(t *testing.T) {
	tests := []struct {
		name, body                  string
		status                      int
		wantType, wantCode, wantMsg string
	}{
		{"documented", `{"error":{"message":"Invalid API key provided","type":"authentication_error","code":401}}`, 401, "authentication_error", "401", "Invalid API key provided"},
		{"observed live", `{"error": {"message": "Access denied."}}`, 401, "", "", "Access denied."},
		{"string code", `{"error":{"message":"m","type":"t","code":"model_not_found"}}`, 404, "t", "model_not_found", "m"},
		{"anthropic", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, 400, "overloaded_error", "", "Overloaded"},
		{"detail", `{"detail": "Not Found"}`, 404, "", "", "Not Found"},
		{"bare message", `{"message":"quota exceeded"}`, 402, "", "", "quota exceeded"},
		{"html", `<html>Bad Gateway</html>`, 400, "", "", "<html>Bad Gateway</html>"},
		{"empty", ``, 403, "", "", "Forbidden"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(status(tt.status, tt.body, "X-Request-Id", "req_123"))
			defer srv.Close()
			err := get(context.Background(), newTestClient(srv))
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.StatusCode != tt.status || apiErr.Type != tt.wantType || apiErr.Code != tt.wantCode || apiErr.Message != tt.wantMsg {
				t.Fatalf("got status=%d type=%q code=%q msg=%q", apiErr.StatusCode, apiErr.Type, apiErr.Code, apiErr.Message)
			}
			if apiErr.RequestID != "req_123" || string(apiErr.Body) != tt.body {
				t.Fatalf("request id %q, body %q", apiErr.RequestID, apiErr.Body)
			}
		})
	}
}

func TestAPIErrorMessage(t *testing.T) {
	e := &APIError{StatusCode: 429, Type: "rate_limit_error", Message: "slow down", RequestID: "r1"}
	if got, want := e.Error(), "avelin: HTTP 429 rate_limit_error: slow down (request id r1)"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestRetry429HonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls,
		status(429, `{"error":{"message":"Rate limit reached.","type":"rate_limit_error","code":429}}`, "Retry-After", "1"),
		okBody))
	defer srv.Close()
	start := time.Now()
	if err := get(context.Background(), newTestClient(srv)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("retried after %s, want >= 1s", elapsed)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestRetry5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls, status(503, ""), status(502, ""), okBody))
	defer srv.Close()
	if err := get(context.Background(), newTestClient(srv)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestRetryLimits(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		opts      []Option
		wantCalls int32
		wantCode  int
	}{
		{"exhausted", status(500, `{"error":{"message":"boom"}}`), nil, DefaultMaxRetries + 1, 500},
		{"max retries option", status(500, ""), []Option{WithMaxRetries(5)}, 6, 500},
		{"disabled", status(503, ""), []Option{WithMaxRetries(0)}, 1, 503},
		{"4xx not retried", status(400, `{"error":{"message":"bad"}}`), nil, 1, 400},
		{"retry-after too long", status(429, "", "Retry-After", "120"), nil, 1, 429},
		{"retry-after overflow", status(429, "", "Retry-After", "99999999999"), nil, 1, 429},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(sequence(&calls, tt.handler))
			defer srv.Close()
			err := get(context.Background(), newTestClient(srv, tt.opts...))
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.wantCode {
				t.Fatalf("err = %v, want APIError %d", err, tt.wantCode)
			}
			if calls.Load() != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
		})
	}
}

func TestContextCancelDuringRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(waitForClient))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if err := get(ctx, newTestClient(srv)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestContextCancelDuringBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls, status(429, "", "Retry-After", "30")))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	err := get(ctx, newTestClient(srv))
	if !errors.Is(err, context.Canceled) || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v after %s, want prompt context.Canceled", err, time.Since(start))
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(waitForClient))
	defer srv.Close()
	err := get(context.Background(), newTestClient(srv, WithTimeout(50*time.Millisecond)))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "avelin: request timed out") {
		t.Fatalf("err = %v, want a context.DeadlineExceeded timeout", err)
	}
}

func TestTimeoutWhileReadingBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		io.WriteString(w, `{"ok":`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	err := get(context.Background(), newTestClient(srv, WithTimeout(50*time.Millisecond)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"", 0, false},
		{"3", 3 * time.Second, true},
		{" 0 ", 0, true},
		{"Thu, 01 Jan 2026 00:00:05 GMT", 5 * time.Second, true},
		{"Wed, 31 Dec 2025 23:59:00 GMT", 0, true},
		{"soon", 0, false},
		{"-5", 0, false},
		{"99999999999", 1 << 30 * time.Second, true},
	}
	for _, tt := range tests {
		got, ok := parseRetryAfter(tt.in, now)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseRetryAfter(%q) = %s, %v; want %s, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestBackoffBounds(t *testing.T) {
	c := NewClient()
	for attempt := 0; attempt < 70; attempt++ {
		d, ok := c.backoff(attempt, http.Header{})
		if !ok || d < 0 || d > maxBackoff {
			t.Fatalf("attempt %d: backoff %s, %v", attempt, d, ok)
		}
	}
}
