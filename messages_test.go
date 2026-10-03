package avelin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
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

func TestMessageParamRoundTrip(t *testing.T) {
	in := []MessageParam{
		{Role: "user", Content: "hi"},
		{Role: "user", Blocks: []ContentBlock{{Type: "tool_result", ToolUseID: "toolu_1", Content: "ok"}}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out []MessageParam
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", out, in)
	}
}

func TestCreateMessageAPIError(t *testing.T) {
	srv := httptest.NewServer(status(400, `{"error": {"message": "Bad request. Please try again."}}`, "X-Avelin-Request-Id", "req_1"))
	defer srv.Close()
	_, err := newTestClient(srv).CreateMessage(context.Background(), MessageRequest{
		Model: "avelin-pro", MaxTokens: 10, Messages: []MessageParam{{Role: "user", Content: "hi"}},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 || apiErr.Message != "Bad request. Please try again." || apiErr.RequestID != "req_1" {
		t.Fatalf("err = %v", err)
	}
}

// TestCreateMessageRequiresMaxTokens checks the request is refused before
// sending: the live API answers 400 for max_tokens 0 and 500 when it is
// missing.
func TestCreateMessageRequiresMaxTokens(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls, status(200, string(fixture(t, "message.json")))))
	defer srv.Close()
	c := newTestClient(srv)
	req := MessageRequest{Model: "avelin-pro", Messages: []MessageParam{{Role: "user", Content: "hi"}}}
	if _, err := c.CreateMessage(context.Background(), req); err == nil || !strings.Contains(err.Error(), "MaxTokens") {
		t.Errorf("CreateMessage err = %v", err)
	}
	req.MaxTokens = -1
	if _, err := c.CreateMessageStream(context.Background(), req); err == nil || !strings.Contains(err.Error(), "MaxTokens") {
		t.Errorf("CreateMessageStream err = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("server was called %d times", calls.Load())
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
		{"empty data keep-alive", start + "data:\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			"message_start message_stop", func(err error) bool { return err == nil }},
		{"message_stop without closing blank line", start + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n",
			"message_start message_stop", func(err error) bool { return err == nil }},
		{"error event with plain-text data", start + "event: error\ndata: Internal Server Error\n\n",
			"message_start", func(err error) bool {
				var apiErr *APIError
				return errors.As(err, &apiErr) && apiErr.Message == "Internal Server Error"
			}},
		{"OpenAI-shaped error without event line", start + "data: {\"error\":{\"message\":\"overloaded\",\"type\":\"server_error\",\"code\":500}}\n\n",
			"message_start", func(err error) bool {
				var apiErr *APIError
				return errors.As(err, &apiErr) && apiErr.Message == "overloaded" && apiErr.Code == "500"
			}},
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

// TestMessageRequestJSONFields sets every request field, so a wrong JSON tag
// fails here.
func TestMessageRequestJSONFields(t *testing.T) {
	data, err := json.Marshal(MessageRequest{
		Model: ModelCodingPro, MaxTokens: 512, System: "s",
		Messages: []MessageParam{
			{Role: "assistant", Blocks: []ContentBlock{
				{Type: "thinking", Thinking: "t", Signature: "sig"},
				{Type: "tool_use", ID: "toolu_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Dubai"}`)},
			}},
			{Role: "user", Blocks: []ContentBlock{{Type: "tool_result", ToolUseID: "toolu_1", Content: "no data", IsError: true}}},
		},
		Thinking:    &Thinking{Type: "enabled", BudgetTokens: 2048},
		Tools:       []MessageTool{{Name: "get_weather", Description: "d", InputSchema: map[string]any{"type": "object"}}},
		Temperature: Ptr(0.0), TopP: Ptr(0.9), TopK: 40, StopSequences: []string{"END"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, data, `{"model":"avelin-coding-pro","max_tokens":512,"system":"s","messages":[
		{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"sig"},
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Dubai"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"no data","is_error":true}]}],
		"thinking":{"type":"enabled","budget_tokens":2048},
		"tools":[{"name":"get_weather","description":"d","input_schema":{"type":"object"}}],
		"temperature":0,"top_p":0.9,"top_k":40,"stop_sequences":["END"]}`)
}

func TestMessageStreamToolUse(t *testing.T) {
	body := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig123"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_abc123","name":"get_weather","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\": "}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Abu Dhabi\"}"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}

event: message_stop
data: {"type":"message_stop"}

`
	srv := httptest.NewServer(sse(body))
	defer srv.Close()
	stream, err := newTestClient(srv).CreateMessageStream(context.Background(), MessageRequest{
		Model: "m", MaxTokens: 1, Messages: []MessageParam{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var signature, toolName, input, stop string
	var outputTokens int
	for stream.Next() {
		ev := stream.Current()
		if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" && ev.Index == 1 && ev.ContentBlock.ID == "toolu_abc123" {
			toolName = ev.ContentBlock.Name
		}
		if ev.Delta != nil {
			signature += ev.Delta.Signature
			input += ev.Delta.PartialJSON
			stop += ev.Delta.StopReason
		}
		if ev.Usage != nil {
			outputTokens = ev.Usage.OutputTokens
		}
	}
	if stream.Err() != nil || signature != "sig123" || toolName != "get_weather" || input != `{"city": "Abu Dhabi"}` || stop != "tool_use" || outputTokens != 20 {
		t.Fatalf("signature=%q tool=%q input=%q stop=%q tokens=%d err=%v", signature, toolName, input, stop, outputTokens, stream.Err())
	}
}

func TestMessageAccumulate(t *testing.T) {
	srv, _ := mockServer(t)
	stream, err := newTestClient(srv).CreateMessageStream(context.Background(), MessageRequest{
		Model: "avelin-coding-fast", MaxTokens: 1024, Messages: []MessageParam{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var got Message
	for stream.Next() {
		if err := got.Accumulate(stream.Current()); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	want := Message{ID: "msg_...", Type: "message", Role: "assistant", Model: "avelin-coding-fast",
		Content: []ContentBlock{
			{Type: "thinking", Thinking: "The user wants"},
			{Type: "text", Text: "Here is a debounce helper..."},
		},
		StopReason: "end_turn", Usage: MessageUsage{InputTokens: 18, OutputTokens: 210}}
	if !reflect.DeepEqual(got, want) || got.Text() != "Here is a debounce helper..." {
		t.Fatalf("accumulated:\n got %+v\nwant %+v", got, want)
	}
}

// accumulateEvents feeds JSON events to a new Message.
func accumulateEvents(t *testing.T, events ...string) (Message, error) {
	t.Helper()
	var m Message
	for _, data := range events {
		var ev MessageStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatal(err)
		}
		if err := m.Accumulate(ev); err != nil {
			return m, err
		}
	}
	return m, nil
}

func TestMessageAccumulateToolUse(t *testing.T) {
	msg, err := accumulateEvents(t,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Need the weather."}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\": "}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"Abu Dhabi\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"get_time","input":{}}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`,
		`{"type":"message_stop"}`)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != "tool_use" || msg.Usage.OutputTokens != 20 || len(msg.Content) != 3 {
		t.Fatalf("message = %+v", msg)
	}
	thinking, weather, clock := msg.Content[0], msg.Content[1], msg.Content[2]
	if thinking.Thinking != "Need the weather." || thinking.Signature != "sig123" {
		t.Errorf("thinking block = %+v", thinking)
	}
	if weather.ID != "toolu_1" || weather.Name != "get_weather" || string(weather.Input) != `{"city": "Abu Dhabi"}` {
		t.Errorf("tool_use block = %+v (input %s)", weather, weather.Input)
	}
	if string(clock.Input) != "{}" {
		t.Errorf("tool_use without arguments has input %s, want {}", clock.Input)
	}
	if _, err := json.Marshal(MessageParam{Role: "assistant", Blocks: msg.Content}); err != nil {
		t.Errorf("accumulated content cannot be sent back: %v", err)
	}
}

func TestMessageAccumulateErrors(t *testing.T) {
	for name, event := range map[string]string{
		"delta before start":  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
		"stop before start":   `{"type":"content_block_stop","index":0}`,
		"start skips ahead":   `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		"start without block": `{"type":"content_block_start","index":0}`,
	} {
		if _, err := accumulateEvents(t, event); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestMessageToolRoundTripWithMock runs the flow from examples/messages-tools.
func TestMessageToolRoundTripWithMock(t *testing.T) {
	srv, rec := mockServer(t)
	c := newTestClient(srv)
	req := MessageRequest{Model: ModelAgenticPro, MaxTokens: 1024,
		Messages: []MessageParam{{Role: "user", Content: "What's the weather in Abu Dhabi?"}},
		Tools:    []MessageTool{{Name: "get_weather", InputSchema: map[string]any{"type": "object"}}}}
	first, err := c.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.StopReason != "tool_use" {
		t.Fatalf("first reply = %+v", first)
	}
	block := first.Content[0]
	req.Messages = append(req.Messages,
		MessageParam{Role: "assistant", Blocks: first.Content},
		MessageParam{Role: "user", Blocks: []ContentBlock{{Type: "tool_result", ToolUseID: block.ID, Content: `{"temp_c": 31}`}}})
	second, err := c.CreateMessage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.Text() != "It's 31°C and sunny in Abu Dhabi." || second.StopReason != "end_turn" {
		t.Fatalf("final reply = %+v", second)
	}
	_, body := rec.last()
	assertJSON(t, body, `{"model":"avelin-agentic-pro","max_tokens":1024,"messages":[
		{"role":"user","content":"What's the weather in Abu Dhabi?"},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_abc123","name":"get_weather","input":{"city":"Abu Dhabi"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_abc123","content":"{\"temp_c\": 31}"}]}],
		"tools":[{"name":"get_weather","input_schema":{"type":"object"}}]}`)
}

// TestLiveMessageResponses decodes responses captured from the live API on
// 2026-10-03: an empty thinking block, no total_tokens, a stream that starts
// with ping and mixes "data:" and "data: ".
func TestLiveMessageResponses(t *testing.T) {
	srv := httptest.NewServer(status(200, string(fixture(t, "live_message.json"))))
	defer srv.Close()
	msg, err := newTestClient(srv).CreateMessage(context.Background(), MessageRequest{Model: ModelPro, MaxTokens: 300, Messages: []MessageParam{{Role: "user", Content: "Is 91 prime?"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msg.Content[0], ContentBlock{Type: "thinking"}) || msg.Text() != "No, 91 is not prime because it equals 7 × 13." {
		t.Errorf("content = %+v", msg.Content)
	}
	if msg.Usage != (MessageUsage{InputTokens: 137, OutputTokens: 16}) || msg.StopReason != "end_turn" {
		t.Errorf("usage = %+v, stop = %q", msg.Usage, msg.StopReason)
	}

	streamSrv := httptest.NewServer(sse(string(fixture(t, "live_messages_stream.txt"))))
	defer streamSrv.Close()
	stream, err := newTestClient(streamSrv).CreateMessageStream(context.Background(), MessageRequest{Model: ModelPro, MaxTokens: 300, Messages: []MessageParam{{Role: "user", Content: "Is 91 prime?"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var streamed Message
	var first string
	for stream.Next() {
		if first == "" {
			first = stream.Current().Type
		}
		if err := streamed.Accumulate(stream.Current()); err != nil {
			t.Fatal(err)
		}
	}
	if stream.Err() != nil || first != "ping" || streamed.Text() != "No, 91 is not prime because it equals 7 times 13." || streamed.StopReason != "end_turn" {
		t.Fatalf("err=%v first=%q text=%q stop=%q", stream.Err(), first, streamed.Text(), streamed.StopReason)
	}
	if streamed.Usage != (MessageUsage{InputTokens: 137, OutputTokens: 16}) || !strings.HasPrefix(streamed.ID, "msg_") {
		t.Fatalf("usage = %+v, id = %q", streamed.Usage, streamed.ID)
	}
}

func TestMessageAccumulateCacheUsage(t *testing.T) {
	msg, err := accumulateEvents(t,
		`{"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":1}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":7,"cache_read_input_tokens":3}}`)
	want := MessageUsage{InputTokens: 10, OutputTokens: 5, CacheCreationInputTokens: 7, CacheReadInputTokens: 3}
	if err != nil || msg.Usage != want {
		t.Fatalf("usage = %+v, err = %v", msg.Usage, err)
	}
}

func TestMessageToolChoiceJSON(t *testing.T) {
	for _, tt := range []struct {
		choice *MessageToolChoice
		want   string
	}{
		{nil, ``},
		{&MessageToolChoice{Type: "auto"}, `,"tool_choice":{"type":"auto"}`},
		{&MessageToolChoice{Type: "any"}, `,"tool_choice":{"type":"any"}`},
		{&MessageToolChoice{Type: "tool", Name: "get_weather"}, `,"tool_choice":{"type":"tool","name":"get_weather"}`},
		{&MessageToolChoice{Type: "none"}, `,"tool_choice":{"type":"none"}`},
	} {
		data, err := json.Marshal(MessageRequest{Model: ModelFast, MaxTokens: 10, Messages: []MessageParam{{Role: "user", Content: "hi"}}, ToolChoice: tt.choice})
		if err != nil {
			t.Fatal(err)
		}
		assertJSON(t, data, `{"model":"avelin-fast","max_tokens":10,"messages":[{"role":"user","content":"hi"}]`+tt.want+`}`)
	}
}
