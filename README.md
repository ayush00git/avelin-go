# avelin-go

[![CI](https://github.com/ayush00git/avelin-go/actions/workflows/ci.yml/badge.svg)](https://github.com/ayush00git/avelin-go/actions/workflows/ci.yml)

Unofficial Go client for the [AVELIN](https://avelin.ai) AI API: chat completions (OpenAI-style),
messages (Anthropic-style), models, embeddings and the public model catalog.
Standard library only, Go 1.22+. Not affiliated with or endorsed by AVELIN.

## Install

```sh
go get github.com/ayush00git/avelin-go
```

## Quickstart

```go
client := avelin.NewClient() // reads AVELIN_API_KEY
resp, err := client.CreateChatCompletion(context.Background(), avelin.ChatCompletionRequest{
	Model:    avelin.ModelPro,
	Messages: []avelin.ChatMessage{{Role: "user", Content: "Give me three productivity tips."}},
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(resp.Choices[0].Message.Content)
```

More in [examples/](examples): `chat`, `stream`, `messages`, and tool calling in `chat-tools` and
`messages-tools`.

## Supported endpoints

| Endpoint | Methods | Needs key |
|---|---|---|
| `POST /v1/chat/completions` | `CreateChatCompletion`, `CreateChatCompletionStream` | yes |
| `POST /v1/messages` | `CreateMessage`, `CreateMessageStream` | yes |
| `GET /v1/models` | `ListModels` | yes |
| `POST /v1/embeddings` | `CreateEmbeddings` (`bge-m3`) | yes |
| `GET /public/models.json` | `FetchCatalog` | no |

## Options

| Option | Default |
|---|---|
| `WithAPIKey` | `AVELIN_API_KEY` env var |
| `WithBaseURL` | `AVELIN_BASE_URL` env var, else `https://api.avelin.ai` (no `/v1`) |
| `WithHTTPClient` | `http.DefaultClient` |
| `WithMaxRetries` | 2 retries on 429 and 5xx, exponential backoff with jitter, honors `Retry-After` |
| `WithTimeout` | 10 minutes per attempt; a stream is only timed until a successful response starts |

Every call takes a `context.Context`. A stream is never retried once it has started.

## Streaming

```go
stream, err := client.CreateMessageStream(ctx, avelin.MessageRequest{
	Model:     avelin.ModelCodingFast,
	MaxTokens: 1024,
	Messages:  []avelin.MessageParam{{Role: "user", Content: "Write a debounce helper in Go."}},
})
if err != nil {
	log.Fatal(err)
}
defer stream.Close()
var msg avelin.Message
for stream.Next() {
	ev := stream.Current()
	if ev.Delta != nil {
		fmt.Print(ev.Delta.Text)
	}
	if err := msg.Accumulate(ev); err != nil {
		log.Fatal(err)
	}
}
if err := stream.Err(); err != nil {
	log.Fatal(err)
}
fmt.Println(msg.StopReason, msg.Usage.OutputTokens)
```

`Message.Accumulate` and `ChatCompletion.Accumulate` build the full result from a stream, tool
calls included, so it can be read or sent back as history. A stream that ends before `[DONE]` or
`message_stop` returns an error wrapping `io.ErrUnexpectedEOF`.

## Errors and metadata

Non-2xx responses, and errors reported inside a stream, return `*avelin.APIError` (use
`errors.As`) with `StatusCode`, `Type`, `Code`, `Message`, `RequestID`, `Body` and `Header`, which
includes AVELIN's `X-RateLimit-*` headers.

Every value a method returns carries `Meta` (HTTP status and headers) and `Raw` (the full JSON
body), so headers and fields this package does not model are still reachable. Streams expose
`Meta()`, and each event has `Raw`.

## Model IDs

`models_gen.go` has a constant per model in the public catalog (`ModelFast` ... `ModelAgenticUltra`);
refresh it with `go generate ./...`. `ModelBGEM3` is the embeddings model. Legacy names such as
`"avelin-coding"` work as plain strings.

## Tools

- `go run ./cmd/avelin-models` prints the live public catalog (id, family, context, $/1M tokens). No key needed.
- `go run ./cmd/mockserver` serves the `testdata/` fixtures on `127.0.0.1:8089` for trying the examples offline.
- [VERIFY.md](VERIFY.md) has step-by-step checks.

## Not yet

- `/v1/audio/transcriptions`, `/v1/audio/speech`, `/v1/images/generations`
- `/v2` web endpoints (`scrape`, `search`, `map`, `crawl`, `extract`): AVELIN defers their schemas to Firecrawl's docs
- Image input, embeddings `encoding_format: "base64"`, forcing a specific tool with `tool_choice`

## Status

Built from AVELIN's published docs and tested against mock servers only; it has not been run
against the authenticated API. Assumed rather than documented: the request ID header
(`X-Request-Id`, then `Request-Id`), how errors appear mid-stream (they end the stream with
`*APIError`), and `thinking.budget_tokens` (from Anthropic's spec).

## License

MIT, see [LICENSE](LICENSE). This is an unofficial project, not affiliated with AVELIN.
