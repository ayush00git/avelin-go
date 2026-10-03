package avelin

import (
	"context"
	"encoding/json"
	"net/http"
)

//go:generate go run ./internal/genmodels -out models_gen.go -snapshot testdata/public_models.json

// ModelID names a model, such as "avelin-pro". Constants for the public
// catalog are generated into models_gen.go; the legacy names are below.
type ModelID string

// Legacy model names. AVELIN's docs call them aliases of the canonical names,
// but keys created in AVELIN's dashboard are scoped by these names: such a
// key accepts ModelCoding and ModelCodingArchitect and rejects
// ModelCodingFast and ModelCodingUltra with 403. GET /v1/models lists the
// names a key can use.
const (
	ModelCoding          ModelID = "avelin-coding"           // canonical: ModelCodingFast
	ModelCodingPlus      ModelID = "avelin-coding-plus"      // canonical: ModelCodingPro
	ModelCodingArchitect ModelID = "avelin-coding-architect" // canonical: ModelCodingUltra
	ModelAgentic         ModelID = "avelin-agentic"          // canonical: ModelAgenticPro
	ModelAgenticHigh     ModelID = "avelin-agentic-high"     // canonical: ModelAgenticUltra
)

// ModelList is the response of ListModels.
type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
	// Meta is the HTTP status and headers of the response.
	Meta Meta `json:"-"`
	// Raw is the full response body, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// Model is a model available to the API key.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ListModels lists the models available to the API key.
func (c *Client) ListModels(ctx context.Context) (*ModelList, error) {
	var out ModelList
	var err error
	out.Meta, out.Raw, err = c.do(ctx, request{method: http.MethodGet, path: "/v1/models"}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Catalog is the public model catalog served at /public/models.json. AVELIN
// does not document this file; the fields mirror its observed content.
type Catalog struct {
	Object string         `json:"object"`
	Data   []CatalogModel `json:"data"`
	// Meta is the HTTP status and headers of the response.
	Meta Meta `json:"-"`
	// Raw is the full response body, for fields this package does not model.
	Raw json.RawMessage `json:"-"`
}

// CatalogModel is one model in the public catalog.
type CatalogModel struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Created          int64          `json:"created"`
	InputModalities  []string       `json:"input_modalities"`
	OutputModalities []string       `json:"output_modalities"`
	Pricing          CatalogPricing `json:"pricing"`
	// SupportedSamplingParameters lists request fields such as
	// "reasoning_effort" and "temperature".
	SupportedSamplingParameters []string `json:"supported_sampling_parameters"`
	ContextLength               int      `json:"context_length"`
	MaxOutputLength             int      `json:"max_output_length"`
}

// CatalogPricing holds prices in US dollars per token as decimal strings,
// for example "0.0000014" ($1.40 per million tokens).
type CatalogPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

// FetchCatalog fetches the public model catalog. It needs no API key and
// never sends one.
func (c *Client) FetchCatalog(ctx context.Context) (*Catalog, error) {
	var out Catalog
	var err error
	out.Meta, out.Raw, err = c.do(ctx, request{method: http.MethodGet, path: "/public/models.json", public: true}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
