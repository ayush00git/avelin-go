//go:build integration

// Integration tests call the real API and cost a few cents at most. They run
// only with -tags integration and skip unless AVELIN_API_KEY is set:
//
//	AVELIN_API_KEY=sk-avelin-... go test -tags integration -run Integration -v ./...
package avelin_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ayush00git/avelin-go"
)

func integrationClient(t *testing.T) (*avelin.Client, context.Context) {
	t.Helper()
	if os.Getenv("AVELIN_API_KEY") == "" {
		t.Skip("AVELIN_API_KEY not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return avelin.NewClient(), ctx
}

func TestIntegrationListModels(t *testing.T) {
	c, ctx := integrationClient(t)
	list, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) == 0 {
		t.Fatal("no models")
	}
	for _, m := range list.Data {
		t.Logf("%s (owned by %s)", m.ID, m.OwnedBy)
	}
	t.Logf("headers: %v", list.Meta.Header)
}

func TestIntegrationChat(t *testing.T) {
	c, ctx := integrationClient(t)
	resp, err := c.CreateChatCompletion(ctx, avelin.ChatCompletionRequest{
		Model:     avelin.ModelFast,
		Messages:  []avelin.ChatMessage{{Role: "user", Content: "Reply with the single word: pong"}},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID == "" || len(resp.Choices) == 0 {
		t.Fatalf("unexpected response: %s", resp.Raw)
	}
	if resp.Meta.RequestID() == "" {
		t.Errorf("no request ID; headers: %v", resp.Meta.Header)
	}
	t.Logf("content=%q finish=%s usage=%+v request id=%s", resp.Choices[0].Message.Content, resp.Choices[0].FinishReason, resp.Usage, resp.Meta.RequestID())
}

func TestIntegrationChatStream(t *testing.T) {
	c, ctx := integrationClient(t)
	stream, err := c.CreateChatCompletionStream(ctx, avelin.ChatCompletionRequest{
		Model:         avelin.ModelFast,
		Messages:      []avelin.ChatMessage{{Role: "user", Content: "Count from 1 to 5."}},
		MaxTokens:     256,
		StreamOptions: &avelin.StreamOptions{IncludeUsage: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var completion avelin.ChatCompletion
	chunks := 0
	for stream.Next() {
		chunks++
		if err := completion.Accumulate(stream.Current()); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if chunks == 0 || len(completion.Choices) == 0 || completion.Choices[0].FinishReason == "" || completion.Usage.TotalTokens == 0 {
		t.Fatalf("incomplete stream: %d chunks, accumulated %+v", chunks, completion)
	}
	t.Logf("%d chunks, content=%q finish=%s usage=%+v", chunks, completion.Choices[0].Message.Content, completion.Choices[0].FinishReason, completion.Usage)
}

func TestIntegrationMessages(t *testing.T) {
	c, ctx := integrationClient(t)
	msg, err := c.CreateMessage(ctx, avelin.MessageRequest{
		Model:     avelin.ModelFast,
		MaxTokens: 256,
		Thinking:  &avelin.Thinking{Type: "disabled"},
		Messages:  []avelin.MessageParam{{Role: "user", Content: "Reply with the single word: pong"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) == 0 {
		t.Fatalf("no content: %s", msg.Raw)
	}
	t.Logf("text=%q stop=%s usage=%+v", msg.Text(), msg.StopReason, msg.Usage)
}

func TestIntegrationMessagesStream(t *testing.T) {
	c, ctx := integrationClient(t)
	stream, err := c.CreateMessageStream(ctx, avelin.MessageRequest{
		Model:     avelin.ModelFast,
		MaxTokens: 256,
		Messages:  []avelin.MessageParam{{Role: "user", Content: "Count from 1 to 5."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var msg avelin.Message
	var last string
	for stream.Next() {
		last = stream.Current().Type
		if err := msg.Accumulate(stream.Current()); err != nil {
			t.Fatal(err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if last != "message_stop" || len(msg.Content) == 0 || msg.StopReason == "" || msg.Usage.OutputTokens == 0 {
		t.Fatalf("last event %q, accumulated %+v", last, msg)
	}
	t.Logf("text=%q stop=%s usage=%+v", msg.Text(), msg.StopReason, msg.Usage)
}

const weatherSchema = `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`

func TestIntegrationChatTools(t *testing.T) {
	c, ctx := integrationClient(t)
	resp, err := c.CreateChatCompletion(ctx, avelin.ChatCompletionRequest{
		Model:     avelin.ModelAgenticFast,
		MaxTokens: 512,
		Messages:  []avelin.ChatMessage{{Role: "user", Content: "What's the weather in Abu Dhabi? Use the get_weather tool."}},
		Tools: []avelin.Tool{{Type: "function", Function: avelin.FunctionDefinition{
			Name: "get_weather", Description: "Get current weather for a city", Parameters: json.RawMessage(weatherSchema)}}},
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	choice := resp.Choices[0]
	if len(choice.Message.ToolCalls) == 0 {
		t.Fatalf("no tool call: finish=%s content=%q", choice.FinishReason, choice.Message.Content)
	}
	call := choice.Message.ToolCalls[0]
	if call.ID == "" || call.Function.Name != "get_weather" || !json.Valid([]byte(call.Function.Arguments)) {
		t.Fatalf("tool call = %+v", call)
	}
	t.Logf("tool call %s(%s), finish=%s", call.Function.Name, call.Function.Arguments, choice.FinishReason)
}

func TestIntegrationMessagesTools(t *testing.T) {
	c, ctx := integrationClient(t)
	msg, err := c.CreateMessage(ctx, avelin.MessageRequest{
		Model:     avelin.ModelAgenticFast,
		MaxTokens: 512,
		Messages:  []avelin.MessageParam{{Role: "user", Content: "What's the weather in Abu Dhabi? Use the get_weather tool."}},
		Tools:     []avelin.MessageTool{{Name: "get_weather", Description: "Get current weather for a city", InputSchema: json.RawMessage(weatherSchema)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range msg.Content {
		if block.Type == "tool_use" {
			if block.ID == "" || block.Name != "get_weather" || !json.Valid(block.Input) {
				t.Fatalf("tool_use block = %+v", block)
			}
			t.Logf("tool_use %s(%s), stop=%s", block.Name, block.Input, msg.StopReason)
			return
		}
	}
	t.Fatalf("no tool_use block: stop=%s text=%q", msg.StopReason, msg.Text())
}

func TestIntegrationEmbeddings(t *testing.T) {
	c, ctx := integrationClient(t)
	resp, err := c.CreateEmbeddings(ctx, avelin.EmbeddingRequest{
		Model: avelin.ModelBGEM3,
		Input: []string{"AVELIN is a sovereign AI platform.", "Second text."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("got %d embeddings, want 2", len(resp.Data))
	}
	if n := len(resp.Data[0].Embedding); n != 1024 {
		t.Errorf("got %d dimensions, docs say 1024", n)
	}
}

func TestIntegrationErrorShape(t *testing.T) {
	c, ctx := integrationClient(t)
	_, err := c.CreateChatCompletion(ctx, avelin.ChatCompletionRequest{
		Model:    "avelin-model-that-does-not-exist",
		Messages: []avelin.ChatMessage{{Role: "user", Content: "hi"}},
	})
	var apiErr *avelin.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	t.Logf("status=%d type=%q code=%q message=%q request id=%q body=%s",
		apiErr.StatusCode, apiErr.Type, apiErr.Code, apiErr.Message, apiErr.RequestID, apiErr.Body)
}
