# Roadmap

This page describes HEAD: what works, which limits are deliberate, and what is not built. It is a status page, not a plan.

## Implemented

- Four wire formats in both directions, with canonical streaming and non-streaming paths, a raw same-format pass-through and a per-call loss ledger.
- Classification by operation, purpose, purpose source, lane and outcome. Main calls drive conversations, diagnosis and traffic metrics; side calls stay in the conversation and count cost; auxiliary calls are protocol traffic with no conversation.
- Stored providers, channels, models, routes, aliases and targets, with soft deletion, impact previews, configuration health and a one-shot connect flow.
- Four online checkpoints, reversible redaction before policy reads the text, and an optional bounded stream review window.
- A signed audit chain per admitted call, a metadata timeline and offline verification.
- Asynchronous diagnosis with deterministic detectors, coverage-aware verdicts and an optional decision model.
- Console coverage for projects, monitor, calls, conversations, risk, activity, policy, redaction, keys, routes, members and settings, plus global providers, models, channels, users and operations.

## Deliberate limits

- No human approval or review queue. Online policy actions remain `block` and `record`.
- Images, audio and other binary payloads are not inspected or redacted. Stream review covers only the configured leading window; later text reaches the client before asynchronous diagnosis.
- A provider-executed server tool has already run when the gateway sees its result. The gateway can withhold the result but cannot undo the execution.
- Tool destinations and source-to-sink flows are heuristic. A finding is evidence for review, not proof of an attack; a policy that needs a strong claim matches findings.
- The decision model is optional, provider-dependent and returns calibrated answers without a rationale. Its failure cannot release a deterministic block.
- Analysis and metrics cover only traffic through the gateway. Cross-call semantic relations are not modelled: a flow stays inside one call.
- A trajectory reads at most 200 calls and 32 MiB of decoded payload; diagnosis expands at most 200 ancestors within the same budget. A truncated result cannot be `safe`.
- A route target produces one candidate in one format. Multi-choice requests are served only by a streaming same-format pass-through with no redaction or history expansion.
- Auxiliary calls follow `auxiliary_retention_days` (default 3; zero follows `retention_days`); main and side calls follow `retention_days`.
- One process serves one database, and the schema is a fresh baseline with no migration path. A schema or audit-version change requires recreating the database.
- There is no versioned, labelled safety benchmark. Detector changes are judged by unit, integration and acceptance fixtures.

## Related

- [Analysis](analysis.md)
- [Policy](policy.md)
- [Audit](audit.md)
- [Papers](papers.md)
