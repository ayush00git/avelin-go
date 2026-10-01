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
// for content blocks such as tool results. A non-nil Blocks is sent instead of
// Content.
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

// UnmarshalJSON accepts content as a string or an array of blocks, so saved
// conversations can be read back.
func (m *MessageParam) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = MessageParam{Role: raw.Role}
	switch {
	case len(raw.Content) == 0 || string(raw.Content) == "null":
		return nil
	case raw.Content[0] == '[':
		return json.Unmarshal(raw.Content, &m.Blocks)
	default:
		return json.Unmarshal(raw.Content, &m.Content)
	}
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

// Accumulate adds a stream event to m, so that after message_stop m holds
// the whole message, as CreateMessage would return it. Text, thinking and
// signatures are concatenated per block, and a tool_use block's Input is
// assembled from its input_json_delta fragments; it is valid JSON once the
// block's content_block_stop has arrived. It returns an error for a delta or
// stop event whose block has not started.
func (m *Message) Accumulate(ev MessageStreamEvent) error {
	switch ev.Type {
	case "message_start":
		if ev.Message != nil {
			*m = *ev.Message
		}
	case "content_block_start":
		if ev.ContentBlock == nil || ev.Index < 0 || ev.Index > len(m.Content) {
			return fmt.Errorf("avelin: content_block_start for block %d, but %d blocks have started", ev.Index, len(m.Content))
		}
		block := *ev.ContentBlock
		if block.Type == "tool_use" && isEmptyObject(block.Input) {
			block.Input = nil // rebuilt from the input_json_delta fragments
		}
		if ev.Index == len(m.Content) {
			m.Content = append(m.Content, block)
		} else {
			m.Content[ev.Index] = block
		}
	case "content_block_delta":
		block, err := m.startedBlock(ev)
		if err != nil || ev.Delta == nil {
			return err
		}
		block.Text += ev.Delta.Text
		block.Thinking += ev.Delta.Thinking
		block.Signature += ev.Delta.Signature
		block.Input = append(block.Input, ev.Delta.PartialJSON...)
	case "content_block_stop":
		block, err := m.startedBlock(ev)
		if err != nil {
			return err
		}
		if block.Type == "tool_use" && len(block.Input) == 0 {
			block.Input = json.RawMessage("{}")
		}
	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != "" {
			m.StopReason = ev.Delta.StopReason
		}
		if u := ev.Usage; u != nil {
			m.Usage.InputTokens = max(m.Usage.InputTokens, u.InputTokens)
			m.Usage.OutputTokens = max(m.Usage.OutputTokens, u.OutputTokens)
			m.Usage.TotalTokens = max(m.Usage.TotalTokens, u.TotalTokens)
		}
	}
	return nil
}

func (m *Message) startedBlock(ev MessageStreamEvent) (*ContentBlock, error) {
	if ev.Index < 0 || ev.Index >= len(m.Content) {
		return nil, fmt.Errorf("avelin: %s for block %d, but %d blocks have started", ev.Type, ev.Index, len(m.Content))
	}
	return &m.Content[ev.Index], nil
}

func isEmptyObject(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && obj != nil && len(obj) == 0
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
// stream. An "error" event, or a payload with an "error" object in either the
// Anthropic or the OpenAI shape, ends it with an *APIError.
func decodeMessageEvent(meta Meta, ev sseEvent) (*MessageStreamEvent, bool, error) {
	if ev.event == "error" || hasError([]byte(ev.data)) {
		return nil, false, parseAPIError(meta.StatusCode, meta.Header, []byte(ev.data))
	}
	var out MessageStreamEvent
	if err := json.Unmarshal([]byte(ev.data), &out); err != nil {
		return nil, false, fmt.Errorf("avelin: decode stream event: %w", err)
	}
	if out.Type == "" {
		out.Type = ev.event
	}
	if out.Type == "error" {
		return nil, false, parseAPIError(meta.StatusCode, meta.Header, []byte(ev.data))
	}
	out.Raw = json.RawMessage(ev.data)
	return &out, out.Type == "message_stop", nil
}
