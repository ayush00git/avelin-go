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
	// ToolChoice controls tool use: "auto", "none", "required" (call at
	// least one tool), or a ToolChoiceFunction to force one function; nil or
	// "" leaves it to the server. A bare function name, which AVELIN's
	// reference shows, is accepted but ignored.
	ToolChoice any `json:"tool_choice,omitempty"`
	// ReasoningEffort is "low", "medium" or "high" on models that reason.
	// AVELIN documents only "high", but all three are accepted. Reasoning
	// tokens count toward MaxTokens.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// StreamOptions applies to CreateChatCompletionStream only;
	// CreateChatCompletion leaves it out of the request.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// StreamOptions configures a chat completion stream.
type StreamOptions struct {
	// IncludeUsage asks for a final chunk that carries Usage. It is not in
	// AVELIN's docs but works on the live API.
	IncludeUsage bool `json:"include_usage"`
}

// ChatMessage is one message of a conversation. In a response it is the
// generated message; in a stream chunk it is the incremental delta. A
// response message can be sent back as history as it is: AVELIN accepts its
// ReasoningContent and the tool calls' Index.
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

// ToolChoiceFunction forces a call to the named function when used as
// ChatCompletionRequest.ToolChoice. AVELIN reports such a call with
// FinishReason "stop", not "tool_calls". Set ToolChoice back to nil before
// sending the tool result, or the model is forced to call the function again.
type ToolChoiceFunction struct {
	Name string
}

// MarshalJSON encodes {"type":"function","function":{"name":...}}.
func (f ToolChoiceFunction) MarshalJSON() ([]byte, error) {
	type function struct {
		Name string `json:"name"`
	}
	return json.Marshal(struct {
		Type     string   `json:"type"`
		Function function `json:"function"`
	}{"function", function(f)})
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
	// Index is the call's position in the response; in streams it ties
	// fragments of one call together. AVELIN sends it in plain responses too.
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
	// A forced tool call reports "stop", so check Message.ToolCalls rather
	// than this field.
	FinishReason string `json:"finish_reason"`
}

// Usage reports token counts. Only the first three fields are documented by
// AVELIN; the rest appear in live chat responses and are zero for
// embeddings.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// CompletionTokensDetails splits CompletionTokens into reasoning and
	// text.
	CompletionTokensDetails CompletionTokensDetails `json:"completion_tokens_details"`
	// PromptTokensDetails reports how many prompt tokens came from the
	// prompt cache.
	PromptTokensDetails      PromptTokensDetails `json:"prompt_tokens_details"`
	CacheCreationInputTokens int                 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                 `json:"cache_read_input_tokens"`
}

// CompletionTokensDetails splits completion tokens by kind.
type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
	TextTokens      int `json:"text_tokens"`
}

// PromptTokensDetails splits prompt tokens by kind.
type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
	TextTokens   int `json:"text_tokens"`
}

// ChatCompletionChunk is one event of a chat completion stream.
type ChatCompletionChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
	// Usage is set on the final chunk when StreamOptions.IncludeUsage is
	// true.
	Usage *Usage `json:"usage,omitempty"`
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

// Accumulate adds a stream chunk to c, so that after the last chunk c holds
// the whole completion. Content, reasoning and tool call arguments are
// concatenated per choice, and the merged tool calls have a nil Index. Usage
// is copied from the final chunk when StreamOptions.IncludeUsage was set. It
// returns an error if a chunk refers to a choice or tool call more than one
// past the last one started.
func (c *ChatCompletion) Accumulate(chunk ChatCompletionChunk) error {
	if chunk.ID != "" {
		c.ID = chunk.ID
	}
	c.Object = "chat.completion"
	if chunk.Created != 0 {
		c.Created = chunk.Created
	}
	if chunk.Model != "" {
		c.Model = chunk.Model
	}
	if chunk.Usage != nil {
		c.Usage = *chunk.Usage
	}
	for _, delta := range chunk.Choices {
		if delta.Index < 0 || delta.Index > len(c.Choices) {
			return fmt.Errorf("avelin: chunk for choice %d, but %d choices have started", delta.Index, len(c.Choices))
		}
		if delta.Index == len(c.Choices) {
			c.Choices = append(c.Choices, ChatChoice{Index: delta.Index})
		}
		choice := &c.Choices[delta.Index]
		if delta.Delta.Role != "" {
			choice.Message.Role = delta.Delta.Role
		}
		choice.Message.Content += delta.Delta.Content
		choice.Message.ReasoningContent += delta.Delta.ReasoningContent
		if delta.FinishReason != "" {
			choice.FinishReason = delta.FinishReason
		}
		for _, fragment := range delta.Delta.ToolCalls {
			if err := accumulateToolCall(&choice.Message.ToolCalls, fragment); err != nil {
				return err
			}
		}
	}
	return nil
}

// accumulateToolCall merges a streamed tool call fragment into calls, finding
// its call by Index or, when the server sends no index, by ID.
func accumulateToolCall(calls *[]ToolCall, fragment ToolCall) error {
	i := len(*calls) - 1 // without an index or a new ID, continue the last call
	if fragment.Index != nil {
		i = *fragment.Index
	} else if i < 0 || (fragment.ID != "" && fragment.ID != (*calls)[i].ID) {
		i = len(*calls)
	}
	if i < 0 || i > len(*calls) {
		return fmt.Errorf("avelin: tool call fragment for call %d, but %d calls have started", i, len(*calls))
	}
	if i == len(*calls) {
		*calls = append(*calls, ToolCall{})
	}
	call := &(*calls)[i]
	if fragment.ID != "" {
		call.ID = fragment.ID
	}
	if fragment.Type != "" {
		call.Type = fragment.Type
	}
	if fragment.Function.Name != "" {
		call.Function.Name = fragment.Function.Name
	}
	call.Function.Arguments += fragment.Function.Arguments
	return nil
}

// Ptr returns a pointer to v, for optional request fields such as
// Temperature.
func Ptr[T any](v T) *T {
	return &v
}

const chatPath = "/v1/chat/completions"

// CreateChatCompletion creates a chat completion.
func (c *Client) CreateChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	req.StreamOptions = nil
	if req.ToolChoice == "" {
		req.ToolChoice = nil
	}
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
	if req.ToolChoice == "" {
		req.ToolChoice = nil
	}
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
