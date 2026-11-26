# Providers

The control plane is stored, not compiled in. `internal/service/control` owns the provider, channel, model and route graph and its invariants; `internal/transport/console` maps HTTP input and output. The code under `internal/llm/provider/builtin` is the seed catalog, not a hard-coded runtime list: startup writes each built-in provider row and the gateway reads provider rows from the database.

## Graph

- **Provider** — one protocol and capability catalog: source (`builtin` or `custom`), label, an endpoint catalog keyed by wire format, an enabled flag and a version. A built-in row is editable; `POST /console/api/providers/{provider_id}/reset` restores the shipped label and endpoints. A built-in provider cannot be deleted. A custom provider can be created, edited or soft-deleted.
- **Channel** — one upstream connection: provider id, name, optional base URL, static headers, forwarded header names, a default output limit, request and first-byte timeouts, the private-network flag, an enabled flag and a version. A channel does not store a format list; its formats are the provider endpoints. Keys are added separately, sealed with the instance key, and only the last four characters are ever returned.
- **Model** — one catalog entry: provider, upstream name, optional display name, kind (`generation`, `embedding` or `moderation`), four prices, currency, enabled flag and version. Formats are derived from the provider endpoints and the kind.
- **Route** — per-project public model name, enabled flag and version. A route carries targets and aliases.
- **Target** — channel, model, optional pinned format, priority, weight, max output tokens, enabled flag and disabled reason. One route serves one model kind; every target must use a channel and a model of the same provider.
- **Alias** — an additional project-facing model name for one route. A rename writes the old route name with `source=renamed`; operators may add `manual` aliases.

Providers, channels, models and routes use soft deletion with partial unique indexes over live rows, so a deleted name can be reused. Targets and aliases are replaced as part of the route that owns them. A mutation that names a version it no longer holds returns `version_conflict` rather than overwriting another edit.

## Types

A provider is one `ProviderSpec`: an id, a label, a `custom` flag and the endpoint catalog keyed by wire format. Each `EndpointSpec` declares:

| Field | Meaning |
| --- | --- |
| `Format` | the wire format the endpoint speaks |
| `BaseURL` | the default host for this format; absolute HTTP(S), no credentials, query or fragment |
| `Path` | the request path, with `{model}` and `{action}` substituted |
| `CountTokensPath` | the native token-counting path, when the endpoint has one |
| `Auth` | `bearer`, `x-api-key`, `x-goog-api-key` or `query` |
| `StaticHeaders` | headers the endpoint always adds |
| `Capability` | what the wire format can carry |
| `Compat` | behavior the format does not imply |
| `Discovery` | the model-listing path and the OpenAI, Anthropic or Gemini response shape |
| `Auxiliary` | auxiliary endpoints such as embeddings, moderations and token counting |
| `Transport` | `auto`, `stream` or `json` |
| `Aliases` | upstream model aliases applied at call time |

The endpoint catalog is stored as JSON on the provider row, so the console can round-trip an edited provider exactly. A declared endpoint base URL must be an absolute HTTP(S) URL without credentials, query or fragment, and a declared path must begin with `/`. The endpoint base URL is the default host. A channel base URL, when set, replaces it for every format the channel serves, which is how a channel reaches a proxy or a self-hosted gateway; an empty channel base URL keeps the provider host, so multi-host providers still work through one channel.

## Built-in seeds

| Provider | Formats | Endpoints | Auth | Discovery |
| --- | --- | --- | --- | --- |
| Anthropic | Messages | `/v1/messages`, `/v1/messages/count_tokens` | `x-api-key` + `anthropic-version` | `/v1/models` |
| OpenAI | Chat, Responses | `/v1/chat/completions`, `/v1/responses`, `/v1/responses/input_tokens`; embeddings and moderations | Bearer | `/v1/models` |
| Google | Gemini | `/v1beta/models/{model}:{action}`, `:countTokens` | `x-goog-api-key` | `/v1beta/models` |
| DeepSeek | Chat, Responses, Messages | `https://api.deepseek.com`; Messages under `/anthropic` | Bearer / `x-api-key` | `/models` |
| Z.AI | Chat, Responses, Messages | PAAS v4, `/api/v1`, `/api/anthropic`; embeddings on PAAS | Bearer / `x-api-key` | none |
| Qwen | Chat, Responses, Messages | compatible-mode, apps/anthropic; embeddings | Bearer / `x-api-key` | `/models` |
| Custom | Chat | `/chat/completions` (channel supplies the host) | Bearer | `/models` |

Compatibility highlights:

- Anthropic, DeepSeek Messages, Z.AI Messages and Qwen Messages require a max output token field.
- DeepSeek Messages ignores top-k, budget tokens, service tier and custom tool types, and maps a `claude-` upstream name prefix to `deepseek-chat`.
- Qwen Chat and Responses may report a failed request with HTTP 200.
- Google substitutes the model and the streaming or plain action into the path.
- Custom declares a conservative Chat baseline (text, image input and function tools) and no native token counting; an operator-supplied provider can declare any endpoint catalog.

`Capability` describes what the wire format can carry (media carriers, tool kinds, reasoning, citations, cache control, top-k, stop sequences, streaming usage, logprobs, refusal, multi-choice, native counting, streamed tool arguments and server tools). `Compat` carries behavior the format does not imply (required output limit, ignored fields, rejected reasoning or tools, HTTP-200 failure bodies). Codecs read only `Peer.Profile`, never a vendor name.

