//go:build integration

// Integration tests call the real API and cost a few cents at most. They run
// only with -tags integration and skip unless AVELIN_API_KEY is set:
//
//	AVELIN_API_KEY=sk-avelin-... go test -tags integration -run Integration -v ./...
package avelin_test

import (
	"context"
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
	t.Logf("content=%q finish=%s usage=%+v", resp.Choices[0].Message.Content, resp.Choices[0].FinishReason, resp.Usage)
	t.Logf("headers: %v", resp.Meta.Header)
}

func TestIntegrationChatStream(t *testing.T) {
	c, ctx := integrationClient(t)
	stream, err := c.CreateChatCompletionStream(ctx, avelin.ChatCompletionRequest{
		Model:     avelin.ModelFast,
		Messages:  []avelin.ChatMessage{{Role: "user", Content: "Count from 1 to 5."}},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	chunks, content := 0, ""
	for stream.Next() {
		chunks++
		for _, choice := range stream.Current().Choices {
			content += choice.Delta.Content
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if chunks == 0 {
		t.Fatal("no chunks")
	}
	t.Logf("%d chunks, content=%q", chunks, content)
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
	var last string
	for stream.Next() {
		last = stream.Current().Type
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if last != "message_stop" {
		t.Fatalf("last event %q, want message_stop", last)
	}
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
