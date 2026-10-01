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
	mux.HandleFunc("POST /v1/chat/completions", requireAuth(s.chat))
	mux.HandleFunc("POST /v1/messages", requireAuth(s.messages))
	mux.HandleFunc("GET /v1/models", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		s.file(w, "models_list.json")
	}))
	mux.HandleFunc("POST /v1/embeddings", requireAuth(s.embeddings))
	mux.HandleFunc("GET /public/models.json", func(w http.ResponseWriter, r *http.Request) {
		s.file(w, "public_models.json")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"detail": "Not Found"}`)
	})
	return mux
}

type server struct {
	dir   fs.FS
	delay time.Duration
}

// requireAuth rejects requests without a bearer token, with the body the
// real API was observed to return.
func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			writeJSON(w, http.StatusUnauthorized, `{"error": {"message": "Access denied."}}`)
			return
		}
		next(w, r)
	}
}

// chat answers a request that offers tools with a tool call, and the
// follow-up that carries the tool result with a final answer.
func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
		Tools  []json.RawMessage `json:"tools"`
		Stream bool              `json:"stream"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		badRequest(w, "model and messages are required")
		return
	}
	switch {
	case req.Stream:
		s.stream(w, "chat_stream.txt")
	case len(req.Tools) > 0 && req.Messages[len(req.Messages)-1].Role == "tool":
		s.file(w, "chat_completion_after_tool.json")
	case len(req.Tools) > 0:
		s.file(w, "chat_completion_tool_call.json")
	default:
		s.file(w, "chat_completion.json")
	}
}

// messages answers like chat: a tool_use when tools are offered, then a final
// answer once the last message carries a tool_result.
func (s *server) messages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools  []json.RawMessage `json:"tools"`
		Stream bool              `json:"stream"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Model == "" || req.MaxTokens <= 0 || len(req.Messages) == 0 {
		badRequest(w, "model, max_tokens and messages are required")
		return
	}
	last := req.Messages[len(req.Messages)-1].Content
	switch {
	case req.Stream:
		s.stream(w, "messages_stream.txt")
	case len(req.Tools) > 0 && strings.Contains(string(last), `"tool_result"`):
		s.file(w, "message_after_tool.json")
	case len(req.Tools) > 0:
		s.file(w, "message_tool_use.json")
	default:
		s.file(w, "message.json")
	}
}

func (s *server) embeddings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string          `json:"model"`
		Input json.RawMessage `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Model == "" || len(req.Input) == 0 {
		badRequest(w, "model and input are required")
		return
	}
	s.file(w, "embeddings.json")
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
	for i, event := range strings.SplitAfter(string(data), "\n\n") {
		if strings.TrimSpace(event) == "" {
			continue
		}
		if i > 0 {
			time.Sleep(s.delay)
		}
		if _, err := w.Write([]byte(event)); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
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
