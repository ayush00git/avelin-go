package avelin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ChatCompletionRequest is the body of POST /v1/chat/completions
// (OpenAI-compatible).
type ChatCompletionRequest struct {
	Model    ModelID       `json:"model"`
	Messages []ChatMessage `json:"messages"`
	// MaxTokens caps the generated tokens. Zero leaves it to the server.
	MaxTokens int `json:"max_tokens,omitempty"`
	// Temperature is between 0 and 2. Use Ptr to set it.
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP is between 0 and 1. Use Ptr to set it.
	TopP *float64 `json:"top_p,omitempty"`
	// N is the number of choices to generate. Zero means the default, 1.
	N int `json:"n,omitempty"`
	// Stop holds up to 4 stop sequences.
	Stop             []string `json:"stop,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	Tools            []Tool   `json:"tools,omitempty"`
	// ToolChoice is "auto" or "none".
	ToolChoice string `json:"tool_choice,omitempty"`
	// ReasoningEffort is "high" to request deeper reasoning on models that
	// support it. AVELIN documents no other value.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// ChatMessage is one message of a conversation. In a response it is the
// generated message; in a stream chunk it is the incremental delta. When a
// response message is sent back as history, ReasoningContent goes with it;
// clear it if the server rejects reasoning in input.
type ChatMessage struct {
	// Role is "system", "user", "assistant" or "tool".
	Role    string `json:"role"`
	Content string `json:"content"`
	// ReasoningContent is the model's reasoning, returned separately from
	// Content by reasoning-capable models.
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID identifies the call a "tool" message answers.
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Tool is a function the model may call.
type Tool struct {
	// Type is "function".
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a function. Parameters is a JSON Schema
// object, given as any value that marshals to one, such as a map or a
// json.RawMessage.
type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
	// Index identifies the call across stream chunks. It is nil outside
	// streams; set it to nil before sending a streamed call back as history.
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is a called function's name and its JSON-encoded arguments.
// In stream chunks the arguments arrive in fragments to be concatenated.
type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// ChatCompletion is the response of CreateChatCompletion.
type ChatCompletion struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
	// Meta is the HTTP status and headers of the response.
	Meta Meta `json:"-"`
	// Raw is the full response body, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// ChatChoice is one generated message.
type ChatChoice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
	// FinishReason is "stop", "length", "tool_calls" or "content_filter".
	FinishReason string `json:"finish_reason"`
}

// Usage reports token counts.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk is one event of a chat completion stream.
type ChatCompletionChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
	// Raw is the event's JSON data, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// ChatChunkChoice is the delta for one choice. FinishReason is empty until
// the choice's last chunk.
type ChatChunkChoice struct {
	Index        int         `json:"index"`
	Delta        ChatMessage `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

// Ptr returns a pointer to v, for optional request fields such as
// Temperature.
func Ptr[T any](v T) *T {
	return &v
}

const chatPath = "/v1/chat/completions"

// CreateChatCompletion creates a chat completion.
func (c *Client) CreateChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	var out ChatCompletion
	var err error
	out.Meta, out.Raw, err = c.do(ctx, request{method: http.MethodPost, path: chatPath, body: req}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateChatCompletionStream creates a chat completion streamed as
// server-sent events. The caller must Close the stream.
func (c *Client) CreateChatCompletionStream(ctx context.Context, req ChatCompletionRequest) (*Stream[ChatCompletionChunk], error) {
	body := struct {
		ChatCompletionRequest
		Stream bool `json:"stream"`
	}{req, true}
	resp, err := c.send(ctx, request{method: http.MethodPost, path: chatPath, body: body, stream: true})
	if err != nil {
		return nil, err
	}
	return newStream(resp, decodeChatEvent), nil
}

// decodeChatEvent decodes one chat stream event. [DONE] ends the stream, and
// a payload with an "error" object ends it with an *APIError.
func decodeChatEvent(meta Meta, ev sseEvent) (*ChatCompletionChunk, bool, error) {
	if ev.data == "[DONE]" {
		return nil, true, nil
	}
	if hasError([]byte(ev.data)) {
		return nil, false, parseAPIError(meta.StatusCode, meta.Header, []byte(ev.data))
	}
	var chunk ChatCompletionChunk
	if err := json.Unmarshal([]byte(ev.data), &chunk); err != nil {
		return nil, false, fmt.Errorf("avelin: decode stream chunk: %w", err)
	}
	chunk.Raw = json.RawMessage(ev.data)
	return &chunk, false, nil
}
