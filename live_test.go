//go:build live

// The live test fetches the public model catalog, which needs no API key:
//
//	go test -tags live -run Live -v .
package avelin_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ayush00git/avelin-go"
)

func TestLiveCatalog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := avelin.NewClient(avelin.WithBaseURL(avelin.DefaultBaseURL), avelin.WithAPIKey(""))
	catalog, err := c.FetchCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Data) == 0 {
		t.Fatal("catalog is empty")
	}
	ids := map[string]bool{}
	for _, m := range catalog.Data {
		ids[m.ID] = true
		_, okIn := new(big.Rat).SetString(m.Pricing.Prompt)
		_, okOut := new(big.Rat).SetString(m.Pricing.Completion)
		if m.ID == "" || m.ContextLength <= 0 || !okIn || !okOut {
			t.Errorf("malformed entry: %+v", m)
		}
		t.Logf("%-22s ctx=%-8d in=%s out=%s params=%v", m.ID, m.ContextLength, m.Pricing.Prompt, m.Pricing.Completion, m.SupportedSamplingParameters)
	}
	for _, id := range []avelin.ModelID{avelin.ModelFast, avelin.ModelPro, avelin.ModelUltra,
		avelin.ModelCodingFast, avelin.ModelCodingPro, avelin.ModelCodingUltra,
		avelin.ModelAgenticFast, avelin.ModelAgenticPro, avelin.ModelAgenticUltra} {
		if !ids[string(id)] {
			t.Errorf("%s is no longer in the catalog; run go generate", id)
		}
	}
	if len(ids) != 9 {
		t.Logf("catalog now has %d models; run go generate to refresh the constants", len(ids))
	}
}
