package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestConstName(t *testing.T) {
	for id, want := range map[string]string{
		"avelin-pro":          "ModelPro",
		"avelin-coding-ultra": "ModelCodingUltra",
		"avelin-agentic-fast": "ModelAgenticFast",
		"bge-m3":              "ModelBgeM3",
		"avelin-x.y_z":        "ModelXYZ",
	} {
		if got := constName(id); got != want {
			t.Errorf("constName(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestGeneratedFileIsCurrent checks that models_gen.go matches the catalog
// snapshot in testdata. Run go generate in the repository root to refresh
// both.
func TestGeneratedFileIsCurrent(t *testing.T) {
	catalog, err := os.ReadFile("../../testdata/public_models.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := generate(catalog)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../models_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("models_gen.go is out of date with testdata/public_models.json; run go generate")
	}
}

func TestGenerateRejectsBadInput(t *testing.T) {
	for _, in := range []string{`not json`, `{"data":[]}`, `{"data":[{"id":"avelin-a-b"},{"id":"avelin-a.b"}]}`} {
		if _, err := generate([]byte(in)); err == nil {
			t.Errorf("generate(%s) succeeded", in)
		} else if strings.Contains(in, "a-b") && !strings.Contains(err.Error(), "both map to") {
			t.Errorf("generate(%s) error = %v", in, err)
		}
	}
}
