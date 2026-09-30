package avelin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	messagesPath     = "/v1/messages"
	anthropicVersion = "2023-06-01"
)

// MessageRequest is the body of POST /v1/messages (Anthropic-compatible).
type MessageRequest struct {
	Model ModelID `json:"model"`
	// MaxTokens is required by the API.
	MaxTokens int            `json:"max_tokens"`
	Messages  []MessageParam `json:"messages"`
	System    string         `json:"system,omitempty"`
	// Thinking controls extended thinking, which is on by default on models
	// that support it. Set it to &Thinking{Type: "disabled"} to turn it off.
	Thinking *Thinking     `json:"thinking,omitempty"`
	Tools    []MessageTool `json:"tools,omitempty"`
	// Temperature and TopP are optional. Use Ptr to set them.
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	TopK          int      `json:"top_k,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

// Thinking configures extended thinking.
type Thinking struct {
	// Type is "disabled", the only value AVELIN documents, or "enabled".
	Type string `json:"type"`
	// BudgetTokens is the Anthropic thinking budget for Type "enabled".
	// AVELIN's API reference does not document it.
	BudgetTokens int `json:"budget_tokens,omitempty"`
}

// MessageParam is an input message. Set Content for plain text, or Blocks
// for content blocks such as tool results. Blocks wins if both are set.
type MessageParam struct {
	// Role is "user" or "assistant".
	Role    string
	Content string
	Blocks  []ContentBlock
}

// MarshalJSON encodes content as a string, or as an array when Blocks is set.
func (m MessageParam) MarshalJSON() ([]byte, error) {
	if m.Blocks != nil {
		return json.Marshal(struct {
			Role    string         `json:"role"`
			Content []ContentBlock `json:"content"`
		}{m.Role, m.Blocks})
	}
	return json.Marshal(struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{m.Role, m.Content})
}

// ContentBlock is one block of message content. Type selects the fields
// that apply:
//
//   - "text": Text
//   - "thinking": Thinking, Signature
//   - "tool_use": ID, Name, Input
//   - "tool_result" (input only): ToolUseID, Content, IsError
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	// Content is a tool result: a string, or any value that marshals to an
	// array of content blocks.
	Content any  `json:"content,omitempty"`
	IsError bool `json:"is_error,omitempty"`
}

// MessageTool is a tool definition. InputSchema is a JSON Schema object, as
// any value that marshals to one.
type MessageTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

// Message is the response of CreateMessage.
type Message struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`
	// Content holds the blocks in order. With thinking on, a "thinking"
	// block comes before the "text" block, so use Text for the answer.
	Content []ContentBlock `json:"content"`
	// StopReason is, for example, "end_turn" or "tool_use".
	StopReason string       `json:"stop_reason"`
	Usage      MessageUsage `json:"usage"`
	// Meta is the HTTP status and headers of the response.
	Meta Meta `json:"-"`
	// Raw is the full response body, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// Text returns the concatenated text of the message's "text" blocks.
func (m *Message) Text() string {
	var b strings.Builder
	for _, block := range m.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// MessageUsage reports token counts. TotalTokens is an AVELIN addition to
// the Anthropic format.
type MessageUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// MessageStreamEvent is one event of a messages stream. Type selects the
// fields that are set:
//
//   - "message_start": Message
//   - "content_block_start": Index, ContentBlock
//   - "content_block_delta": Index, Delta
//   - "content_block_stop": Index
//   - "message_delta": Delta.StopReason, Usage
//   - "message_stop", "ping": none
//
// Other event types are passed through with Type and Raw set. An "error"
// event ends the stream with an *APIError.
type MessageStreamEvent struct {
	Type         string              `json:"type"`
	Message      *Message            `json:"message,omitempty"`
	Index        int                 `json:"index"`
	ContentBlock *ContentBlock       `json:"content_block,omitempty"`
	Delta        *MessageStreamDelta `json:"delta,omitempty"`
	Usage        *MessageUsage       `json:"usage,omitempty"`
	// Raw is the event's JSON data, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// MessageStreamDelta is the delta of a content_block_delta event (Type
// "text_delta", "thinking_delta", "input_json_delta" or "signature_delta")
// or of a message_delta event (StopReason).
type MessageStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
	Signature   string `json:"signature"`
	StopReason  string `json:"stop_reason"`
}

// CreateMessage creates a message.
func (c *Client) CreateMessage(ctx context.Context, req MessageRequest) (*Message, error) {
	var out Message
	var err error
	out.Meta, out.Raw, err = c.do(ctx, messagesRequest(req, false), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateMessageStream creates a message streamed as server-sent events. The
// caller must Close the stream.
func (c *Client) CreateMessageStream(ctx context.Context, req MessageRequest) (*Stream[MessageStreamEvent], error) {
	resp, err := c.send(ctx, messagesRequest(req, true))
	if err != nil {
		return nil, err
	}
	return newStream(resp, decodeMessageEvent), nil
}

func messagesRequest(req MessageRequest, stream bool) request {
	var body any = req
	if stream {
		body = struct {
			MessageRequest
			Stream bool `json:"stream"`
		}{req, true}
	}
	return request{
		method: http.MethodPost,
		path:   messagesPath,
		body:   body,
		header: map[string]string{"anthropic-version": anthropicVersion},
		stream: stream,
	}
}

// decodeMessageEvent decodes one messages stream event. message_stop ends the
// stream.
func decodeMessageEvent(meta Meta, ev sseEvent) (*MessageStreamEvent, bool, error) {
	var out MessageStreamEvent
	if err := json.Unmarshal([]byte(ev.data), &out); err != nil {
		return nil, false, fmt.Errorf("avelin: decode stream event: %w", err)
	}
	if out.Type == "" {
		out.Type = ev.event
	}
	if out.Type == "error" || ev.event == "error" {
		return nil, false, parseAPIError(meta.StatusCode, meta.Header, []byte(ev.data))
	}
	out.Raw = json.RawMessage(ev.data)
	return &out, out.Type == "message_stop", nil
}
