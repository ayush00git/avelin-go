package avelin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func userMessage(text string) []ChatMessage {
	return []ChatMessage{{Role: "user", Content: text}}
}

func TestCreateChatCompletion(t *testing.T) {
	srv, rec := mockServer(t)
	resp, err := newTestClient(srv).CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Model: "avelin-pro",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Explain quantum computing."},
		},
		MaxTokens:   2048,
		Temperature: Ptr(0.7),
	})
	if err != nil {
		t.Fatal(err)
	}
	header, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-pro","messages":[
		{"role":"system","content":"You are a helpful assistant."},
		{"role":"user","content":"Explain quantum computing."}],
		"max_tokens":2048,"temperature":0.7}`)
	if header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", header.Get("Content-Type"))
	}

	msg := resp.Choices[0].Message
	if resp.ID != "chatcmpl-abc123" || msg.Role != "assistant" || resp.Choices[0].FinishReason != "stop" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if msg.Content != "Quantum computing leverages quantum mechanics..." ||
		msg.ReasoningContent != "The user is asking about quantum computing..." {
		t.Errorf("message = %+v", msg)
	}
	if resp.Usage != (Usage{PromptTokens: 24, CompletionTokens: 180, TotalTokens: 204}) {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.Meta.StatusCode != 200 || resp.Meta.Header.Get("Content-Type") != "application/json" {
		t.Errorf("meta = %+v", resp.Meta)
	}
	if string(resp.Raw) != string(fixture(t, "chat_completion.json")) {
		t.Errorf("Raw does not hold the response body")
	}
}

func TestCreateChatCompletionToolCall(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(status(200, string(fixture(t, "chat_completion_tool_call.json")))))
	defer srv.Close()
	params := map[string]any{
		"type":       "object",
		"properties": map[string]any{"city": map[string]any{"type": "string", "description": "City name"}},
		"required":   []string{"city"},
	}
	resp, err := newTestClient(srv).CreateChatCompletion(context.Background(), ChatCompletionRequest{
		Model:    "avelin-agentic-pro",
		Messages: userMessage("What's the weather in Abu Dhabi?"),
		Tools: []Tool{{Type: "function", Function: FunctionDefinition{
			Name: "get_weather", Description: "Get current weather for a city", Parameters: params,
		}}},
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-agentic-pro",
		"messages":[{"role":"user","content":"What's the weather in Abu Dhabi?"}],
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Get current weather for a city",
			"parameters":{"type":"object","properties":{"city":{"type":"string","description":"City name"}},"required":["city"]}}}],
		"tool_choice":"auto"}`)

	choice := resp.Choices[0]
	if choice.FinishReason != "tool_calls" || choice.Message.Content != "" || len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("choice = %+v", choice)
	}
	call := choice.Message.ToolCalls[0]
	if call.ID != "call_abc123" || call.Function.Name != "get_weather" || call.Function.Arguments != `{"city": "Abu Dhabi"}` {
		t.Fatalf("tool call = %+v", call)
	}
}

func TestCreateChatCompletionAPIError(t *testing.T) {
	srv := httptest.NewServer(status(400, `{"error":{"message":"This model's maximum context length is 256000 tokens, but you requested 300000 tokens","type":"invalid_request_error","code":400}}`))
	defer srv.Close()
	_, err := newTestClient(srv).CreateChatCompletion(context.Background(), ChatCompletionRequest{Model: "avelin-pro", Messages: userMessage("hi")})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || apiErr.Type != "invalid_request_error" || apiErr.Code != "400" {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateChatCompletionMockRejectsMissingKey(t *testing.T) {
	srv, _ := mockServer(t)
	c := newTestClient(srv, WithAPIKey(" "))
	_, err := c.CreateChatCompletion(context.Background(), ChatCompletionRequest{Model: "avelin-pro", Messages: userMessage("hi")})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 || apiErr.Message != "Access denied." {
		t.Fatalf("err = %v", err)
	}
}

// collectChat drains a chat stream into its role, reasoning and content.
func collectChat(s *Stream[ChatCompletionChunk]) (chunks int, role, reasoning, content, finish string) {
	for s.Next() {
		chunks++
		for _, c := range s.Current().Choices {
			role += c.Delta.Role
			reasoning += c.Delta.ReasoningContent
			content += c.Delta.Content
			finish += c.FinishReason
		}
	}
	return
}

func TestCreateChatCompletionStream(t *testing.T) {
	srv, rec := mockServer(t)
	stream, err := newTestClient(srv).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{
		Model: "avelin-pro", Messages: userMessage("Write a haiku about sovereignty."),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	chunks, role, reasoning, content, finish := collectChat(stream)
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if chunks != 4 || role != "assistant" || reasoning != "The user" || content != "Vendor-neutral" || finish != "stop" {
		t.Fatalf("got %d chunks role=%q reasoning=%q content=%q finish=%q", chunks, role, reasoning, content, finish)
	}
	if stream.Next() {
		t.Fatal("Next returned true after [DONE]")
	}
	if !strings.Contains(string(stream.Current().Raw), `"finish_reason":"stop"`) {
		t.Errorf("Raw = %s", stream.Current().Raw)
	}
	if stream.Meta().Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("meta = %+v", stream.Meta())
	}
	header, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-pro","messages":[{"role":"user","content":"Write a haiku about sovereignty."}],"stream":true}`)
	if header.Get("Accept") != "text/event-stream" {
		t.Errorf("Accept = %q", header.Get("Accept"))
	}
}

func sse(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, body)
	}
}

