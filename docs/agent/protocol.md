# Protocol layer

This document describes the canonical item model, the stream contract, the loss ledger and the conversion rules shared by the four protocol codecs.

## Canonical model

`internal/protocol` is standard-library only. The four formats (`openai_chat`, `openai_responses`, `anthropic_messages`, `google_gemini`) map onto one item model:

- `Request`: model, instructions, ordered items, tools, tool choice, parallel policy, response format, reasoning, limits, stop, stream options, provider-state fields (`store`, `background`, `conversation`, `include`, `truncation`, `max_tool_calls`, `prompt_cache_retention`), identity and raw overlay.
- `Item`: message, reasoning, tool call, tool result, server tool call, server tool result or native provider object. Items carry a stable ID, an ordinal and an optional role/status. A message owns ordered parts; a tool call owns `ToolCall`; a tool result owns `CallID`, `Name`, `IsError` and ordered content parts.
- `Part`: text, refusal, reasoning text/summary, redacted reasoning, tool arguments, media or native payload. Citations and prompt-cache markers attach to parts.
- `Response`: items, `Finish` (kind, raw value, stop sequence, incomplete reason), usage with a raw breakdown, failure, metadata and raw overlay.
- `Raw`: unknown fields at request, item, part, tool, citation, usage and response level, tagged with the source format. Same-format nodes replay them; every other target records `extras_dropped`.

For every protocol the codec captures the fields it does not model into `Raw`; the encoder re-emits them only towards the same format. Unknown content blocks become native items or native parts, so array positions survive.

## Item kinds and mapping

| Protocol | Message | Reasoning | Tool call | Tool result | Server tool |
| --- | --- | --- | --- | --- | --- |
| Chat | `messages[].content` | `reasoning_content` | `tool_calls` / `function_call` | `role:"tool"` | not native |
| Responses | `input`/`output` message items | reasoning items with summary/content/encrypted_content | `function_call` / `custom_tool_call` | `function_call_output` | provider items |
| Messages | `content` text/image/document | `thinking` / `redacted_thinking` | `tool_use` | `tool_result` | `server_tool_use`/results |
| Gemini | `parts[].text` | `text` with `thought` | `functionCall` | `functionResponse` | built-in tool calls |

Consecutive text and media parts share one message item; reasoning, tool and native blocks become their own items and split the surrounding message so the original order survives. Tool arguments stream through a `tool_arguments` part and are materialized on `Item.ToolCall.Arguments` when the item stops.

## Stream contract

`Event` kinds: `message_start`, `item_start`, `part_start`, `part_delta`, `part_stop`, `item_stop`, `message_delta`, `message_stop`, `error`, `ping`.

- Items and parts may be open concurrently; each start/stop pair is balanced and a stop snapshot's streamed content equals the concatenated deltas.
- Encoders that require sequential blocks (Messages, Gemini, Responses) rely on the gateway releasing held items in order.
- `message_delta` carries usage, finish and failure; `message_stop` is terminal.
- Protocol-native errors are produced by `StreamEncoder.Fail`: Chat writes an error object followed by `[DONE]`, Responses writes `response.failed` with the next sequence number, Messages writes an `error` event, Gemini writes an error JSON object. A Responses stream that fails before its first frame writes an `error` event with sequence 0.
- `Continuable.Resume` lets an encoder continue after the gateway forwarded some items verbatim.
- Response headers are committed as soon as the first upstream stream is accepted, followed by an SSE keepalive comment except for Gemini; the Gemini CLI treats an unconsumed comment as an incomplete JSON segment. Frames stay buffered until the first content delta, so an attempt that fails before content can still fail over. If every attempt fails after the headers, the client receives a protocol-native error frame and terminal frame instead of a second HTTP status.

### Clean EOF

A provider may close the connection without its terminal frame. The decoder synthesizes a terminal event only when the response is complete: at least one content item exists and every function tool call with arguments has valid JSON. Free-text custom tools are exempt. Otherwise `End` returns `ErrTruncated`. A synthesized end carries `LossCleanEOF`.

## Loss ledger

`Ledger` holds `Losses` and `Repairs`. Every loss has a path, a code, a class, the source/target formats and an item key:

- `fatal`: the target cannot represent core semantics. The gateway rejects before any upstream call. Media that cannot be carried and a required server tool with no equivalent are fatal.
- `degraded`: private or secondary data is dropped while the request continues. Foreign signatures, private provider fields, citation subfields, usage breakdowns and mapped tool kinds are degraded.

