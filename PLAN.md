# PLAN: avelin-go v0.1 (as shipped)

Unofficial Go client for the AVELIN API (https://api.avelin.ai). Standard library only, Go 1.22+.
All phases below are done; this file records what shipped and why.

## Scope
- Shipped: `POST /v1/chat/completions` and `POST /v1/messages` (plain and SSE streaming), `GET /v1/models`,
  `POST /v1/embeddings`, `GET /public/models.json`, generated model constants, `cmd/avelin-models`.
- Not yet: audio, images, TTS, `/v2` web endpoints (docs defer to Firecrawl), image input,
  embeddings `encoding_format`, forcing a specific tool.

## Layout
```
client.go       Client, options, Meta, request + retry plumbing, ErrMissingAPIKey
errors.go       APIError and body parsing
stream.go       SSE reader, Stream[T]
chat.go messages.go models.go embeddings.go
models_gen.go   written by internal/genmodels (go generate), with testdata/public_models.json
internal/mockapi  fixture-backed handler used by unit tests and cmd/mockserver
cmd/avelin-models cmd/mockserver examples/{chat,stream,messages}  testdata/ fixtures
```

## Public API
```go
NewClient(opts ...Option) *Client  // key: AVELIN_API_KEY; base URL: AVELIN_BASE_URL or DefaultBaseURL
WithAPIKey WithBaseURL WithHTTPClient WithMaxRetries WithTimeout
CreateChatCompletion / CreateChatCompletionStream -> *ChatCompletion / *Stream[ChatCompletionChunk]
CreateMessage / CreateMessageStream               -> *Message / *Stream[MessageStreamEvent]
ListModels -> *ModelList   CreateEmbeddings -> *EmbeddingResponse   FetchCatalog -> *Catalog (no key)
Stream[T]: Next, Current, Err, Close, Meta     APIError{StatusCode, Type, Code, Message, RequestID, Header, Body}
Results: Meta{StatusCode, Header} + Raw (full JSON).  Helpers: Ptr, Message.Text, ModelBGEM3
```

## Schema sources
| Item | Source |
|---|---|
| Chat request/response, reasoning_content, chunks, [DONE] | AVELIN reference (confirmed) |
| Chat tool calls; stream tool_call `index`; `tool_call_id` | AVELIN shape; index/tool_call_id from OpenAI spec |
| Messages request/response, thinking, stream events | AVELIN reference; ping/error events, signature, input_json_delta, tool_result from Anthropic spec |
| Messages `usage.total_tokens`, `anthropic-version: 2023-06-01` | AVELIN reference |
| Bearer auth on all endpoints | AVELIN reference (x-api-key not used) |
| /v1/models, /v1/embeddings (bge-m3, 1024 dims) | AVELIN reference |
| models.json fields | observed live (undocumented) |
| Error bodies | documented `{"error":{message,type,code}}`; observed `{"error":{message}}` and `{"detail":...}`; Anthropic shape also parsed |

## Assumptions (not confirmed by AVELIN)
- Request ID header: none documented or observed; `X-Request-Id`, then `Request-Id`.
- `error.code` may be a number or string; exposed as text.
- `thinking.budget_tokens` (Anthropic spec; AVELIN mentions it only in a guide).
- A chat chunk with a top-level `error`, or an Anthropic `error` event, ends the stream with *APIError.
- Retries: 429 and 5xx only (documented), never transport errors (POSTs may not be idempotent), never
  after a stream starts; Retry-After above 60s is returned as an error instead of waited on.
- `WithTimeout` (default 10 min) covers a whole non-streaming attempt, but only the header wait for streams.
- `avelin-models` family column is derived from the ID; `avelin-<tier>` is "intelligence" per the docs.
- SSE: events without a terminating blank line are dropped at EOF (spec); a stream without its
  terminator reports `io.ErrUnexpectedEOF`.

## Claims checked
- "Ultra models don't support reasoning_effort": not found; docs and models.json say they do.
- "models.json shows a multimodal family": it has no family field. See FINDINGS.md.

## Tests
- httptest for every endpoint: success, API errors, 429 + Retry-After, 5xx retry, cancel, timeout,
  truncated and broken streams, error events; fixtures in `testdata/` copied from the docs' examples.
- `-tags integration` (skips without AVELIN_API_KEY), `-tags live` (public catalog only).
- genmodels test keeps `models_gen.go` in sync with the catalog snapshot.
- Dev machine had no C compiler; `-race` ran with `CC="zig cc"`. staticcheck 2026.2.1 clean.

## Phases
0 research + plan, 1 skeleton, 2 chat, 3 messages, 4 models/embeddings/catalog/codegen,
5 mock + integration/live, 6 docs, 7 review. One commit each.