func TestChatStreamEndings(t *testing.T) {
	const chunk = `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hi"}}]}`
	tests := []struct {
		name       string
		body       string
		wantChunks int
		check      func(error) bool
	}{
		{"multi-line data", "data: {\"id\":\"c\",\ndata: \"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: [DONE]\n\n", 1,
			func(err error) bool { return err == nil }},
		{"explicit null error", "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}],\"error\":null}\n\ndata: [DONE]\n\n", 1,
			func(err error) bool { return err == nil }},
		{"empty data keep-alive", "data:\n\ndata: " + chunk + "\n\ndata: [DONE]\n\n", 1,
			func(err error) bool { return err == nil }},
		{"[DONE] without closing blank line", "data: " + chunk + "\n\ndata: [DONE]\n", 1,
			func(err error) bool { return err == nil }},
		{"empty error object", "data: {\"error\":{}}\n\n", 0,
			func(err error) bool {
				var apiErr *APIError
				return errors.As(err, &apiErr) && apiErr.Message == "error without a message"
			}},
		{"no [DONE]", "data: " + chunk + "\n\n", 1,
			func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }},
		{"broken mid-event", "data: " + chunk + "\n\ndata: {\"id\":\"c\",\"cho", 1,
			func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }},
		{"error payload", "data: " + chunk + "\n\ndata: {\"error\":{\"message\":\"overloaded\",\"type\":\"server_error\",\"code\":500}}\n\n", 1,
			func(err error) bool {
				var apiErr *APIError
				return errors.As(err, &apiErr) && apiErr.Message == "overloaded" && apiErr.StatusCode == 200
			}},
		{"invalid JSON", "data: {nope}\n\n", 0,
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "decode stream chunk") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(sequence(&calls, sse(tt.body)))
			defer srv.Close()
			stream, err := newTestClient(srv).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			chunks, _, _, _, _ := collectChat(stream)
			if chunks != tt.wantChunks || !tt.check(stream.Err()) {
				t.Fatalf("chunks = %d, err = %v", chunks, stream.Err())
			}
			if calls.Load() != 1 {
				t.Fatalf("stream was retried: %d calls", calls.Load())
			}
		})
	}
}

func TestChatStreamRetriesBeforeStart(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls,
		status(429, "", "Retry-After", "0"),
		status(503, ""),
		sse(string(fixture(t, "chat_stream.txt")))))
	defer srv.Close()
	stream, err := newTestClient(srv).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if chunks, _, _, content, _ := collectChat(stream); chunks != 4 || content != "Vendor-neutral" || stream.Err() != nil {
		t.Fatalf("chunks = %d, content = %q, err = %v", chunks, content, stream.Err())
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

// stallAfter sends one SSE event and then waits for the client to go away.
func stallAfter(event string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, event+"\n\n")
		w.(http.Flusher).Flush()
		waitForClient(w, r)
	}
}

const chatChunkEvent = `data: {"choices":[{"delta":{"content":"Hi"}}]}`

func TestChatStreamContextCancel(t *testing.T) {
	srv := httptest.NewServer(stallAfter(chatChunkEvent))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := newTestClient(srv).CreateChatCompletionStream(ctx, ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if !stream.Next() {
		t.Fatalf("first Next failed: %v", stream.Err())
	}
	time.AfterFunc(50*time.Millisecond, cancel)
	if stream.Next() || !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", stream.Err())
	}
}

