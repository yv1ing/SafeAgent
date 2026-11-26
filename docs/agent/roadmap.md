# Roadmap

This page states what the current release covers and which limits are deliberate. It is a status page, not a plan.

## Current coverage

- Four wire formats convert in both directions, with canonical non-streaming and streaming paths, a raw same-format pass-through and a per-call loss ledger.
- Calls are classified by operation, purpose, purpose source, lane and outcome. Main calls drive conversations, diagnosis and traffic metrics; side calls stay in the conversation without diagnosis; auxiliary calls are protocol traffic with no conversation.
- Providers, channels, models, routes, aliases and targets are stored and editable, with soft deletion, impact previews, configuration health and a one-shot connect flow.
- Online policy runs at four checkpoints, with reversible redaction before policy reads the text and an optional bounded stream review window.
- A conversation is projected as one trajectory with stable node and edge identities, replay steps, bounded history and an in-process cache. The redaction index is deduplicated by value id.
- The asynchronous worker rebuilds a call from the audit events, analyses it with deterministic detectors and an optional decision model, and stores findings, flows, coverage and a verdict.
- Every admitted call has an HMAC-chained audit trail, a timeline and an offline verification command.
- The console covers monitor, calls, conversations, risk, activity, policy, redaction, keys, routes, members and settings, plus global models, channels, users and operations.

## Known boundaries

- Human approval and a review queue are not implemented. Online policy actions remain `block` and `record`.
- Images, audio and other binary payloads are not inspected or redacted.
- Stream review covers only the configured leading window. Text after that window reaches the client before asynchronous diagnosis.
- A provider-executed server tool has already run before the gateway sees its result. The gateway can withhold the result but cannot undo the execution.
- Tool destinations and source-to-sink flows are inferred heuristically. A finding is evidence for review, not proof of an attack, and a policy that needs a strong claim matches findings.
- The decision model is optional, provider-dependent and returns calibrated answers without a rationale. Its failure cannot release a deterministic block.
- Analysis and metrics cover only traffic through the gateway.
- A conversation trajectory reads at most 200 calls and 32 MiB of decoded payload, and a diagnosis expands at most 200 ancestors within the same 32 MiB budget. Beyond that the result is marked truncated and the verdict cannot be `safe`.
- A route target produces one candidate in one format. Multi-choice requests are served only by a streaming same-format pass-through with no redaction or history expansion.
- Auxiliary calls are retained for the project's `auxiliary_retention_days` (default 3); main and side calls follow `retention_days`.
- The service runs a single instance against one database, and the schema is a fresh baseline with no migration path. A schema or audit-version change requires recreating the database.
- Cross-call semantic relations are not modelled: a flow stays inside one call, and the analysis unit is one agent key and one call chain. A multi-agent task split across keys or sessions is not linked into one trajectory.
- There is no versioned, labelled safety benchmark. Detector changes are judged by unit, integration and acceptance fixtures.

## Related

- [Analysis](analysis.md)
- [Policy](policy.md)
- [Audit](audit.md)
- [Papers](papers.md)
