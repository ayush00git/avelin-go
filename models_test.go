package avelin

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestListModels(t *testing.T) {
	srv, rec := mockServer(t)
	list, err := newTestClient(srv).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 3 || list.Data[0] != (Model{ID: "avelin-ultra", Object: "model", Created: 1704067200, OwnedBy: "avelin"}) {
		t.Fatalf("models = %+v", list.Data)
	}
	if header, _ := rec.last(); header.Get("Authorization") != "Bearer sk-avelin-test" {
		t.Errorf("Authorization = %q", header.Get("Authorization"))
	}
	if list.Meta.StatusCode != 200 || len(list.Raw) == 0 {
		t.Errorf("meta = %+v", list.Meta)
	}
}

func TestListModelsRetries5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls, status(502, "<html>bad gateway</html>"), status(200, string(fixture(t, "models_list.json")))))
	defer srv.Close()
	list, err := newTestClient(srv).ListModels(context.Background())
	if err != nil || len(list.Data) != 3 || calls.Load() != 2 {
		t.Fatalf("err = %v, calls = %d", err, calls.Load())
	}
}

func TestCreateEmbeddings(t *testing.T) {
	srv, rec := mockServer(t)
	resp, err := newTestClient(srv).CreateEmbeddings(context.Background(), EmbeddingRequest{
		Model: ModelBGEM3, Input: []string{"AVELIN is a sovereign AI platform."},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, body := rec.last()
	assertJSON(t, body, `{"model":"bge-m3","input":["AVELIN is a sovereign AI platform."]}`)
	if len(resp.Data) != 1 || resp.Data[0].Index != 0 || len(resp.Data[0].Embedding) != 3 || resp.Data[0].Embedding[1] != -0.0456 {
		t.Fatalf("data = %+v", resp.Data)
	}
	if resp.Model != "bge-m3" || resp.Usage != (Usage{PromptTokens: 9, TotalTokens: 9}) {
		t.Fatalf("response = %+v", resp)
	}
}

func TestCreateEmbeddingsRateLimited(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(sequence(&calls, status(429,
		`{"error":{"message":"Rate limit reached. Please try again later.","type":"rate_limit_error","code":429}}`,
		"Retry-After", "0", "X-RateLimit-Remaining-Requests", "0")))
	defer srv.Close()
	_, err := newTestClient(srv).CreateEmbeddings(context.Background(), EmbeddingRequest{Model: ModelBGEM3, Input: []string{"x"}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Type != "rate_limit_error" || apiErr.Header.Get("X-RateLimit-Remaining-Requests") != "0" {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != DefaultMaxRetries+1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestFetchCatalogNeedsNoKey(t *testing.T) {
	srv, rec := mockServer(t)
	t.Setenv("AVELIN_API_KEY", "")
	catalog, err := NewClient(WithBaseURL(srv.URL)).FetchCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Data) != 9 {
		t.Fatalf("got %d models", len(catalog.Data))
	}
	ids := map[string]CatalogModel{}
	for _, m := range catalog.Data {
		ids[m.ID] = m
	}
	for _, id := range []ModelID{ModelFast, ModelPro, ModelUltra, ModelCodingFast, ModelCodingPro, ModelCodingUltra,
		ModelAgenticFast, ModelAgenticPro, ModelAgenticUltra} {
		if _, ok := ids[string(id)]; !ok {
			t.Errorf("constant %s not in catalog", id)
		}
	}
	pro := ids["avelin-pro"]
	if pro.Pricing != (CatalogPricing{Prompt: "0.0000014", Completion: "0.0000044"}) || pro.ContextLength != 256000 || pro.MaxOutputLength != 65536 {
		t.Errorf("avelin-pro = %+v", pro)
	}

	// With a key configured, it is still not sent to the public endpoint.
	if _, err := newTestClient(srv).FetchCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if header, _ := rec.last(); header.Get("Authorization") != "" {
		t.Errorf("Authorization sent to public endpoint: %q", header.Get("Authorization"))
	}
}

func TestUnknownRoute(t *testing.T) {
	srv, _ := mockServer(t)
	var out map[string]any
	_, _, err := newTestClient(srv).do(context.Background(), request{method: "GET", path: "/v1/nope"}, &out)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 404 || apiErr.Message != "Not Found" {
		t.Fatalf("err = %v", err)
	}
}

// TestLegacyModelNames pins the legacy names from AVELIN's model catalog page
// (https://avelin.ai/docs/models/README, "Legacy Model Names").
func TestLegacyModelNames(t *testing.T) {
	for got, want := range map[ModelID]ModelID{
		ModelCoding:          "avelin-coding",
		ModelCodingPlus:      "avelin-coding-plus",
		ModelCodingArchitect: "avelin-coding-architect",
		ModelAgentic:         "avelin-agentic",
		ModelAgenticHigh:     "avelin-agentic-high",
	} {
		if got != want {
			t.Errorf("legacy constant = %q, want %q", got, want)
		}
	}
}