## Resolution and selection

For generation and auxiliary calls the pipeline resolves the public model in this order:

1. `X-SafeAgent-Model`, when present and resolvable in the caller's project (`resolution=explicit`);
2. the exact route name (`resolution=exact`);
3. a route alias (`resolution=alias`);
4. for side and auxiliary lanes only, the project's `default_route_id` (`resolution=fallback`).

A main call whose model is not configured fails with `model_not_found`; it never silently uses the project default. `/v1/models` returns route names and aliases. The routing event records which rule matched, so a fallback is visible in the call timeline.

Each target contributes at most one candidate: the target's pinned format when set, otherwise the client format when the provider exposes it for the model kind, otherwise the first usable format in provider order. A failed candidate moves to the next target; the same target is not retried in another format.

The selector orders candidates by ascending priority, applies weighted random choice inside one priority, and prefers candidates already in the client format. A sticky session key keeps a conversation on the channel that served it when that candidate is still usable. Circuit breakers and per-key cooldown remove unhealthy candidates; a key rejected for authentication or throttled is cooled so the next attempt uses another key. The total attempts per call are capped by `max_attempts` (default 3).

Only transient upstream failures, authentication failures, rate limits and channel/format conversion failures fail over. A policy block, an invalid request, a rejected request and a non-retryable upstream error are final.

## Lifecycle

- Disabling a channel or model keeps every target in place; the runtime skips it and the configuration report marks the affected route degraded.
- Soft-deleting a channel or model disables the route targets that reference it in the same transaction and advances the version of every affected route, so a stale route edit cannot write the old target set back. With `detach_targets=true` the targets are removed instead. The route itself stays, so history and the route name remain readable.
- Restoring a channel or model clears the soft deletion, re-enables only the targets it disabled itself and advances the same route versions.
- A channel or model still referenced by routes requires `acknowledge=true` on delete; without it the API returns `impact_requires_acknowledge`. `GET /console/api/configuration/impact` (with `channel_id` or `model_id`) lists the affected routes and projects before the operator confirms.
- Deleting a route removes its aliases and clears a project default that pointed at it.
- Renaming a route writes the old name as a `renamed` alias, so existing clients keep resolving.

The control-plane service serializes graph edits with a process mutex. A multi-row edit writes through one repository transaction, and a provider mutation reloads the in-memory registry used by the console after it commits. A reload failure is logged and reported through the configuration health report; it does not turn the committed edit into an error, because a retry would collide with the edit that already succeeded. The gateway reads each target's provider row on every call, so a stored edit is visible without a restart.

## Configuration health

`control.Health` checks the stored graph and returns warnings instead of refusing to run: an unreadable provider endpoint catalog, a channel or model whose provider is missing, a model whose provider exposes no endpoint for its kind, and a route target that references a disabled or deleted channel/model. Startup logs each warning; `GET /console/api/configuration/health` returns the counts and the same warnings to the console. A process-level prerequisite (configuration file, database, instance key, schema fingerprint) or an unreadable stored policy is fatal.

## Discovery and connect

Discovery is initiated for one enabled channel format. It calls that format's listing endpoint with the channel's first enabled key and configured headers through the pinned network client and parses the OpenAI, Anthropic or Gemini shape. Discovery never sends a billable generation request.

The returned names are candidates; nothing is imported without confirmation. Importing creates the missing generation models for the provider, and an existing model is left untouched. A name that already belongs to an embedding or moderation model is skipped and reported.

`POST /console/api/configuration/connect` performs the one-shot flow in one transaction: an optional new custom provider, one channel, up to eight sealed keys and a list of catalog models. The channel keeps the `enabled` flag the request carries, so an omitted flag creates a disabled channel instead of quietly enabling one. A failed connection leaves no half-configured provider behind. The console's channel editor uses the provider catalog endpoint to render the available formats and models.

## Field rejection

When an upstream rejects a request because of an optional field, the pipeline identifies the field from the error body, applies the rejection to the endpoint in memory for the rest of the call, records a `capability_field_removed` loss, writes the override to `channel_capability_overrides` and retries the same key once. A second rejection is returned unchanged. Supported fields are `top_k`, `budget_tokens`, `service_tier`, `stop`, `logprobs`, `thinking`, `tools` and `custom_tool`. The channel API exposes stored overrides read-only.

## Limits

| Subject                  | Limit                                                       |
| ------------------------ | ----------------------------------------------------------- |
| targets per route        | 32                                                          |
| weight                   | 0..1000                                                     |
| aliases per route        | 16                                                          |
| batch model import       | 200                                                         |
| keys per connect request | 8                                                           |
| configured headers       | 32, value at most 4096 bytes                                |
| request timeout          | 600 s default, 3600 s maximum                               |
| first-byte timeout       | 60 s default, 600 s maximum                                 |
| output limit             | 1,048,576 tokens                                            |
| pricing                  | per-million-token integer micros, bounded to avoid overflow |
| currencies               | USD, CNY, EUR, JPY, GBP                                     |

## Related

- [Protocol layer](protocol.md)
- [Storage](storage.md)
- [Analysis](analysis.md)
- [Development](development.md)
