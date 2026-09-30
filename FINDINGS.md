# FINDINGS

Checked on 2026-09-30 (UTC) using public pages and requests without a key only (no sign-in). The docs
pages render client-side, so their raw sources at `https://avelin.ai/docs/<page>.md` were read.

## API behavior (observed)

1. Unauthenticated calls to `/v1/models`, `/v1/chat/completions`, `/v1/messages` and `/v1/embeddings` return `401 {"error": {"message": "Access denied."}}` with no `type` or `code`; the reference documents `{"error":{"message":"Invalid API key provided","type":"authentication_error","code":401}}`. https://avelin.ai/docs/api/reference
2. Unknown routes return `404 {"detail": "Not Found"}` (the FastAPI default), not the documented error shape, although the docs list 404 for "Unknown model or endpoint". https://api.avelin.ai/v1/does-not-exist
3. `/v1/messages` answers with the OpenAI error shape (observed on 401), not the Anthropic shape `{"type":"error","error":{"type":...,"message":...}}` that Anthropic clients expect. https://api.avelin.ai/v1/messages
4. No request ID header was present on any observed response, while the reference advises logging errors "with request IDs". https://avelin.ai/docs/api/reference
5. `HEAD https://api.avelin.ai/public/models.json` returns 404 while `GET` returns 200, so tools that probe with HEAD see it as missing. https://api.avelin.ai/public/models.json

## Docs vs the public catalog

6. The catalog at `https://api.avelin.ai/public/models.json` is not mentioned in the docs; its fields and price units (USD per token, as decimal strings) are undocumented, and it has no family field. https://api.avelin.ai/public/models.json
7. The catalog lists `reasoning_effort` in `supported_sampling_parameters` for all 9 models, including `avelin-fast`, which the reference marks "Not supported" for reasoning. https://avelin.ai/docs/api/reference
8. The catalog covers only the 9 chat models; `bge-m3`, `whisper-large-v3`, `whisper-large-v3-turbo`, `avelin-stt`, `tts-1`, `tts-1-hd`, `avelin-imagegen` and `avelin-imagegen-pro`, all on the pricing page, are absent. https://avelin.ai/docs/pricing
9. The docs and API page call `avelin-fast`, `avelin-pro` and `avelin-ultra` the "Intelligence" family, but these IDs have no family segment (`avelin-pro` vs `avelin-coding-pro`), so the family cannot be read from the ID or the catalog. https://avelin.ai/business/avelin-api

## Docs inconsistencies

10. Reasoning defaults disagree: the reference says `avelin-fast` "Not supported" and `avelin-coding-fast`/`avelin-agentic-fast` "Full, Enabled"; the model catalog page says all three fast tiers are "Off by default"; the families page says `avelin-fast` "Limited" and the other two "Full", while its text says the coding fast tier has thinking off by default. https://avelin.ai/docs/models/README
11. `reasoning_effort` is documented only with the value `"high"`; whether `"low"` or `"medium"` are accepted is not stated. https://avelin.ai/docs/api/reference
12. The quickstart's Anthropic Python example calls `avelin-pro` and prints `msg.content[0].text`, but the same page says thinking is on by default with the thinking block first; the SDK's `ThinkingBlock` has no `text` attribute, so the example would fail when thinking is on. https://avelin.ai/docs/api/quickstart
13. Chat stream framing differs: the API index shows consecutive `data:` lines with no blank line between events, the reference shows blank-line-separated events; under the SSE spec the first form merges into one event. This client follows the reference. https://avelin.ai/docs/api/index
14. `tool_choice` is `"auto"` or `"none"` in the API index and "auto, none, or specific tool name" (type string) in the reference; OpenAI forces a function with an object, so how to force a tool is unclear. https://avelin.ai/docs/api/reference
15. Errors are said to "follow the OpenAI error format", but `code` is documented as a number (`401`); OpenAI's `code` is a string or null. https://avelin.ai/docs/api/reference
16. The reference documents only `Authorization: Bearer` for `/v1/messages`, but the quickstart's Anthropic SDK examples pass `api_key=`, which the SDK sends as `x-api-key`, and the Cline guide's curl uses `x-api-key`; the reference does not say both are accepted. https://avelin.ai/docs/guides/coding/cline
17. Messages response IDs look like `resp_...` in the quickstart and API index but `msg_abc123` in the reference. https://avelin.ai/docs/api/quickstart
18. The reference's retry example retries on every exception, including 4xx, although the text says to back off on 429 and 5xx. https://avelin.ai/docs/api/reference
19. The AVELIN-API system page still uses pre-rename names (`coding-plus`, `coding-architect`, `agentic-high`) and lists "the coding tier" inside the Intelligence family. https://avelin.ai/docs/systems/avelin-api
20. The `/v2` web endpoints defer their full request and response schemas to Firecrawl's v2 docs, so a client cannot be built from AVELIN's docs alone. https://avelin.ai/docs/api/web-scraping
21. Endpoint lists differ: the API page lists `/v2` scrape, search and crawl; the docs also have `/v2/map` and `/v2/extract`; `POST /v1/audio/speech` appears on the utility models page but not in the API reference. https://avelin.ai/docs/models/utility-models

## Site

22. `https://docs.avelin.ai` is linked from the platform sign-in page ("Read the docs") but does not resolve (NXDOMAIN from public DNS). https://platform.avelin.ai
23. Docs page HTML contains only the navigation; content loads from `https://avelin.ai/docs/<page>.md` (served as `application/octet-stream`), so crawlers and non-JS tools see no content. https://avelin.ai/docs/api/reference
24. Billing is described two ways: the platform page offers a prepaid balance, while the pricing page says "Metered per token, invoiced monthly" and to request a key from sales@avelin.ai. https://avelin.ai/docs/pricing
25. The platform sign-in page's public HTML includes a "Dev scenario" panel (Reset active, Balance exhausted, Account suspended). Not tested; it may be hidden or inert in production. https://platform.avelin.ai

## Checked and not confirmed

- "Ultra models don't support `reasoning_effort`": no source says so. The docs mark every ultra model as full reasoning, and the catalog lists `reasoning_effort` for all of them.
- "models.json shows a multimodal family": the catalog has no family field and never says "multimodal". The docs use "multimodal" only to describe the Intelligence family, whose three models are the only ones with `image` input.