`RepairRequest` restores target invariants and records each change: empty items, unanswered calls, orphan results, duplicate results, reasoning order for Messages and an impossible tool choice.

## Same-format pass-through

The gateway decodes every request and response as a shadow so redaction and policy always see the content.

- Request bytes are forwarded unchanged (except the model, stream flag and the usage-in-stream option) only when source and target formats match, redaction produced no hits, the request carries no signature, no history was expanded and no repair was needed. Messages without a limit are not passed through because the gateway must apply its default; a Responses request is passed through only when it already asks for `reasoning.encrypted_content`.
- A streaming Chat, Messages or Gemini response is forwarded verbatim while it contains only plain text. Responses streams are always encoded canonically because their terminal snapshot lists every output item. When a tool call or an unsupported item appears, the gateway resumes a canonical encoder with the already-sent item/part counts and sequence number, so held tool calls are still judged and the stream keeps a valid terminal sequence. Non-streaming responses are always encoded canonically with raw overlays.

## Conversion policy

Important mappings:

- IDs: `resp_`, `msg_`, `chatcmpl-` prefixes are preserved only when they match the target; otherwise a target-native gateway-stable ID is synthesized. Gemini call IDs are synthesized with a per-call salt when absent and paired to `functionResponse` by name and order.
- Usage: `Input` is gross input including cache reads/writes. Cost prices uncached input separately. Chat audio and prediction buckets are kept in `Usage.Details`; cache, reasoning and Gemini thought counts stay in their typed fields, and cache-write TTL buckets are preserved where the target has them.
- Finish: every target maps the canonical kind and records `finish_mapped` when the exact meaning is lost.
- Output limit: an Anthropic Messages upstream requires one. An explicit client value wins, then the route target default, the channel default, the project default, and finally 8192. A defaulted limit is recorded; 4096 is never applied. Chat, Responses and Gemini requests that omit a limit are left to the provider.
- Headers: a protocol-aware allowlist forwards `anthropic-version`, `anthropic-beta`, `OpenAI-Beta`, `OpenAI-Organization`, `OpenAI-Project` and `x-goog-api-client`, plus channel-configured names. Credentials, cookies and hop-by-hop headers are never forwarded; a source-only protocol header is recorded.
- Reasoning signatures are sealed towards clients and verified on echo; a foreign signature is stripped with a loss and never passed off as another provider's. The envelope also carries the reasoning text, because some clients echo only the opaque signature; the gateway restores the text from the verified envelope. When a Gemini stream contains reasoning but no upstream signature, the gateway seals a SafeAgent envelope and records `gemini_dummy_signature`, because the Gemini CLI drops thought text that has no signature. Redacted reasoning survives only towards its originating protocol.
- When an assistant tool call has no reasoning at all, target reasoning is disabled and `reasoning_not_replayable` is recorded. Gemini CLI drops thought parts before its next turn, so this keeps the conversation valid without inventing reasoning.
- Responses keeps reasoning first in its assistant turn and the tool calls of that turn together, with the outputs after the call group. DeepSeek rejects an output placed between two calls when reasoning is replayed, while other providers accept the grouped form.
- Server tool calls are mapped by capability (`web_search`, `code_execution`); unmatched server tools are dropped with a loss unless a required tool choice names one, which is fatal. A mapped server tool call enters the same tool checkpoint as a client tool and is held until the verdict.
- Media: the capability descriptor decides whether each carrier (URL, inline data, file ID, tool-result media) is available. Core media that cannot be represented is rejected before the upstream.
- Responses state: `previous_response_id` is expanded from gateway history, including tools and instructions; `store=true`, `background` and `conversation` are rejected. `store` is always sent as false upstream.
- Multiple Chat choices (`n>1`) and Gemini candidates (`candidateCount>1`) are rejected with `unsupported_multi_choice`. Responses has no multiple-choice field.

## Tests

- `internal/protocol/format/format_test.go` checks the 4×4 request and response matrix in stream and JSON mode, plus defaults, capabilities, headers and usage buckets.
- Codec packages test terminal/truncation, tool aggregation, thinking signatures, server tools, media in tool results and multi-choice rejection.
- `internal/service/gateway/gateway_test.go` covers redaction restoration, tool holds, failover boundaries, same-format pass-through and native error frames.

## Related

- [Model](model.md)
- [Development](development.md)
- [Agent guide](README.md)
