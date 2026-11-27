# Verification

[中文](verification_zh.md)

SafeAgent is verified at three levels: unit tests for the deterministic core, integration tests against PostgreSQL, and an acceptance lab that drives real protocol clients through configured channels.

## What the checks cover

- Canonical conversion in both directions for Chat Completions, Responses, Anthropic Messages and Gemini, including streaming, raw pass-through and conversion losses.
- Redaction before egress, restoration on return, stable value identities and historical rule display.
- The full trajectory path: start, observations, context assembly, action proposal and execution, gate decisions, approvals, checkpoint and end.
- The ten invariants, deterministic detectors, coverage, risk and finding fingerprints.
- Approval binding, single-use consumption, replay rejection and blocked irreversible actions.
- Route resolution, failover, quotas, concurrency limits, tool holds and stream review.
- Evidence bundle export and offline verification, including tamper detection and checkpoint failures.
- Console access control, project isolation, CSRF protection and raw-payload visibility.
- Browser flows for sign-in, projects, channels, routes, calls and trajectories.

The acceptance lab uses real upstream providers configured through environment variables. A case that cannot run is reported as skipped with its reason; a skip is not a pass.

## What the checks cannot show

No finite test proves the absence of prompt injection, side-channel leakage or a missed state transfer. Coverage is measured at the recorder; an agent path that never reaches the gateway is invisible. The acceptance lab exercises representative flows, not every provider behavior.

## Further reading

- [Architecture](architecture.md)
- [Agent safety](agent-safety.md)
- [Protocol conversion](protocol-conversion.md)
