# Protocol layer

This document describes the canonical model, the stream session, the provider catalog, the loss ledger and the conversion rules shared by the four codecs.

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/llm/model` | Canonical request, response, item, event, loss and profile types. Standard library only. |
| `internal/llm/codec` | Codec contract, shared conversion helpers and the `chat`, `responses`, `messages`, `gemini` codecs. |
| `internal/llm/provider` | `ProviderSpec`, `EndpointSpec`, `Capability`, `Compat`, endpoint URL/auth construction and model discovery parsing. |
| `internal/llm/provider/builtin` | One file per built-in provider; `All()` registers them. |
| `internal/llm/stream` | One streaming session per call: commit boundary, tool holds, restoration, keepalive, raw pass-through and JSON synthesis. |
| `internal/pipeline` | Admission, redaction, four policy checkpoints, routing, attempts, audit and auxiliary endpoints. |
| `internal/transport/http` | HTTP and WebSocket routes, body limits and Gemini framing. |

Codecs never branch on a vendor name. They read `Peer.Profile`, which carries the endpoint's `Capability` and `Compat`; the provider catalog supplies it.

## Canonical model

The four formats (`openai_chat`, `openai_responses`, `anthropic_messages`, `google_gemini`) map onto one item model:

- `Request`: model, instructions, ordered items, tools, tool choice, parallel policy, response format, reasoning, limits, stop, stream options, choice count, safety settings, provider-state fields (`store`, `background`, `conversation`, `include`, `truncation`, `max_tool_calls`, `prompt_cache_retention`), identity and raw overlay.
- `Item`: message, reasoning, tool call, tool result, server tool call, server tool result or native provider object. A server item carries `Server *ServerInfo{Format, Type, Capability, Config}` so the same format replays it exactly.
- `Part`: text, refusal, reasoning text/summary, redacted reasoning, tool arguments, media or native payload. Citations, logprobs and prompt-cache markers attach to parts.
- `Response`: items, `Status`, `Finish`, `Usage`, failure, `Meta` (id, model, created, fingerprint, service tier) and raw overlay.
- `Raw`: unknown fields at request, item, part, tool, citation, usage, event and response level, tagged with the source format.

`Event` kinds stay `message_start`, `item_start`, `part_start`, `part_delta`, `part_stop`, `item_stop`, `message_delta`, `message_stop`, `error` and `ping`. `Event.Raw` carries the unknown fields of a terminal envelope.

`model.Assembler` is the decoder-side state machine: it numbers items and parts, guarantees one start and one stop per node, materializes tool arguments and implements clean EOF. `model.Journal` is the session-side state: it folds the events the client actually received into the response the audit stores, including items that were still open when a call was cancelled.

## Stream session

Each call creates its own `stream.Session`, which tracks four behaviors:

1. **Commit boundary.** No HTTP status or SSE header is written before the first forwardable content. A failure before that point can still fail over to another channel or key. Once a frame is written, the response is committed.
2. **Holding.** Text and reasoning deltas are forwarded immediately. A complete tool item is queued until the gate returns; queued frames are then released or discarded. If a tool item is the first content, the response remains uncommitted while it is held. Holding is bounded by `ToolHoldBytes`; exceeding it is a resource error (`tool_call_too_large`), not a policy verdict.
3. **Termination.** The session stages terminal frames, merges losses and lets the pipeline seal the call before writing them. A failed committed stream writes a protocol-native error frame. An uncommitted failure returns an HTTP error and can fail over.
4. **Cancellation.** When the client disconnects, the session stops writing and the pipeline cancels upstream. A committed stream's already-sent content is stored as a partial response.

Keepalive frames are protocol-native: an SSE comment for Chat and Responses, `event: ping` for Messages, nothing for Gemini. They start only after the response is committed.

A decoder that reaches a clean EOF synthesizes the terminal event and records `clean_eof_synthesized`. An incomplete stream is a failure.

## Raw pass-through

A same-format request may be forwarded as raw bytes only when all of the following hold:

- source and target format are the same;
- redaction made no hit and no signature was unsealed;
- history was not expanded and no repair ran;
- the endpoint's `rawSafe` conditions pass (for Messages, an output limit is present; for Responses, `previous_response_id` is empty and `store` is false/absent);
- no reasoning item is missing.

Only `model`, `stream`, Chat `include_usage` and Responses state fields are patched. Responses never uses the raw response fast path; other same-format responses are forwarded raw while every part is text, refusal or reasoning text/summary. A tool, media, native item or incompatible ID switches to the canonical encoder. Losses and the request-side summary are still recorded; the summary is returned as `X-SafeAgent-Losses` (unique codes) and `X-SafeAgent-Loss-Count`, while a streaming response keeps its response-side losses in the audit record. A non-streaming response is encoded before its headers are sent, so its response conversion losses are included in the same headers.

## Provider catalog

Each provider is one `ProviderSpec` with an `EndpointSpec` per format. An endpoint declares the base URL, path, count-token path, auth style, static headers, capability, compatibility quirks, discovery path/shape, transport mode, model aliases and auxiliary endpoints.

A channel stores `provider_id`, per-format endpoint overrides (base URL, path, disabled), capability overrides, headers and limits. An endpoint entry overrides the catalog; an absent entry keeps the provider default; a disabled entry hides that format. See [Providers](providers.md) for the full override semantics.

## Loss ledger

`Ledger` holds `Losses` and `Repairs`. Every loss has a path, a code, a class, the source/target formats and an item key:

- `fatal`: the target cannot represent core semantics. The gateway rejects before any upstream call. Media that cannot be carried and a required server tool with no equivalent are fatal.
- `degraded`: private or secondary data is dropped while the request continues. Foreign signatures, provider fields, citation subfields, usage breakdowns, logprobs and mapped tool kinds are degraded.

Additional codes include `server_tool_mapped`, `tool_arg_granularity`, `signature_resealed`, `media_fetched`, `media_unavailable`, `capability_field_removed`, `multi_choice_requires_passthrough`, `count_estimated`, `usage_estimated` and `http200_failed_status`.

`RepairRequest` restores target invariants and records each change: empty items, unanswered calls, orphan results, tool results after their assistant turn, assistant text before tool calls in Responses, reasoning order for Responses and Messages, and an impossible tool choice.

## Conversion policy

Important mappings:

- IDs: `resp_`, `msg_` and `chatcmpl-` prefixes are preserved only when they match the target; otherwise a target-native gateway-stable ID is synthesized. Gemini call IDs are synthesized with a per-call salt when absent and paired to `functionResponse` by name and order.
- Usage: `Input` is gross input including cache reads/writes. Chat audio and prediction buckets are kept in `Usage.Details`; cache, reasoning and Gemini thought counts stay in their typed fields.
- Finish: every target maps the canonical kind and records `finish_mapped` when the exact meaning is lost.
- Output limit: an endpoint with `RequireMaxTokens` needs one. An explicit client value wins, then the route target default, the channel default, the project default, the endpoint default, and finally 8192. A defaulted limit is recorded.
- Headers: a protocol-aware allowlist forwards `anthropic-version`, `anthropic-beta`, `OpenAI-Beta`, `OpenAI-Organization`, `OpenAI-Project` and `x-goog-api-client`, plus channel-configured names. Credentials, cookies, hop-by-hop headers and every `X-SafeAgent-*` header are never forwarded; a source-only protocol header is recorded. `X-SafeAgent-Model` selects the public model of a request and must still name a route in the caller's project.
- Reasoning signatures are sealed towards clients and verified on echo; a foreign signature is stripped with a loss. The envelope also carries the reasoning text. Gemini reasoning without an upstream signature receives a gateway envelope so the Gemini CLI keeps it.
- When an assistant tool call has no reasoning, target reasoning is disabled and `reasoning_not_replayable` is recorded.
- Server tool calls are replayed natively towards their own format and mapped by capability (`web_search`, `code_execution`) across formats. An unmatched required server tool is fatal.
- Media: the capability descriptor decides whether each carrier (URL, inline data, file ID, tool-result media) is available. Core media that cannot be represented is rejected before the upstream.
- Responses state: the gateway owns it. `previous_response_id` is expanded from gateway history; `store=true` is served by the gateway while the upstream always receives `store=false`. `background` and `conversation` are rejected.
- Multiple candidates (`n>1` or `candidateCount>1`) are served only by a streaming same-format pass-through with no redaction or history expansion. Any other combination is refused with `unsupported_multi_choice`.

## Session linking

The optional `X-SafeAgent-Session` header groups calls from one agent key. The value is trimmed, limited to 128 bytes and rejected if it contains control characters; the stored id is `sess_` plus a SHA-256 prefix over the agent key and the raw value. It is never an identity and never replaces authentication.

Parent resolution order: `previous_response_id`, then the latest tool result, then the conversation-prefix fingerprint. The explicit header does not discover a parent; it names the session that groups the call. An explicit header that conflicts with a `previous_response_id` parent returns 409 `session_conflict` and the call is recorded as rejected. A heuristic parent in another session is dropped without rejecting the call. `auditevent.Request.SessionSource` records which source was used.

## Checkpoints and auxiliary endpoints

Online policy runs at four points: request admission, added tool result, completed tool call and non-streaming response. Request and tool-result blocks run before the upstream; a tool-call block runs before the call is released to the client; the response checkpoint runs before a JSON response is written. Streaming response text has no online checkpoint.

The gateway serves model listing and retrieval in OpenAI, Anthropic and Gemini shapes, native token counting (`/v1/messages/count_tokens`, `/v1/responses/input_tokens`, `/v1beta/models/{model}:countTokens`), Responses retrieve/input_items/cancel/delete, and same-format embeddings and moderations passthrough. Counting falls back to a local estimate and sets `X-SafeAgent-Token-Estimate: true`; embeddings and moderations return `auxiliary_unsupported` when no channel declares them. Every generation, counting, model, passthrough and Responses lifecycle call is authenticated, rate-limited and audited; a begin or seal failure refuses the operation.

## Tests

The test map, including the 4×4 codec matrix and the pipeline suites, is in [Development](development.md).

## Related

- [Providers](providers.md)
- [Audit](audit.md)
- [Development](development.md)
- [Agent guide](README.md)
