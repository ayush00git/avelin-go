package avelin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// APIError is returned when the API answers with a non-2xx status or reports
// an error inside a stream. Use errors.As to get it from a returned error.
type APIError struct {
	// StatusCode is the HTTP status. For errors reported inside a stream it
	// is the status of the stream response, usually 200.
	StatusCode int
	// Type is error.type, such as "rate_limit_error". It may be empty.
	Type string
	// Code is error.code as text. AVELIN documents numeric codes that repeat
	// the HTTP status. It may be empty.
	Code string
	// Message is error.message, or a fallback built from the body or status.
	Message string
	// RequestID comes from the X-Request-Id or Request-Id header, if present.
	RequestID string
	// Header holds the response headers, including any X-RateLimit-* headers.
	Header http.Header
	// Body is the raw response body (at most 1 MiB), or the stream event data.
	Body []byte
}

// Error formats the status, type, message and request ID.
func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "avelin: HTTP %d", e.StatusCode)
	if e.Type != "" {
		b.WriteString(" " + e.Type)
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.RequestID != "" {
		b.WriteString(" (request id " + e.RequestID + ")")
	}
	return b.String()
}

// newAPIError builds an APIError from a failed response and closes its body.
func newAPIError(resp *http.Response) *APIError {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return parseAPIError(resp.StatusCode, resp.Header, body)
}

// parseAPIError understands the documented OpenAI-style body
// {"error":{"message","type","code"}}, the Anthropic body
// {"type":"error","error":{"type","message"}}, the {"detail": ...} body the
// server returns for unknown routes, and a bare {"message": ...}.
func parseAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{StatusCode: status, Header: header, RequestID: requestID(header), Body: body}
	var parsed struct {
		Error *struct {
			Message string          `json:"message"`
			Type    string          `json:"type"`
			Code    json.RawMessage `json:"code"`
		} `json:"error"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	switch err := json.Unmarshal(body, &parsed); {
	case err == nil && parsed.Error != nil:
		e.Message = parsed.Error.Message
		e.Type = parsed.Error.Type
		e.Code = rawText(parsed.Error.Code)
	case err == nil && parsed.Detail != nil:
		e.Message = rawText(parsed.Detail)
	case err == nil:
		e.Message = parsed.Message
	default:
		e.Message = strings.TrimSpace(string(body))
		if len(e.Message) > 200 {
			e.Message = strings.ToValidUTF8(e.Message[:200], "") + "..."
		}
	}
	if e.Message == "" && status >= 400 {
		e.Message = http.StatusText(status)
	} else if e.Message == "" {
		e.Message = "error without a message"
	}
	return e
}

// hasError reports whether a JSON body or event has a top-level "error"
// field that is not null.
func hasError(data []byte) bool {
	var probe struct {
		Error json.RawMessage `json:"error"`
	}
	return json.Unmarshal(data, &probe) == nil && len(probe.Error) > 0 && string(probe.Error) != "null"
}

// rawText returns a JSON string's value, or other JSON values as written.
func rawText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
}
