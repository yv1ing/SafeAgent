# Protocol layer

This page owns the canonical wire model, the streaming session, raw pass-through, the conversion loss ledger, admission control, session linking and the gateway endpoint surface. The provider graph those endpoints resolve against is in [Providers](providers.md); the observation model built from a decoded request is in [Model](model.md).

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/llm/model` | Canonical request, response, item, event and loss types, `Capability` and `Compat`. Standard library only. |
| `internal/llm/codec` | Codec contract, shared conversion helpers and the `chat`, `responses`, `messages`, `gemini` codecs. |
| `internal/llm/provider` | `ProviderSpec`, `EndpointSpec`, endpoint URL/auth construction, endpoint catalog (de)serialization and model discovery parsing. |
| `internal/llm/provider/builtin` | One file per built-in provider seed; `All()` returns them in order. |
| `internal/llm/stream` | One streaming session per call: commit boundary, tool holds, restoration, keepalive, raw pass-through and JSON synthesis. |
| `internal/pipeline` | Admission, classification, redaction, checkpoints, routing, attempts, audit and auxiliary endpoints. |
| `internal/transport/gateway` | HTTP and WebSocket route registration, body limits and Gemini framing. |

Codecs never branch on a vendor name. They read `Peer.Profile`, which carries the endpoint's `Capability` and `Compat`; the stored provider endpoint catalog supplies it.

## Canonical model

The four formats (`openai_chat`, `openai_responses`, `anthropic_messages`, `google_gemini`) map onto one item model:

- `Request`: model, instructions, ordered items, tools and tool policy, response format, reasoning, limits, sampling (`temperature`, `top_p`, `top_k`, `stop`, `seed`, penalties, `logit_bias`, `logprobs`, `modalities`), stream options, choice count, safety settings, provider-state fields (`previous_response_id`, `store`, `background`, `conversation`, `include`, `truncation`, `max_tool_calls`, `prompt_cache_retention`), identity (`metadata`, `user`, `service_tier`, `prompt_cache_key`, `safety_identifier`, `verbosity`) and a raw overlay.
- `Item`: message, reasoning, tool call, tool result, server tool call, server tool result or native provider object. A server item carries `Server *ServerInfo{Format, Type, Capability, Config}` so the same format replays it exactly.
- `Part`: text, refusal, reasoning text/summary, redacted reasoning, tool arguments, media or native payload. Citations, logprobs and prompt-cache markers attach to parts.
- `Response`: items, `Status`, `Finish`, `Usage`, failure, `Meta` (id, model, created, fingerprint, service tier) and a raw overlay.
- `Raw`: unknown fields at request, item, part, tool, citation, usage, event and response level, tagged with the source format.

`Event` kinds are `message_start`, `item_start`, `part_start`, `part_delta`, `part_stop`, `item_stop`, `message_delta`, `message_stop`, `error` and `ping`. `Event.Raw` carries the unknown fields of a terminal envelope.

`model.Assembler` is the decoder-side state machine: it numbers items and parts, guarantees one start and one stop per node, materializes tool arguments and implements clean EOF. `model.Journal` is the session-side state: it folds the events the client actually received into the response the audit stores, including items that were still open when a call was cancelled.

## Stream session

Each call creates its own `stream.Session`, which tracks five behaviors:

1. **Commit boundary.** No HTTP status or SSE header is written before the first forwardable content. A failure before that point can still fail over to another channel or key. Once a frame is written, the response is committed.
2. **Stream review.** When a project enables it, the session holds the leading text until `ReviewBytes` fill the window, until the next frame arrives after `ReviewTimeout`, or until a short stream ends. The pipeline runs the response checkpoint on that preview; a block returns a protocol error before the first byte. After the window commits, later text is not reviewed.
3. **Holding.** Text and reasoning deltas are forwarded immediately unless the stream review window is open. A complete tool item is queued until the gate returns; queued frames are then released or discarded. If a tool item is the first content, the response stays uncommitted while it is held. Holding is bounded by `ToolHoldBytes`; exceeding it is a resource error (`tool_call_too_large`), not a policy verdict.
4. **Termination.** The session stages canonical terminal frames, merges losses and lets the pipeline seal the call before writing them. A raw pass-through keeps the provider's terminal frame and writes it as received. A failed committed stream writes a protocol-native error frame. An uncommitted failure returns an HTTP error and can fail over.
5. **Cancellation.** When the client disconnects, the session stops writing and the pipeline cancels upstream. A committed stream's already-sent content is stored as a partial response.

The journal records what the session actually delivered. When a committed stream fails, items whose frames never left the hold or review queue are removed before the partial response is stored; a held sibling tool call therefore cannot appear in the audit response.

Keepalive frames are protocol-native: an SSE comment for Chat and Responses, `event: ping` for Messages, nothing for Gemini. They start only after the response is committed.

A decoder that reaches a clean EOF synthesizes the terminal event and records `clean_eof_synthesized`. An incomplete stream is a failure.

## Raw pass-through

A same-format request may be forwarded as raw bytes only when all of the following hold:

- source and target are the same Chat, Messages or Gemini format (Responses always re-encodes because the gateway owns `store`, `include` and `previous_response_id`);
- redaction made no hit, no signature was present and history was not expanded;
- adaptation, compatibility rewrites, output-limit defaults, custom-tool downgrades and capability preflight produced no loss; no repair ran;
- the canonical encoder would preserve every modelled field: unsupported sampling fields, stream options, tool schemas or selections, response formats, reasoning budgets, rewritten roles and legacy Chat aliases all disable the fast path;
- for Messages, an output limit is present;
- the raw request body survives the format's own safety check.

Only `model`, `stream` and Chat `include_usage` are patched. Responses are always encoded canonically. A same-format response is forwarded raw while every part is text, refusal or reasoning text/summary; a tool, media, native item or incompatible ID switches to the canonical encoder. Stream review buffers the raw frames until its window is released; it does not require the canonical encoder.

Every generation conversion loss is written to the call's audit record. A streaming response sets `X-SafeAgent-Losses` (unique request-side codes) and `X-SafeAgent-Loss-Count` before its first byte; response-side losses stay in the audit record. A non-streaming response is encoded before its headers are sent, so its headers carry the complete conversion ledger. Auxiliary endpoints report their own conversion losses only where their wire shape exposes them.

## Provider catalog

A provider is one `ProviderSpec` with an `EndpointSpec` per wire format. An endpoint declares the base URL, path, count-token path, auth style, static headers, capability, compatibility quirks, discovery path/shape, transport mode, model aliases and auxiliary endpoints. At startup the built-in specs are seeded into the `providers` table and the runtime catalog is loaded from that table; a channel stores a provider id and the host/limits/credentials, not a format list. The routing layer derives the usable formats from the provider endpoints, the catalog model kind and an optional target-pinned format. [Providers](providers.md) owns the graph and its lifecycle.

## Loss ledger

`Ledger` holds `Losses` and `Repairs`. A loss carries a code and class; its path, source/target formats and item key are filled when they apply:

- `fatal`: the target cannot represent core semantics. The gateway rejects before any upstream call. Media that cannot be carried and a required server tool with no equivalent are fatal.
- `degraded`: private or secondary data is dropped while the request continues. Foreign signatures, provider fields, citation subfields, usage breakdowns, logprobs and mapped tool kinds are degraded.

Additional codes include `signature_stripped`, `server_tool_mapped`, `item_id_synthesized`, `capability_field_removed`, `clean_eof_synthesized`, `reasoning_not_replayable` and `multi_choice_requires_passthrough`.

`RepairRequest` restores target invariants and records each change: empty items, unanswered calls, orphan or duplicate results, duplicate calls, tool results after their assistant turn, assistant text before tool calls in Responses, reasoning order for Responses and Messages, and an impossible tool choice.

## Conversion policy

Important mappings:

- IDs: `resp_`, `msg_` and `chatcmpl-` prefixes are preserved only when they match the target; otherwise a target-native gateway-stable ID is synthesized. Gemini call IDs are synthesized with a per-call salt when absent and paired to `functionResponse` by name and order.
- Usage: `Input` is gross input including cache reads/writes. Chat audio and prediction buckets are kept in `Usage.Details`; cache, reasoning and Gemini thought counts stay in their typed fields.
- Finish: every target maps the canonical kind and records `finish_mapped` when the exact meaning is lost.
- Output limit: an endpoint with `RequireMaxTokens` needs one. An explicit client value wins, then the route target default, the channel default, the project default, the endpoint default, and finally 8192. A defaulted limit is recorded.
- Headers: a protocol-aware allowlist forwards `anthropic-version`, `anthropic-beta`, `OpenAI-Beta`, `OpenAI-Organization`, `OpenAI-Project` and `x-goog-api-client`, plus channel-configured names. Credentials, cookies, hop-by-hop headers and every `X-SafeAgent-*` header are never forwarded; a source-only protocol header is recorded as a loss.
- Reasoning signatures are sealed towards clients and verified on echo; a signature is written only when the target format's text coverage can carry it, and a foreign or uncovered signature is stripped with a `signature_stripped` loss. The envelope also carries the reasoning text. Gemini reasoning without an upstream signature receives a gateway envelope so the Gemini CLI keeps it.
- When an assistant tool call has no reasoning, target reasoning is disabled and `reasoning_not_replayable` is recorded.
- Server tool calls are replayed natively towards their own format and mapped by capability (`web_search`, `code_execution`) across formats. An unmatched required server tool is fatal.
- Media: the capability descriptor decides whether each carrier (URL, inline data, file ID, tool-result media) is available. Core media that cannot be represented is rejected before the upstream.
- Responses state: the gateway owns it. `previous_response_id` is expanded from gateway history; `store=true` is served by the gateway while the upstream always receives `store=false`. `background` and `conversation` are rejected.
- Multiple candidates (`n>1` or `candidateCount>1`) are served only by a streaming same-format pass-through with no redaction or history expansion. Any other combination is refused with `unsupported_multi_choice`.

## Session linking

The optional `X-SafeAgent-Session` header groups calls from one agent key. The value is trimmed, limited to 128 bytes and rejected if it contains control characters; the stored id is `sess_` plus a SHA-256 prefix over the agent key and the raw value. It is never an identity and never replaces authentication.

Parent resolution order: `previous_response_id`, then the latest tool result, then the conversation-prefix fingerprint. The explicit header does not discover a parent; it names the session that groups the call. An explicit header that conflicts with a `previous_response_id` parent returns 409 `session_conflict` and the call is recorded as rejected. A heuristic parent in another session is dropped without rejecting the call. `auditevent.Request.SessionSource` records which source was used.

## Endpoint surface

```text
POST   /v1/chat/completions
POST   /v1/responses
POST   /v1/messages
POST   /v1/messages/count_tokens
POST   /v1/responses/input_tokens
GET    /v1/models
GET    /v1/models/:model
GET    /v1beta/models
GET    /v1beta/models/:model
POST   /v1beta/models/*action          generateContent, streamGenerateContent, countTokens
GET    /v1/responses                   WebSocket
GET    /v1/responses/:id
GET    /v1/responses/:id/input_items
POST   /v1/responses/:id/cancel
DELETE /v1/responses/:id
POST   /v1/embeddings
POST   /v1/moderations
```

The POST generation routes decode into the canonical model and run the full call path. The remaining endpoints are auxiliary operations, except the Responses WebSocket, which is another generation transport: model listing and retrieval in the OpenAI, Anthropic and Gemini shapes, native token counting, Responses retrieve/input_items/cancel/delete, and same-format embeddings and moderations pass-through. An auxiliary call is authenticated, rate-limited, redacted and audited, but it never creates a conversation, is never analysed and is not counted as a main call.

The public model selects the route; the forwarded body carries the selected target's upstream model name. The gateway never converts the wire format for these auxiliary calls. Counting falls back to a local estimate and sets `X-SafeAgent-Token-Estimate: true`; embeddings and moderations return `auxiliary_unsupported` when no channel declares them. `/v1/models` returns the names and aliases of every enabled route.

## Admission control

Every `/v1` request passes the same admission order: authenticate the agent key, decode the body, classify the call, acquire its rate and concurrency slots, open the call record, then screen it (redaction, request checkpoints, route resolution and quota). A request that fails before the call is opened is returned to the client but not persisted.

Rate and concurrency limits are process-local (`internal/platform/limit`) and are counted per project and per agent key. Each scope carries a requests-per-minute ceiling and a concurrency ceiling; zero disables that dimension, though the scope is still tracked. A new project starts at 120 requests per minute and 8 concurrent calls, and both are editable; an agent key is unlimited unless the operator sets limits. Exceeding either returns `429 rate_limited` before the call is opened, so a throttled request leaves no audit row. The limiter is memory only: a restart forgets the window, and two processes are prevented from sharing a database by the instance lease, not by the limiter.

The daily token quota is stored per project. Once a call has resolved its route, the gateway reserves an estimate of the call's tokens, settles the reservation against the reported usage at the end of the call, and returns `429 daily_quota_exceeded` when the day's used plus reserved total would exceed the project's quota. The reservation is deliberately rough: a quarter of the encoded request size plus the requested output ceiling, clamped so a client-supplied ceiling can neither make it negative nor overflow it. A project without a quota reserves nothing and counts usage only for reporting.

## Related

- [Providers](providers.md)
- [Model](model.md)
- [Audit](audit.md)
- [Development](development.md)
