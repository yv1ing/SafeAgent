# Protocol layer

This document describes the canonical wire model, the streaming contract and the conversion rules shared by the four protocol codecs.

## Canonical model

`internal/protocol` is standard-library only. The four formats map onto one model:

- `Format`: `openai_chat`, `openai_responses`, `anthropic_messages`, `google_gemini`.
- `Request`: instructions, messages, tools, tool choice, sampling, response format, reasoning, limits, stop, metadata and provider-state fields (`store`, `include`, `truncation`, `max_tool_calls`, `safety_identifier`, `prompt_cache_retention`, `verbosity`, logprobs, modalities).
- `Message`/`Block`: text, reasoning, redacted reasoning, tool call, tool result, media, citation, refusal, native.
- `Response`: blocks, finish, stop sequence, failure, usage and `ResponseMeta` (id, model, created, system fingerprint, service tier).
- `Extras`: unknown provider fields keyed by source format. They replay only to that format; any other target records a loss. Unknown nested fields below the first supported level are not captured today; this is a known fidelity limit, and the gateway does not silently rewrite it.

## Stream contract

`Event` kinds: `message_start`, `block_start`, `block_delta`, `block_stop`, `message_delta`, `message_stop`. Blocks are strictly sequential; deltas carry only new content; the stop snapshot is the concatenated deltas. `BlockStream` enforces the contract.

SSE parsing follows the event format: a bare `data` line carries no payload, a comment is ignored, and an event with no data does not produce a frame. `max_stream_bytes` is capped at 128 MiB by the reader itself; configuration enforces the same ceiling.

### Clean EOF

A provider may close the connection without its terminal frame. The decoder synthesizes a terminal event only when the response is complete:

- at least one text/reasoning/tool-call block exists;
- every function tool call with arguments has valid JSON, and an absent argument object is normalized to `{}`;
- free-text custom tools are exempt.

Otherwise `End` returns `ErrTruncated`. A synthesized end carries `LossCleanEOF`. This distinguishes a clean EOF from a transport cut.

## Media

Media carries a URL, inline data or a provider file identifier. A data URL is accepted and split into its MIME type and payload. When MIME is absent, it is inferred from the URL path and then from the filename; inline Chat/Responses file data without either uses `application/octet-stream` for the data URL. Messages documents require PDF; Gemini requires a known MIME type.

## Conversion policy

Every request goes through decode → canonical → encode, including same-format traffic. There is no same-format passthrough, so redaction and analysis see message, tool, metadata and parameter values. A conversion that cannot carry a field records a `Loss` with a path, code and target; it does not silently drop it.

Important mappings:

- Usage: `Input` is gross input including cache reads/writes. Cost prices uncached input separately. Gemini `toolUsePromptTokenCount` is a breakdown field and is not added to the prompt count.
- Chat `url_citation` annotations use `start_index`/`end_index`; Messages citations use `start_char_index`/`end_char_index`. Both decode into the same citation offsets and encode where an equivalent exists.
- Responses `store` is always false upstream; the history is replayed by the gateway. A client `store=true` records a loss.
- Anthropic thinking supports `enabled` with or without a budget, `adaptive`, `display` and `between_tools`.
- Gemini `thinkingLevel` is a protobuf enum (`LOW`, `HIGH`, `THINKING_LEVEL_UNSPECIFIED`); it is mapped to the canonical effort tiers before re-encoding.
- A Gemini response with only `promptFeedback.blockReason` becomes a content-filtered completion, not an empty transport failure.
- Reasoning signatures are sealed towards clients and verified on echo; foreign signatures are stripped with a loss, never forged. Unsigned reasoning keeps its native kind in the target format (Messages `thinking`, Responses `reasoning`) because providers such as DeepSeek require it when a tool loop replays an earlier turn; it is never rewritten as visible assistant text. A provider that requires a signature may reject an unsigned turn; the gateway never forges one.
- Responses input items that belong to one assistant turn are grouped into one canonical message. Messages and tool calls then travel together when the target has no item sequence.
- Anthropic Messages accepts a `system` message inside the array (Claude Code emits one); the canonical model keeps the role and the Messages encoder folds it into the leading system prompt.

## Tests

- `internal/protocol/format/testdata/axonhub` holds AxonHub-derived fixtures.
- `format_test.go` checks request/response/stream round trips and the 4×4 matrix.
- Codec packages test terminal/truncation, empty responses, tool-call aggregation, thinking, citations, media and usage.

## Related

- [Model](model.md)
- [Development](development.md)
- [Agent guide](README.md)
