package avelin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateMessage(t *testing.T) {
	srv, rec := mockServer(t)
	msg, err := newTestClient(srv).CreateMessage(context.Background(), MessageRequest{
		Model:     "avelin-coding-fast",
		MaxTokens: 1024,
		System:    "You are a senior software engineer.",
		Messages:  []MessageParam{{Role: "user", Content: "Write a Python function to debounce calls."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	header, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-coding-fast","max_tokens":1024,"system":"You are a senior software engineer.",
		"messages":[{"role":"user","content":"Write a Python function to debounce calls."}]}`)
	if header.Get("anthropic-version") != "2023-06-01" || header.Get("Authorization") != "Bearer sk-avelin-test" {
		t.Errorf("headers = %v", header)
	}
	if msg.ID != "msg_abc123" || msg.Role != "assistant" || msg.StopReason != "end_turn" || msg.Text() != "Here is a debounce helper..." {
		t.Errorf("message = %+v", msg)
	}
	if msg.Usage != (MessageUsage{InputTokens: 18, OutputTokens: 210, TotalTokens: 228}) {
		t.Errorf("usage = %+v", msg.Usage)
	}
	if msg.Meta.StatusCode != 200 || string(msg.Raw) != string(fixture(t, "message.json")) {
		t.Errorf("meta = %+v, raw = %s", msg.Meta, msg.Raw)
	}
}

func TestCreateMessageThinkingDisabled(t *testing.T) {
	srv, rec := mockServer(t)
	_, err := newTestClient(srv).CreateMessage(context.Background(), MessageRequest{
		Model: "avelin-pro", MaxTokens: 1024, Thinking: &Thinking{Type: "disabled"},
		Messages: []MessageParam{{Role: "user", Content: "Summarize this document."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-pro","max_tokens":1024,"thinking":{"type":"disabled"},
		"messages":[{"role":"user","content":"Summarize this document."}]}`)
}

func TestMessageTextSkipsThinking(t *testing.T) {
	var msg Message
	err := json.Unmarshal([]byte(`{"content":[
		{"type":"thinking","thinking":"Step-by-step thinking process..."},
		{"type":"text","text":"Final answer..."}]}`), &msg)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text() != "Final answer..." || msg.Content[0].Thinking != "Step-by-step thinking process..." {
		t.Fatalf("Text() = %q, content = %+v", msg.Text(), msg.Content)
	}
}

func TestCreateMessageToolUse(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.wrap(status(200, string(fixture(t, "message_tool_use.json")))))
	defer srv.Close()
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","description":"City name"}},"required":["city"]}`)
	msg, err := newTestClient(srv).CreateMessage(context.Background(), MessageRequest{
		Model: "avelin-agentic-pro", MaxTokens: 1024,
		Messages: []MessageParam{{Role: "user", Content: "What's the weather in Abu Dhabi?"}},
		Tools:    []MessageTool{{Name: "get_weather", Description: "Get current weather for a city", InputSchema: schema}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-agentic-pro","max_tokens":1024,
		"messages":[{"role":"user","content":"What's the weather in Abu Dhabi?"}],
		"tools":[{"name":"get_weather","description":"Get current weather for a city",
			"input_schema":{"type":"object","properties":{"city":{"type":"string","description":"City name"}},"required":["city"]}}]}`)

	block := msg.Content[0]
	if msg.StopReason != "tool_use" || block.Type != "tool_use" || block.ID != "toolu_abc123" || block.Name != "get_weather" {
		t.Fatalf("message = %+v", msg)
	}
	assertJSON(t, block.Input, `{"city":"Abu Dhabi"}`)

	followUp, err := json.Marshal([]MessageParam{
		{Role: "assistant", Blocks: msg.Content},
		{Role: "user", Blocks: []ContentBlock{{Type: "tool_result", ToolUseID: block.ID, Content: "31C and sunny"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, followUp, `[
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_abc123","name":"get_weather","input":{"city":"Abu Dhabi"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_abc123","content":"31C and sunny"}]}]`)
}

func TestCreateMessageAPIError(t *testing.T) {
	srv, _ := mockServer(t)
	_, err := newTestClient(srv).CreateMessage(context.Background(), MessageRequest{
		Model: "avelin-pro", Messages: []MessageParam{{Role: "user", Content: "hi"}},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || apiErr.Type != "invalid_request_error" {
		t.Fatalf("err = %v, want 400 for missing max_tokens", err)
	}
}

// collectMessage drains a messages stream.
func collectMessage(s *Stream[MessageStreamEvent]) (events []MessageStreamEvent, types, thinking, text, stop string) {
	var names []string
	for s.Next() {
		ev := s.Current()
		events = append(events, ev)
		names = append(names, ev.Type)
		if ev.Delta != nil {
			thinking += ev.Delta.Thinking
			text += ev.Delta.Text
			stop += ev.Delta.StopReason
		}
	}
	return events, strings.Join(names, " "), thinking, text, stop
}

func TestCreateMessageStream(t *testing.T) {
	srv, rec := mockServer(t)
	stream, err := newTestClient(srv).CreateMessageStream(context.Background(), MessageRequest{
		Model: "avelin-coding-fast", MaxTokens: 1024,
		Messages: []MessageParam{{Role: "user", Content: "Write a Python function to debounce calls."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	events, types, thinking, text, stop := collectMessage(stream)
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	want := "message_start content_block_start content_block_delta content_block_stop content_block_start content_block_delta content_block_stop message_delta message_stop"
	if types != want {
		t.Fatalf("event types = %v", types)
	}
	first, last := events[0], events[len(events)-1]
	if thinking != "The user wants" || text != "Here is a debounce helper..." || stop != "end_turn" {
		t.Fatalf("thinking=%q text=%q stop=%q", thinking, text, stop)
	}
	if first.Message == nil || first.Message.ID != "msg_..." || first.Message.Usage.InputTokens != 18 {
		t.Fatalf("message_start = %+v", first.Message)
	}
	if last.Type != "message_stop" || string(last.Raw) != `{"type":"message_stop"}` || stream.Next() {
		t.Fatalf("last event = %+v", last)
	}
	header, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-coding-fast","max_tokens":1024,"stream":true,
		"messages":[{"role":"user","content":"Write a Python function to debounce calls."}]}`)
	if header.Get("anthropic-version") != "2023-06-01" || header.Get("Accept") != "text/event-stream" {
		t.Errorf("headers = %v", header)
	}
}

