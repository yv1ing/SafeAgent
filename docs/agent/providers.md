# Providers

The provider catalog is code, not configuration. Each built-in provider is one file under `internal/llm/provider/builtin`; adding a provider means adding that file and one entry in `builtin.All()`. A channel stores the provider id, an ordered list of enabled formats with optional base URL and path overrides per format, the catalog model ids it exposes, keys, headers and machine-written capability overrides.

## Types

```go
type ProviderSpec struct {
    ID        string
    Label     string
    Custom    bool
    Endpoints map[model.Format]EndpointSpec
}

type EndpointSpec struct {
    Format          model.Format
    BaseURL         string
    Path            string        // {model} and {action} are substituted
    CountTokensPath string
    Auth            AuthStyle     // bearer, x-api-key, x-goog-api-key, query
    StaticHeaders   map[string]string
    Capability      model.Capability
    Compat          model.Compat
    Discovery       DiscoverySpec // path and OpenAI/Anthropic/Gemini shape
    Auxiliary       []AuxiliarySpec
    Transport       TransportMode // auto, stream, json
    Aliases         []ModelAlias
}
```

`Capability` describes what the wire format of this endpoint can carry: media carriers, tool kinds, reasoning support, citations, cache control, top-k, stop sequences, streaming usage, logprobs, refusal, multi-choice, native counting, streamed tool arguments and server tools. `Compat` describes behavior the format does not imply: required output limit, ignored fields, rejected reasoning or tools, and HTTP-200 failure bodies. Codecs read only `Peer.Profile`, never a vendor name.

## Matrix

| Provider | Formats | Endpoints | Auth | Discovery |
| --- | --- | --- | --- | --- |
| Anthropic | Messages | `/v1/messages`, `/v1/messages/count_tokens` | `x-api-key` + `anthropic-version` | `/v1/models` |
| OpenAI | Chat, Responses | `/v1/chat/completions`, `/v1/responses`, `/v1/responses/input_tokens`; embeddings, moderations | Bearer | `/v1/models` |
| Google | Gemini | `/v1beta/models/{model}:{action}`, `:countTokens` | `x-goog-api-key` | `/v1beta/models` |
| DeepSeek | Chat, Responses, Messages | OpenAI-compatible base; Messages under `/anthropic` | Bearer / `x-api-key` | `/models` |
| Z.AI | Chat, Responses, Messages | PAAS v4, `/api/v1`, `/api/anthropic`; embeddings on PAAS | Bearer / `x-api-key` | none |
| Qwen | Chat, Responses, Messages | compatible-mode, apps/anthropic; embeddings | Bearer / `x-api-key` | compatible models |
| Custom | Chat | operator base URL + `/chat/completions` | Bearer | optional `/models` |

Compatibility highlights:

- Anthropic, DeepSeek, Z.AI and Qwen Messages: `RequireMaxTokens`.
- DeepSeek Messages ignores top-k, budget tokens, service tier and custom tool types, with `claude-*` aliased to `deepseek-chat`.
- Google substitutes the model and the streaming or plain action into the path.
- Qwen: `HTTP200FailedStatus`.
- Custom: standard Chat Completions capabilities; the operator must supply a base URL.

## Channel overrides

A channel enables one or more formats the provider offers, in fallback order. Each endpoint may override the base URL and path; empty values keep the provider default, and a custom provider requires a base URL. The channel also lists the catalog model ids it exposes. A model remains global: the same model can be enabled on several channels, and a channel can enable models across its formats.

A route target chooses one channel and one catalog model. At resolution time the effective formats are the channel endpoint order filtered by the model formats. The client format wins when it is present; otherwise the first enabled format is used. One target produces one candidate for one format, so a failed target moves to the next target rather than retrying another format in place.

Capability overrides are written by the gateway after an upstream rejects a field and are read-only in the console. They are keyed by channel, model, format and field. Supported fields are `top_k`, `budget_tokens`, `service_tier`, `stop`, `logprobs`, `thinking`, `tools` and `custom_tool`.

## Discovery

Discovery is initiated for one enabled channel format. It calls that format's listing endpoint with the channel's first enabled key and configured headers through the pinned dialer and parses the OpenAI, Anthropic or Gemini shape. Discovery never sends a billable generation request.

The returned names are candidates; nothing is imported without confirmation. Importing creates a missing catalog model with the discovered format, adds the format to an existing generation model, and enables the model on the channel. A name that already belongs to an embedding or moderation model is skipped and reported.

## Field rejection

When an upstream rejects a request because of an optional field, the pipeline identifies the field from the error body, folds it into the endpoint's in-memory compatibility set for that channel model format, records a `capability_field_removed` loss, writes the override to the channel and retries the same key once. A second rejection is returned unchanged.

## Related

- [Protocol layer](protocol.md)
- [Development](development.md)
- [Agent guide](README.md)
