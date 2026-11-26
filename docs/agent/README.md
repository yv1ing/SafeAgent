# Agent guide

SafeAgent is a self-hosted gateway between agent clients and model providers. It converts four wire formats, redacts and restores sensitive values, classifies and routes every request, enforces project policy while a call is in flight, records a signed audit chain and diagnoses finished main calls asynchronously.

Everything in this directory describes HEAD. The code is the authority: where a sentence and the code disagree, the code is right.

Current coverage: four-format conversion, classified calls and lanes, a stored supply graph and routing, four-checkpoint policy with reversible redaction, signed audit chains, asynchronous diagnosis and the console. [Roadmap](roadmap.md) lists exact coverage, limits and deliberate exclusions.

## Read by task

Read the path that matches the change, not the whole directory.

- **Any behavioural change**: [model](model.md) → [analysis](analysis.md) → [policy](policy.md) → [storage](storage.md) → [audit](audit.md).
- **Conversion, routing or provider work**: add [protocol](protocol.md) and [providers](providers.md).
- **Control plane, console or API work**: [providers](providers.md) → [storage](storage.md) → [development](development.md).
- **Build, test or acceptance work**: [development](development.md) and [testenv](testenv.md).
- **Context or onboarding**: this page, then [roadmap](roadmap.md) and [glossary](glossary.md).

## Document map

| Document | Owns |
| --- | --- |
| [model](model.md) | the protocol-neutral observation model, its wire adapter, item identity, shown values and value ids |
| [analysis](analysis.md) | call classification, detectors, engine, verdicts, checkpoints, diagnosis, trajectory, decision model |
| [policy](policy.md) | the ordered CEL rule document, its four phases, redaction rules and their lifecycle |
| [storage](storage.md) | tables, supply and routing rows, calls, cascades, lease, recovery, retention and secrets |
| [audit](audit.md) | event kinds, HMAC chain, timeline, verification and interrupted calls |
| [protocol](protocol.md) | canonical model, stream session, raw pass-through, loss ledger, admission control, endpoint surface |
| [providers](providers.md) | the provider seed catalog, channels, models, routes, aliases, targets and discovery |
| [development](development.md) | build, configuration, tests, contracts, conventions and extension points |
| [testenv](testenv.md) | the Docker acceptance lab |
| [roadmap](roadmap.md) | current coverage, limits and deliberate exclusions |
| [papers](papers.md) | which external concepts the code implements and where |
| [glossary](glossary.md) | one English term per concept, with the Chinese wording used by the human docs |

## Core invariants

- One process serves the gateway, the console, the health check and the diagnosis worker. PostgreSQL is the only durable store and the only coordination point.
- Audit events are the source of truth for a call. Every event is HMAC-chained and the call carries its own chain head and event count. The instance key comes from configuration and is never stored in the database.
- A call is recorded once it is admitted. Authentication and decode failures before admission are returned but not persisted.
- The lane, not the endpoint, decides behaviour: only main calls are diagnosed and counted in traffic metrics, side calls belong to the conversation and count cost, and auxiliary calls never create a conversation or enter diagnosis.
- The supply graph is stored. Built-in provider rows are seeded from code at startup and stay editable. The gateway resolves routes from stored rows on every call; a provider mutation reloads the console's in-memory registry. A dangling supply or routing reference logs a warning and degrades routing instead of refusing to start.
- Trajectory and redaction index are read-only projections rebuilt from calls, events, findings and hits. They are cached in process and never stored as a second source of truth.
- A project's policy version is pinned at admission. Diagnosis never changes online policy.
- The decision model is optional; without it, diagnosis is fully deterministic.
- The schema is a fresh baseline. Startup compares the stored fingerprint and refuses a mismatch or a table set without a fingerprint, so a schema change requires recreating the database. Stored audit events carry the audit schema version described in [audit](audit.md); startup refuses a database whose events disagree.
- Agent keys are stored as hashes and provide the caller identity. The console uses a session cookie, the `X-SafeAgent-Request` header and an Origin check for state-changing requests. `X-SafeAgent-Session` groups calls from one agent key; it is not an identity.

## Repository layout

```text
cmd/safeagent          server, schema, OpenAPI and audit verification
cmd/securitycheck      vulnerability report gate

internal/
  analysis/            findings, flows, axes, detectors, engine, policy and decision model
  app/                 composition root, lifecycle, bootstrap and architecture/document tests
  classify/            operation, purpose, purpose source, lane and outcome vocabulary
  llm/
    model/             canonical IR, events, ledger, signatures, assembler and journal
    codec/             codec contract, shared helpers and the four codecs
    provider/          provider and endpoint catalog, discovery parsing, built-in seeds
    stream/            streaming session: commit, hold, restore, raw, keepalive
  observation/         protocol-neutral trace model and its only wire adapter
  pipeline/            online call path: admission, redaction, checkpoints, routing, streaming, seal
  platform/            infrastructure without domain knowledge
  redact/              reversible redaction engine, detectors and value ids
  service/             access, console, control and diagnosis use cases
  storage/             audit contract, entities and PostgreSQL repository
  testkit/             in-memory fixtures for tests only
  transport/           console HTTP API and gateway HTTP/WebSocket routes

web/                   React console
testenv/               Docker acceptance lab
```

## Layering

The architecture tests in `internal/app` enforce these rules. A change that violates one is a design change, not a formatting problem.

- `internal/llm/model` depends only on the standard library. `internal/llm/codec`, `internal/llm/provider` and `internal/llm/stream` sit below the pipeline; codec and provider do not depend on each other or on the stream engine.
- `internal/observation` depends only on the standard library. `internal/observation/adapter` is the single bridge from the wire model to it; it may import `internal/llm/model` and `internal/redact`, and never a codec, the provider catalog, the stream engine, storage, a service, the pipeline or transport.
- `internal/analysis`, `internal/redact` and `internal/platform/*` never import storage, pipeline, service or transport. `internal/analysis` may import only `platform/netguard` from the platform group and never depends on `llm/*`.
- `internal/pipeline` imports the protocol stack and storage. It does not import transport, the application root, gin or bun.
- `internal/service/access` is the shared vocabulary below the use cases. `service/console`, `service/control` and `service/diagnosis` do not import each other, and no service imports transport.
- `internal/storage/repository` imports entities, the audit contract, `internal/classify` and `internal/platform/secure` only. `internal/storage/entity` is a pure type package that imports only `internal/classify`.
- `internal/transport/console` maps the console API onto the three service packages and never imports the wire model, a codec or the stream engine. `internal/transport/gateway` adapts the pipeline to the `/v1` and `/v1beta` surface.
- `internal/classify` is a leaf vocabulary package. `internal/testkit` is test-only. `internal/app` is the only package that wires transport, services, pipeline and storage together.
- Repository methods are verbs, domain types are nouns, and a layer never imports the layer above it.

## Commands

`go run ./cmd/safeagent -config config.toml serve` runs the service. The same binary provides `schema`, `openapi` and `verify-audit`. The complete workflow is in [development](development.md); the acceptance lab is in [testenv](testenv.md).

## Related

- [Human architecture guide](../human/architecture.md)
- [Getting started](../human/getting-started.md)
- [Project README](../../README.md)
