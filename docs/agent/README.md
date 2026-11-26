# Agent guide

SafeAgent is a self-hosted gateway between agent clients and model providers. It converts four wire formats, redacts and restores sensitive values, classifies and routes every request, enforces project policy while a call is in flight, records a signed audit chain and diagnoses finished main calls asynchronously.

Everything in this directory describes HEAD. The code is the authority; where a sentence and the code disagree, the code is right.

## Reading paths

Read the path that matches the change, not the whole directory.

- **Any behavioural change**: [model](model.md) → [analysis](analysis.md) → [policy](policy.md) → [storage](storage.md) → [audit](audit.md).
- **Conversion, routing or provider work**: add [protocol](protocol.md) and [providers](providers.md).
- **Control plane, console or API work**: [providers](providers.md) → [storage](storage.md) → [development](development.md).
- **Build, test or acceptance work**: [development](development.md) and [testenv](testenv.md).
- **Context or onboarding**: this page, then [glossary](glossary.md).

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
| [roadmap](roadmap.md) | what the current release covers and where its boundaries are |
| [papers](papers.md) | which external concepts the code implements and where |
| [glossary](glossary.md) | one English term per concept, with the Chinese wording used by the human docs |

## Repository layout

```text
cmd/safeagent          server, schema, OpenAPI and audit verification
cmd/securitycheck      vulnerability report gate

internal/
  analysis/            findings, flows, axes, detectors, engine, policy and decision model
    builtin/           deterministic detectors
    decision/          decision-model boundary and adapters
    policy/            typed CEL rule compiler
  app/                 composition root, lifecycle, bootstrap and architecture/document tests
  classify/            operation, purpose, purpose source, lane and outcome vocabulary
  llm/
    model/             canonical IR, events, ledger, signatures, assembler and journal
    codec/             codec contract, shared helpers and the four codecs
    provider/          provider and endpoint catalog, discovery parsing
      builtin/         one file per built-in provider seed
    stream/            the streaming session: commit, hold, restore, raw, keepalive
  observation/         protocol-neutral trace model, values, targets and locators
    adapter/           the only wire-model to observation adapter
  pipeline/            online call path: admission, redaction, checkpoints, routing, attempts, streaming, seal
  platform/            infrastructure without domain knowledge
  redact/              reversible redaction engine, detectors and value ids
  service/
    access/            shared actor, failure and input-text vocabulary
    console/           management use cases that are not the supply graph
    control/           supply-graph use cases: providers, channels, models, routes, discovery, connect, health
    diagnosis/         trace rebuild, detectors, decision model, verdict and analysis records
  storage/             auditevent contract, entities and PostgreSQL repository
  testkit/             in-memory fixtures for tests only
  transport/
    console/           console HTTP API
    gateway/           gateway HTTP and WebSocket routes
web/                   React console
testenv/               Docker acceptance lab
```

`platform` contains utilities that know nothing about agents, calls or findings. The domain libraries sit beside the layers: `analysis`, `classify`, `observation` with its `adapter`, `redact`, `llm/*` and `pipeline`.

`pipeline` is one package. `pipeline.go` holds `Pipeline`, `Options` and `New`; `admission.go`, `request.go`, `response.go`, `upstream.go`, `attempt.go` and `finish.go` own the call lifecycle; `recorder.go`, `hits.go` and `outcome.go` own the audit projection; `routing.go`, `models.go`, `history.go` and `session_header.go` own resolution and linking; `adapt.go` and `raw.go` own format adaptation and the raw fast path; `cache.go`, `policy.go` and `auth.go` hold compiled policy, redaction and identity; `auxiliary.go`, `count_tokens.go`, `passthrough.go` and `responses_lifecycle.go` own the auxiliary endpoints.

## Dependency rules

The architecture tests in `internal/app` enforce the boundaries below. A change that violates one is a design change, not a formatting problem.

