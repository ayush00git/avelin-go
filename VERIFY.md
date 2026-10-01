# VERIFY

Run every command from the repository root. Steps 1 to 7 and 10 to 12 need no network.

1. Build.
   ```sh
   go build ./...
   ```
   Expect: no output.

2. Vet, including the tagged test files.
   ```sh
   go vet ./... && go vet -tags integration,live ./...
   ```
   Expect: no output.

3. Formatting.
   ```sh
   gofmt -l .
   ```
   Expect: no output.

4. No third-party dependencies.
   ```sh
   go list -m all; grep -c require go.mod
   ```
   Expect: `github.com/ayush00git/avelin-go` and then `0`.

5. Unit tests.
   ```sh
   go test ./...
   ```
   Expect: `ok` for `github.com/ayush00git/avelin-go`, `cmd/avelin-models` and `internal/genmodels`;
   the other packages print `[no test files]`. Takes about 2 seconds (one test waits on a 1s `Retry-After`).

6. Race detector.
   ```sh
   CGO_ENABLED=1 go test -race ./...
   ```
   Expect: the same `ok` lines. `-race` needs cgo and a C compiler. If you see
   `go: -race requires cgo` or a missing `gcc`, install one (`sudo apt install gcc`) and retry.

7. staticcheck (optional, downloads the tool, does not touch `go.mod`).
   ```sh
   go run honnef.co/go/tools/cmd/staticcheck@latest ./...
   ```
   Expect: no output.

8. Live public catalog (no key, one GET to `https://api.avelin.ai/public/models.json`).
   ```sh
   go run ./cmd/avelin-models
   ```
   Expect (as of 2026-09-30):
   ```
   ID                    FAMILY        CONTEXT  INPUT $/1M  OUTPUT $/1M
   avelin-agentic-fast   agentic       256K     0.285       1.14
   avelin-agentic-pro    agentic       256K     1.26        3.96
   avelin-agentic-ultra  agentic       256K     2.38        11.88
   avelin-coding-fast    coding        1M       0.30        1.20
   avelin-coding-pro     coding        1M       1.40        4.40
   avelin-coding-ultra   coding        1M       2.50        12.50
   avelin-fast           intelligence  256K     0.30        1.20
   avelin-pro            intelligence  256K     1.40        4.40
   avelin-ultra          intelligence  256K     2.50        12.50

   9 models. Family is derived from the ID; the catalog has no family field.
   ```

9. Live test (same endpoint, checks the generated constants still exist).
   ```sh
   go test -tags live -run Live -v .
   ```
   Expect: `--- PASS: TestLiveCatalog` and one log line per model.

10. Examples against the local mock. In terminal 1:
    ```sh
    go run ./cmd/mockserver
    ```
    Expect: `mock AVELIN API listening on http://127.0.0.1:8089`. In terminal 2:
    ```sh
    export AVELIN_BASE_URL=http://127.0.0.1:8089 AVELIN_API_KEY=sk-avelin-mock
    go run ./examples/chat       # Quantum computing leverages quantum mechanics...
                                 # (avelin-pro, 204 tokens)
    go run ./examples/stream     # Vendor-neutral
    go run ./examples/messages   # Here is a debounce helper...
                                 # (avelin-coding-fast, stop: end_turn, 228 tokens)
    go run ./cmd/avelin-models   # the table from step 8, served from testdata
    unset AVELIN_BASE_URL AVELIN_API_KEY
    ```
    Terminal 1 logs each request, for example `POST /v1/chat/completions`. Stop it with Ctrl+C.
    The replies are the example responses from AVELIN's API reference, stored in `testdata/`.

11. Missing key fails fast without sending a request.
    ```sh
    env -u AVELIN_API_KEY go run ./examples/chat
    ```
    Expect: `avelin: no API key: set AVELIN_API_KEY or use WithAPIKey` and exit status 1.

12. Code generation is reproducible (fetches the live catalog).
    ```sh
    go generate ./... && git status --short
    ```
    Expect: no output. If AVELIN changed its catalog, `models_gen.go` and
    `testdata/public_models.json` show as modified together, and `go test ./...` still passes.

13. Integration tests, only if you have a key. They make 7 small requests.
    ```sh
    unset AVELIN_BASE_URL
    AVELIN_API_KEY=sk-avelin-... go test -tags integration -run Integration -v .
    ```
    Expect: 7 `--- PASS` lines. The logs print response headers and the real error body shape,
    which show whether the assumptions in the README's Status section hold. Without a key all 7
    print `--- SKIP`.
