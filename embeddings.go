package avelin

import (
	"context"
	"encoding/json"
	"net/http"
)

// ModelBGEM3 is the embeddings model AVELIN documents: 1024 dimensions,
// up to 8192 input tokens. It is not listed in the public catalog.
const ModelBGEM3 ModelID = "bge-m3"

// EmbeddingRequest is the body of POST /v1/embeddings.
type EmbeddingRequest struct {
	Model ModelID `json:"model"`
	// Input holds the texts to embed, one embedding per text.
	Input []string `json:"input"`
}

// EmbeddingList is the response of CreateEmbeddings.
type EmbeddingList struct {
	Object string      `json:"object"`
	Data   []Embedding `json:"data"`
	Model  string      `json:"model"`
	// Usage has PromptTokens and TotalTokens set.
	Usage Usage `json:"usage"`
	// Meta is the HTTP status and headers of the response.
	Meta Meta `json:"-"`
	// Raw is the full response body, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// Embedding is the vector for the input at Index.
type Embedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// CreateEmbeddings embeds the input texts.
func (c *Client) CreateEmbeddings(ctx context.Context, req EmbeddingRequest) (*EmbeddingList, error) {
	var out EmbeddingList
	var err error
	out.Meta, out.Raw, err = c.do(ctx, request{method: http.MethodPost, path: "/v1/embeddings", body: req}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
