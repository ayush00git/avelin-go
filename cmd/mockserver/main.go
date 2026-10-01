// Command mockserver serves canned AVELIN API responses from testdata so the
// examples and cmd/avelin-models run without an API key or network. Run it
// from the repository root:
//
//	go run ./cmd/mockserver
//	AVELIN_BASE_URL=http://127.0.0.1:8089 AVELIN_API_KEY=sk-avelin-mock go run ./examples/chat
//
// Any non-empty bearer token is accepted.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ayush00git/avelin-go/internal/mockapi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8089", "listen address")
	dir := flag.String("testdata", "testdata", "fixture directory")
	delay := flag.Duration("delay", 150*time.Millisecond, "pause between streamed events")
	flag.Parse()

	if _, err := os.Stat(filepath.Join(*dir, "chat_completion.json")); err != nil {
		log.Fatalf("no fixtures in %q: run from the repository root or pass -testdata", *dir)
	}
	handler := mockapi.New(os.DirFS(*dir), *delay)
	log.Printf("mock AVELIN API listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		handler.ServeHTTP(w, r)
	})))
}