- `internal/llm/model` depends only on the standard library.
- `internal/llm/codec`, `internal/llm/provider` and `internal/llm/stream` sit below the pipeline. They may use the model; codec and provider may not depend on each other or on the stream engine.
- `internal/observation` is the protocol-neutral model and depends only on the standard library. `internal/observation/adapter` is the single bridge from the wire model to it; the adapter may import `internal/llm/model` and `internal/redact`, and never a codec, the provider catalog, the stream engine, storage, a service, the pipeline or transport.
- `internal/analysis`, `internal/redact` and `internal/platform/*` are domain libraries. They never import storage, pipeline, service or transport. `internal/analysis` may import only `platform/netguard` from the platform group, because it inspects operator-supplied URLs, and it never depends on `llm/*`; the adapter translates wire data before analysis sees it.
- `internal/pipeline` imports the protocol stack and storage. It does not import transport, the application root, gin or bun.
- `internal/service/access` is the shared vocabulary below the use cases: the actor, the expected failure kinds, the repository-error translation and the input-text invariants. It imports storage but no service, pipeline or transport.
- `internal/service/console` owns the management use cases that are not the supply graph: accounts, projects, keys, rules, policies, calls, stats, trajectory, redaction, operations, monitor and the analysis read views. It does not import `service/control`.
- `internal/service/control` owns the provider, channel, model and route graph and imports `service/access`; it never imports `service/console`.
- `internal/service/diagnosis` owns the trace rebuild, the detectors, the decision model, the verdict and the analysis records. It imports `service/access` and neither `service/console` nor `service/control`.
- `internal/service` sits between transport and storage: it uses storage and the domain libraries and never imports transport, the application root, gin or bun.
- `internal/storage/repository` imports entities, the audit contract, `internal/classify` and `internal/platform/secure` only; it never imports a service, the pipeline, transport, the application root, the other domain libraries, testkit or gin.
- `internal/storage/entity` is a pure type package that imports only `internal/classify`.
- `internal/transport/console` maps the console API onto the three service packages. Neither it nor `internal/service/console` imports the wire model, a codec or the stream engine; `/v1` data reaches the console only through the observation adapter.
- `internal/classify` is a leaf vocabulary package: the pipeline, storage entities, repository, console services, diagnosis and the gateway transport import it, and it imports nothing from the module.
- `internal/transport/gateway` adapts the pipeline to the `/v1` and `/v1beta` HTTP and WebSocket surface; it imports the pipeline, the protocol stack and `classify`.
- `internal/testkit` holds in-memory fixtures for tests; production code never imports it.
- `internal/app` is the only package that wires transport, services, pipeline and storage together.
- Repository methods are verbs, domain types are nouns, and a layer never imports the layer above it.

## Operating model

- One process serves the gateway, the console, the health check and the diagnosis worker. PostgreSQL is the only durable store and the only coordination point.
- Audit events are the source of truth. Every event is HMAC-chained and a call carries its own chain head and event count. The instance key comes from configuration and is never stored in the database.
- A call is recorded once it is admitted. Authentication and decode failures before admission are returned but not persisted.
- Every call has a classification: operation, purpose, purpose source, lane and outcome. The lane decides whether it joins a conversation, is diagnosed and is counted in traffic metrics. Only main calls are diagnosed and counted; side calls belong to the conversation but are not analysed; auxiliary calls are protocol traffic with no conversation and no analysis.
- The provider, channel, model and route graph is stored, not compiled in. Built-in provider rows are seeded from code at startup and stay editable; a provider mutation reloads the in-memory catalog while channel, model and route rows are read per call. A dangling supply or routing reference logs a warning and degrades routing instead of refusing to start.
- Trajectory and redaction index are read-only projections rebuilt from calls, events, findings and hits. They are cached in process and never stored as a second source of truth.
- A project's policy version is pinned at admission. Policy and diagnosis stay separate: diagnosis never changes online enforcement.
- The decision model is optional; without it, diagnosis is fully deterministic.
- The schema is a fresh baseline. Startup compares the stored fingerprint and refuses a mismatch or a table set without a fingerprint, so a schema change requires recreating the database. Stored audit events carry the audit schema version described in [audit](audit.md); startup refuses a database whose events disagree.
- Agent keys are stored as hashes and provide the caller identity. The console uses a session cookie, the `X-SafeAgent-Request` header and an Origin check for state-changing requests. `X-SafeAgent-Session` groups calls from one agent key; it is not an identity.

## Commands

`go run ./cmd/safeagent -config config.toml serve` runs the service. The same binary provides `schema` (print the baseline DDL), `openapi` (write the console contract) and `verify-audit` (check stored event chains offline). The complete workflow, build and test targets are in [development](development.md); the acceptance lab is in [testenv](testenv.md).

## Related

- [Human architecture guide](../human/architecture.md)
- [Getting started](../human/getting-started.md)
- [Project README](../../README.md)
