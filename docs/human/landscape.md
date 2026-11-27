# Landscape

[中文](landscape_zh.md)

AxonHub, magpie and Sub2API also place a gateway between agents and model providers, but each is optimized for a different goal: provider coverage, local agent configuration, or subscription quota. A comparison with these projects clarifies SafeAgent's scope and its deliberate exclusions.

## How this comparison works

This comparison is qualitative. It draws on the three projects' public documentation and READMEs, not on running benchmarks, and describes their general approach rather than one release. These are neighbouring designs, not a ranking. Features, licenses and internal design change quickly, so consult the project directly before relying on a detail here.

## SafeAgent

SafeAgent is a self-hosted security gateway for agent behavior. It accepts Chat Completions, Responses, Anthropic Messages and Gemini, converts them through one canonical model, masks sensitive values on the way out and restores them on the way back, and records every trajectory in a signed event chain; calls are projections of that chain.

Its analysis unit is one trajectory, and a call is an interval inside it. The control plane separates a global supply graph (providers, channels, models) from per-project routing (routes, targets, aliases). The gateway, the console and the evidence tools read one event log, so an operator's view matches what was enforced at the time.

## AxonHub

AxonHub is a multi-provider AI gateway and development platform: one interface for many models, with routing, load balancing, cost tracking, RBAC and request tracing. Its provider catalog is large and includes vendor variants and OAuth channels.

Its analysis unit is a trace or thread with request and execution layers. The control plane splits channels and models from association rules, so a model can be connected to several channels and routing policy is edited separately. Requests pass through a unified intermediate representation shaped like OpenAI Chat Completions, with non-OpenAI semantics carried in extra fields; same-format traffic can pass through raw. It manages its schema through an ORM and exposes configuration changes through an admin UI.

## magpie

magpie is a local gateway and configuration coordinator for several agent CLIs. It gives tools such as Claude Code and Codex one address, groups upstream accounts, and writes the corresponding client configuration for them.

Its analysis unit is one gateway call, grouped by client session. The control plane is organized around providers, accounts, groups and routes. Protocol conversion covers the four formats, and rewriting agent configuration files is central to the product. It classifies calls by client-declared kind, normalized purpose and whether the caller is an agent or the gateway itself; masked values receive a deterministic placeholder that serves as their identity. Its state is stored in local files rather than a shared database.

## Sub2API

Sub2API is a subscription-quota distribution and resale platform. It pools upstream accounts and manages quota, billing, rate limits and scheduling per user, group and model.

Its analysis unit is the individual request; usage and operational errors are accounted for separately. The control plane is organized around accounts, groups, channels and composite groups. Protocol conversion is mostly pairwise, with Responses used as a hub for some paths. It keeps durable state in a relational database with a cache layer.

## Comparison

| Dimension | SafeAgent | AxonHub | magpie | Sub2API |
| --- | --- | --- | --- | --- |
| Positioning | agent behavior security gateway | multi-provider AI gateway and platform | local multi-agent CLI gateway and config coordinator | subscription quota, billing and relay |
| Analysis unit | one trajectory; a call is an interval | trace or thread, request and execution layers | one gateway call, grouped by client session | one request, with usage and error ledgers |
| Control plane | provider → channel → model supply graph, plus project routes and aliases | channels and models, plus association rules | providers, accounts, groups and routes | accounts, groups, channels and composite groups |
| Protocol strategy | canonical model for four formats, same-format raw pass-through | unified OpenAI-shaped representation, pairwise transforms | four-format conversion, rewrites agent configuration | pairwise conversion, Responses as a hub |
| Storage | PostgreSQL, configuration and signed trajectory ledger | relational schema managed through an ORM | local files | relational database plus a cache layer |

## Adopted ideas

- **From AxonHub:** the separation between provider configuration and routing policy, a visible history of requests and attempts, and a trajectory view over typed event nodes. The OpenAI-shaped representation and its client-specific handling remain AxonHub's choices.
- **From magpie:** call classification by endpoint, declared purpose and lane, and the use of a deterministic placeholder as a value identity so repeated masking collapses into one entry.
- **From Sub2API:** a soft-delete lifecycle with an impact check, and the practice of accounting for usage separately from errors.

SafeAgent's contribution is the combination: protocol conversion, online policy and audit on one call path, a narrower provider scope than a general gateway, and enforcement at the tool boundary rather than only inspecting traffic afterwards.

## Further reading

- [Architecture](architecture.md)
- [Protocol conversion](protocol-conversion.md)
- [Agent safety](agent-safety.md)
- [Verification](verification.md)
