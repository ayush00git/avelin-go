// Package mockapi serves canned AVELIN API responses from fixture files. It
// backs the unit tests and cmd/mockserver. It checks only what a client could
// get wrong (auth header, required fields); it does not simulate models.
package mockapi

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// New returns a handler serving fixtures from dir (the repository's testdata
// directory). streamDelay is the pause between streamed events.
func New(dir fs.FS, streamDelay time.Duration) http.Handler {
	s := &server{dir: dir, delay: streamDelay}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.chat))
	mux.HandleFunc("POST /v1/messages", s.auth(s.messages))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"detail": "Not Found"}`)
	})
	return mux
}

type server struct {
	dir   fs.FS
	delay time.Duration
}

// auth rejects requests without a bearer token, with the body the real API
// was observed to return.
func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeJSON(w, http.StatusUnauthorized, `{"error": {"message": "Access denied."}}`)
			return
		}
		next(w, r)
	}
}

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string            `json:"model"`
		Messages []json.RawMessage `json:"messages"`
		Stream   bool              `json:"stream"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		badRequest(w, "model and messages are required")
		return
	}
	if req.Stream {
		s.stream(w, "chat_stream.txt")
		return
	}
	s.file(w, "chat_completion.json")
}

func (s *server) messages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model     string            `json:"model"`
		MaxTokens int               `json:"max_tokens"`
		Messages  []json.RawMessage `json:"messages"`
		Stream    bool              `json:"stream"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Model == "" || req.MaxTokens <= 0 || len(req.Messages) == 0 {
		badRequest(w, "model, max_tokens and messages are required")
		return
	}
	if req.Stream {
		s.stream(w, "messages_stream.txt")
		return
	}
	s.file(w, "message.json")
}

func (s *server) file(w http.ResponseWriter, name string) {
	data, err := fs.ReadFile(s.dir, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, string(data))
}

// stream writes an SSE fixture one event at a time.
func (s *server) stream(w http.ResponseWriter, name string) {
	data, err := fs.ReadFile(s.dir, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, event := range strings.SplitAfter(string(data), "\n\n") {
		if strings.TrimSpace(event) == "" {
			continue
		}
		if _, err := w.Write([]byte(event)); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(s.delay)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// badRequest writes the error shape AVELIN documents.
func badRequest(w http.ResponseWriter, msg string) {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{"message": msg, "type": "invalid_request_error", "code": 400},
	})
	writeJSON(w, http.StatusBadRequest, string(body))
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(body))
}
