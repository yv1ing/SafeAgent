# SafeAgent agent guide

> This page describes the code at HEAD. It is not a product introduction; the human-facing documents in [docs/human](../human) cover that. Read the path that matches the change, not the whole directory.

## Why the unit is a trajectory

A gateway sees one request and one response. That cannot explain why an agent sent an email on turn 700, whether compaction dropped a constraint, or whether a rollback left an inference cache alive. Those are questions about **state transfers** across the working context, session history, memory, the environment and the inference server. SafeAgent therefore makes one delegated task a trajectory; a call is an interval inside it, and a state transfer is the atomic audit event. The design is implemented, not aspirational; [Roadmap](roadmap.md) lists the remaining limits.

## Reading paths

- **A normal gateway change**: [model](model.md) → [protocol](protocol.md) → [policy](policy.md) → [storage](storage.md) → [development](development.md).
- **A security or threat change**: [model](model.md) → [threat](threat.md) → [audit](audit.md) → [storage](storage.md).
- **Control plane, console or API work**: [providers](providers.md) → [storage](storage.md) → [development](development.md).
- **Build, test or acceptance work**: [development](development.md) and [testenv](testenv.md).
- **Context or onboarding**: this page, then [roadmap](roadmap.md) and [glossary](glossary.md).

## Document map

| Page | Content |
| --- | --- |
| [model](model.md) | trajectory event log, segments, edges, state machines, gates and identifiers |
| [threat](threat.md) | invariants, deterministic checks, findings, coverage and verdict rules |
| [policy](policy.md) | policy documents, phases, approvals, gates and failure semantics |
| [protocol](protocol.md) | the four wire protocols, canonical IR, conversion and streaming |
| [providers](providers.md) | provider catalog, channels, models, routes, health and discovery |
| [storage](storage.md) | event log, content stores, projections, append transaction, retention and recovery |
| [audit](audit.md) | event hash chain, Merkle checkpoints, evidence bundles and verification |
| [development](development.md) | build, test, package layout, contracts and conventions |
| [testenv](testenv.md) | acceptance lab, suites and required environment |
| [roadmap](roadmap.md) | current coverage, deliberate limits and next work |
| [glossary](glossary.md) | one English term per concept, with Chinese wording |

## Core invariants

1. **One write model.** `trajectory_events` is the only authoritative record. Calls, findings, states, edges and summaries are projections.
2. **One state per trajectory.** `trajectory_states` is a deterministic reduction of the event range. Rebuilding projections is safe; separate projection versions are not implemented.
3. **A call is an interval.** `action.proposed` / `action.executed` and the events between them are one call; there is no per-call terminal event.
4. **Lineage is explicit.** Every action can be traversed to input segments and to an authorization or approval event through `edges`.
5. **Completeness is visible.** A trajectory below its coverage threshold cannot be `safe`, and a capture gap is a finding.
6. **The recorder cannot sign checkpoints.** The harness SDK submits events; the gateway assigns sequence numbers, signs the chain and signs checkpoints.
7. **No compatibility path.** There is no migration and no dual write. A different schema fingerprint is refused at startup and the database is recreated.

## Package layout

```text
cmd/safeagent            serve, schema, openapi, verify-trajectory, export-bundle, verify-bundle
internal/classify        call classification: operation, purpose, purpose source and lane
internal/trajectory      event types, payloads, canonical hashing and the chain
internal/state           reducer, labels, capabilities, obligations, approvals, coverage
internal/threat          finding model, detectors, invariants and the threat catalogue
internal/policy          ordered CEL policy and phase compilation
internal/decision        typed semantic decision-model boundary and client
internal/evidence        Merkle checkpoints, bundle build and offline verification
internal/harness         state-transfer SDK used by a harness
internal/pipeline        online paths: admission, gates, routing, streaming and append
internal/observation     protocol-neutral items, targets, trust and locators
internal/llm             wire model, four codecs, providers, upstream stream engine
internal/redact          reversible masking and value identities
internal/storage         entities and PostgreSQL repository
internal/service         access, console, control and decision-model guard use cases
internal/transport       gateway and console HTTP transports
internal/platform        configuration, limits, netguard, routing and cryptography
internal/app             composition root and lifecycle
```

The enforced dependency direction is transport → service → storage, with the domain libraries (`classify`, `trajectory`, `state`, `threat`, `policy`, `decision`, `evidence`, `harness`, `observation`, `redact`, `llm/*`) below the service and pipeline layers. The architecture test in [internal/app/architecture_test.go](../../internal/app/architecture_test.go) is the executable version of this section.

## Provenance and non-claims

The trajectory ledger, state-transfer taxonomy and evidence bundle follow the design in [Securing and Auditing Long-Horizon LLM Agents](https://arxiv.org/abs/2611.23346). The three finding axes come from AgentDoG; the deterministic tool and sink checks are adapted from AgentGuard. The implementation is in `internal/trajectory`, `internal/state`, `internal/threat` and `internal/evidence`.

The project does not claim formal safety, complete capture or a proven absence of prompt injection. It claims a visible, verifiable record of what crossed which boundary and which deterministic control fired.

## Related

- [Model](model.md)
- [Threat](threat.md)
- [Policy](policy.md)
- [Storage](storage.md)
- [Audit](audit.md)
- [Development](development.md)
- [Testenv](testenv.md)
- [Roadmap](roadmap.md)
