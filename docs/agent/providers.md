# Providers and supply

## Graph

The supply graph is global; routing is per project:

```text
provider -> channel -> model
                 \-> route target <- model route <- route alias
```

- `providers` describe endpoint catalogs per wire format.
- `channels` are one provider deployment with a base URL, headers, limits and keys.
- `models` are catalog entries with an upstream name, kind, prices and currency.
- `model_routes`, `route_aliases` and `route_targets` are the project-facing view.

A route target selects one channel and one model. A channel key is sealed with the instance key; a key is decrypted only in the routing path.

## Selection

Resolution order is explicit `X-SafeAgent-Model`, exact route name, alias, and the project default route for side or auxiliary lanes only. A main call does not silently fall back. Targets are ordered by priority, weight, client-format preference and sticky session; a candidate is admitted by its breaker immediately before use. Only transient/auth/rate-limit/conversion failures fail over.

## Lifecycle

Providers, channels and models are soft-deleted. Deleting one returns an impact preview: routes and projects that would lose a target. Restoring a row is explicit. A route target referencing a soft-deleted row is skipped, not rewritten.

## Health and discovery

`configuration/health` reports:

- `provider_endpoints_unreadable` as a **fatal startup error** when a stored provider row cannot be decoded; the service does not start with a broken catalog;
- missing channel keys or duplicate names as warnings;
- routes with no usable target as warnings;
- discovery paths declared by the provider catalog.

A channel with an endpoint discovery path can list models. Connecting a provider creates the channel, keys and catalog models in one operation. Authentication headers are masked in reads.

## Field rejection

When an upstream rejects one optional field, the gateway records a capability override for that channel/model/format and retries once without the field. The override is stored, so later calls do not repeat the failure. The removed field is recorded as a conversion loss.

## Limits

- 32 upstream connections per channel client; 256 channel clients cached.
- first-byte and idle timeouts are enforced by the transport.
- private network targets require `allow_private_network`.
- a provider catalog that cannot be decoded is fatal at startup.

## Related

- [Protocol](protocol.md)
- [Storage](storage.md)
- [Development](development.md)
