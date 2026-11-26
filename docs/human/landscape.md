# Landscape

[中文](landscape_zh.md)

Three open projects solve neighbouring problems. AxonHub, magpie and Sub2API each put a gateway between agents and model providers, but they optimize for different things: provider coverage, local agent configuration, and subscription quota. Placing SafeAgent beside them explains what it is and what it deliberately is not.

## Method

This comparison is qualitative. It draws on the three projects' public documentation and READMEs, not on running benchmarks, and it describes their general approach rather than the details of one release. These are neighbouring designs, not a ranking. Features, licenses and internal design change quickly, so check a project directly before relying on a detail here.

## SafeAgent

SafeAgent is a self-hosted security gateway for agent behavior. It terminates Chat Completions, Responses, Anthropic Messages and Gemini, converts them through one canonical model, masks sensitive values in both directions, and records every admitted call in a signed audit chain.

Its analysis unit is one call, and the chain of calls forms a conversation. The control plane separates a global supply graph (providers, channels, models) from per-project routing policy (routes, targets, aliases). Its storage is a PostgreSQL database with a single schema baseline and an HMAC-chained event record. One Go process serves the gateway, the console and the diagnosis worker.

## AxonHub

AxonHub is a multi-provider AI gateway and development platform: one interface for many models, with routing, load balancing, cost tracking, RBAC and request tracing. Its provider catalog is large and includes vendor variants and OAuth channels.

Its analysis unit is a trace or thread with request and execution layers. The control plane splits channels and models from association rules, so a model can be connected to several channels and the routing policy is edited separately. Requests pass through a unified intermediate representation shaped like OpenAI Chat Completions, with non-OpenAI semantics carried in extra fields; same-format traffic can pass through raw. It manages its relational schema through an ORM with soft deletes and exposes configuration changes through an admin UI.

## magpie

magpie is a local gateway and configuration coordinator for several agent CLIs. It gives tools such as Claude Code and Codex one address, groups upstream accounts, and writes the corresponding client configuration for them.

Its analysis unit is one gateway call, grouped by the client session. The control plane is organised around providers, accounts, groups and routes. Protocol conversion covers the four formats, and rewriting agent configuration files is central to the product. It classifies calls by client-declared kind, normalized purpose and whether the caller is an agent or the gateway itself; masked values get a deterministic placeholder that serves as their identity. Its state lives in local files rather than a shared database.

## Sub2API

Sub2API is a subscription-quota distribution and resale platform. It pools upstream accounts and manages quota, billing, rate limits and scheduling per user, group and model.

Its analysis unit is the individual request, with usage and operational errors kept in two separate accounts. The control plane is organised around accounts, groups, channels and composite groups. Protocol conversion is mostly pairwise, with Responses used as a hub for some paths. It keeps durable state in a relational database with a cache layer beside it.

## Comparison

| Dimension | SafeAgent | AxonHub | magpie | Sub2API |
| --- | --- | --- | --- | --- |
| Positioning | agent behavior security gateway | multi-provider AI gateway and platform | local multi-agent CLI gateway and config coordinator | subscription quota, billing and relay |
| Analysis unit | one call; the call chain forms a conversation | trace or thread, request and execution layers | one gateway call, grouped by client session | one request, with usage and error ledgers |
| Control plane | provider → channel → model supply graph, plus project routes and aliases | channels and models, plus association rules | providers, accounts, groups and routes | accounts, groups, channels and composite groups |
| Protocol strategy | canonical model for four formats, same-format raw pass-through | unified OpenAI-shaped representation, pairwise transforms | four-format conversion, rewrites agent configuration | pairwise conversion, Responses as a hub |
| Storage | PostgreSQL, single schema baseline, signed audit chain | relational schema managed through an ORM | local files | relational database plus a cache layer |

## What SafeAgent takes

- **From AxonHub**: the separation between provider configuration and routing policy, visible request and attempt lineage, and a trajectory rebuilt from typed nodes into several views. The OpenAI-shaped representation and its client-specific handling remain AxonHub's choices.
- **From magpie**: call classification by endpoint, declared purpose and lane, and the use of a deterministic placeholder as a value identity so repeated masking collapses into one entry.
- **From Sub2API**: soft-delete lifecycle with an impact check, and the habit of keeping usage accounting apart from error accounting.

The combination is the contribution. SafeAgent keeps protocol conversion, online policy and audit on one call path, accepts a narrower provider scope than a general gateway, and enforces at the tool boundary rather than only inspecting traffic after the fact.

## Related

- [Architecture](architecture.md)
- [Protocol conversion](protocol-conversion.md)
- [Agent safety](agent-safety.md)
- [Verification](verification.md)
