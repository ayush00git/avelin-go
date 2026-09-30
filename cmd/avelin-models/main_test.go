package main

import "testing"

func TestFamily(t *testing.T) {
	for id, want := range map[string]string{
		"avelin-pro":          "intelligence",
		"avelin-coding-ultra": "coding",
		"avelin-agentic-fast": "agentic",
		"bge-m3":              "?",
	} {
		if got := family(id); got != want {
			t.Errorf("family(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestTokens(t *testing.T) {
	for n, want := range map[int]string{256000: "256K", 1000000: "1M", 65536: "65536", 0: "0"} {
		if got := tokens(n); got != want {
			t.Errorf("tokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestPerMillion(t *testing.T) {
	for in, want := range map[string]string{
		"0.000000285": "0.285",
		"0.00000114":  "1.14",
		"0.0000025":   "2.50",
		"0.0000125":   "12.50",
		"0.000001188": "1.188",
		"0":           "0.00",
		"abc":         "?",
	} {
		if got := perMillion(in); got != want {
			t.Errorf("perMillion(%q) = %q, want %q", in, got, want)
		}
	}
}