func TestChatStreamCloseEarly(t *testing.T) {
	srv := httptest.NewServer(stallAfter(chatChunkEvent))
	defer srv.Close()
	stream, err := newTestClient(srv).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Next() {
		t.Fatal(stream.Err())
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if stream.Next() || stream.Err() != nil || stream.Close() != nil {
		t.Fatalf("after Close: err = %v", stream.Err())
	}
}

func TestChatStreamTimeout(t *testing.T) {
	slowStream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"late\"}}]}\n\ndata: [DONE]\n\n")
	}
	srv := httptest.NewServer(http.HandlerFunc(slowStream))
	defer srv.Close()
	c := newTestClient(srv, WithTimeout(50*time.Millisecond))
	stream, err := c.CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, _, _, content, _ := collectChat(stream); content != "late" || stream.Err() != nil {
		t.Fatalf("timeout cut the stream: content = %q, err = %v", content, stream.Err())
	}

	hang := httptest.NewServer(http.HandlerFunc(waitForClient))
	defer hang.Close()
	_, err = newTestClient(hang, WithTimeout(50*time.Millisecond)).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}

	// An error response whose body stalls is still bounded by the timeout.
	stalledError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.(http.Flusher).Flush()
		waitForClient(w, r)
	}))
	defer stalledError.Close()
	start := time.Now()
	_, err = newTestClient(stalledError, WithTimeout(50*time.Millisecond), WithMaxRetries(0)).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err = %v after %s, want a prompt error", err, time.Since(start))
	}
}

func TestToolResultMessageJSON(t *testing.T) {
	data, err := json.Marshal(ChatMessage{Role: "tool", ToolCallID: "call_abc123", Content: `{"temp_c": 31}`})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, data, `{"role":"tool","content":"{\"temp_c\": 31}","tool_call_id":"call_abc123"}`)
}

// TestChatRequestJSONFields sets every request field, so a wrong JSON tag
// fails here.
func TestChatRequestJSONFields(t *testing.T) {
	index := 0
	data, err := json.Marshal(ChatCompletionRequest{
		Model: ModelPro,
		Messages: []ChatMessage{
			{Role: "assistant", ToolCalls: []ToolCall{{Index: &index, ID: "call_1", Type: "function",
				Function: FunctionCall{Name: "get_weather", Arguments: "{}"}}}},
			{Role: "tool", ToolCallID: "call_1", Content: "31C", ReasoningContent: "r"},
		},
		MaxTokens: 10, Temperature: Ptr(0.0), TopP: Ptr(0.9), N: 2, Stop: []string{"END"},
		PresencePenalty: Ptr(0.5), FrequencyPenalty: Ptr(-0.5),
		Tools:      []Tool{{Type: "function", Function: FunctionDefinition{Name: "get_weather", Description: "d", Parameters: map[string]any{"type": "object"}}}},
		ToolChoice: "none", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, data, `{"model":"avelin-pro","messages":[
		{"role":"assistant","content":"","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{}"}}]},
		{"role":"tool","content":"31C","reasoning_content":"r","tool_call_id":"call_1"}],
		"max_tokens":10,"temperature":0,"top_p":0.9,"n":2,"stop":["END"],"presence_penalty":0.5,"frequency_penalty":-0.5,
		"tools":[{"type":"function","function":{"name":"get_weather","description":"d","parameters":{"type":"object"}}}],
		"tool_choice":"none","reasoning_effort":"high"}`)
}

func TestChatStreamToolCallDeltas(t *testing.T) {
	body := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_abc123","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\": "}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Abu Dhabi\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	srv := httptest.NewServer(sse(body))
	defer srv.Close()
	stream, err := newTestClient(srv).CreateChatCompletionStream(context.Background(), ChatCompletionRequest{Model: "m", Messages: userMessage("hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var id, name, args, finish string
	for stream.Next() {
		for _, choice := range stream.Current().Choices {
			finish += choice.FinishReason
			for _, call := range choice.Delta.ToolCalls {
				if call.Index == nil || *call.Index != 0 {
					t.Fatalf("tool call index = %v", call.Index)
				}
				id += call.ID
				name += call.Function.Name
				args += call.Function.Arguments
			}
		}
	}
	if stream.Err() != nil || id != "call_abc123" || name != "get_weather" || args != `{"city": "Abu Dhabi"}` || finish != "tool_calls" {
		t.Fatalf("id=%q name=%q args=%q finish=%q err=%v", id, name, args, finish, stream.Err())
	}
}