func TestMessageStreamEndings(t *testing.T) {
	const start = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\"}}\n\n"
	tests := []struct {
		name      string
		body      string
		wantTypes string
		check     func(error) bool
	}{
		{"ping and unknown events pass through",
			start + "event: ping\ndata: {\"type\": \"ping\"}\n\nevent: custom\ndata: {\"type\":\"custom\",\"x\":1}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			"message_start ping custom message_stop", func(err error) bool { return err == nil }},
		{"type from event field and multi-line data",
			start + "event: message_stop\ndata: {\n: comment\ndata: }\n\n",
			"message_start message_stop", func(err error) bool { return err == nil }},
		{"no message_stop", start + "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"message_start content_block_start", func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }},
		{"broken mid-event", start + "event: content_block_delta\ndata: {\"type\":\"content_bl",
			"message_start", func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }},
		{"error event", start + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
			"message_start", func(err error) bool {
				var apiErr *APIError
				return errors.As(err, &apiErr) && apiErr.Type == "overloaded_error" && apiErr.Message == "Overloaded"
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(sequence(&calls, sse(tt.body)))
			defer srv.Close()
			stream, err := newTestClient(srv).CreateMessageStream(context.Background(), MessageRequest{
				Model: "m", MaxTokens: 1, Messages: []MessageParam{{Role: "user", Content: "hi"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			_, types, _, _, _ := collectMessage(stream)
			if types != tt.wantTypes || !tt.check(stream.Err()) {
				t.Fatalf("types = %v, err = %v", types, stream.Err())
			}
			if calls.Load() != 1 {
				t.Fatalf("stream was retried: %d calls", calls.Load())
			}
		})
	}
}

func TestMessageStreamRetryAndCancel(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls,
		status(529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`),
		stallAfter("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\"}}")))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := newTestClient(srv).CreateMessageStream(ctx, MessageRequest{
		Model: "m", MaxTokens: 1, Messages: []MessageParam{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if !stream.Next() || stream.Current().Message.ID != "m" {
		t.Fatalf("first event: %v", stream.Err())
	}
	time.AfterFunc(50*time.Millisecond, cancel)
	if stream.Next() || !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", stream.Err())
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}
