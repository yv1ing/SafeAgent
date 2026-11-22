# Roadmap

The current release covers four wire formats, online request/tool/tool-result checkpoints, non-streaming response inspection, an optional bounded streaming review window, an event timeline, an explicit session link and a signed audit chain. The items below are the known limits that matter for agent safety; they are ordered by how much they change an operator's ability to see and stop an action.

## P0: semantic and cross-call relations

**Problem.** Flow detection matches specific tokens. Paraphrases, translated instructions and values transformed by a tool are invisible. Relations stop at one call.

**Direction.** Keep the deterministic literal detector as the baseline and add an optional embedding or model-assisted relation pass over the same observation model. Add a cross-call edge type that links a tool result in call A to a later side-effecting call in call B, but only when the conversation chain is explicit or verified. Every semantic edge records its detector, model and confidence, and a semantic edge never overrides a deterministic block.

**Touchpoints.** `internal/analysis/builtin/flow.go`, `internal/analysis/engine.go`, `internal/observation` (trace/chain), `internal/service/diagnosis` (ancestor expansion), decision-model budget.

**Acceptance.** Paraphrased and cross-call cases are added to the analysis acceptance suite; a low-confidence semantic edge produces `unknown`, not `safe`; the deterministic path remains unchanged when the optional pass is disabled.

## P1: multi-agent trajectories

**Problem.** The unit of analysis is one agent key and one call chain. A2A or supervisor/worker setups split one task across several keys or sessions, so a flow can cross a boundary the model does not represent.

**Direction.** Introduce an explicit trajectory identifier that a trusted orchestrator can attach to several agent keys in one project. Build the chain view over that identifier, and let policy conditions read trajectories without changing the per-call enforcement point. Keep the default single-key behavior.

**Touchpoints.** `internal/pipeline` session resolution, `calls.parent_call_id`/`session_id`, `internal/service/diagnosis`, console conversation tree, policy CEL surface.

**Acceptance.** A two-key fixture links into one trajectory; an unauthenticated caller cannot join another project's trajectory; per-call checkpoints still run before any tool side effect.

## P1: repeatable evaluation

**Problem.** The project has unit, integration and acceptance tests, but no repeatable safety benchmark with labelled trajectories. Detector changes are judged by fixtures and review.

**Direction.** Add a small, versioned evaluation fixture in the style of ATBench: generated and reviewed trajectories with risk source, failure mode and harm labels. Do not copy the paper's dataset or claim its numbers. Report the same coverage-aware verdicts the product produces, and keep false negatives visible.

**Touchpoints.** `internal/analysis` fixture format, a testenv evaluation runner, versioned detector/catalogue metadata, `docs/human/verification.md`.

**Acceptance.** The runner reports per-code precision/recall and verdict confusion; a detector version change produces a diff; the fixture is small enough to run in the normal acceptance lab.

## Known boundaries

- Human approval/review is intentionally not implemented. The policy actions remain `block` and `record`; adding a queue requires an operator-facing workflow and a durable pending state.
- Image, audio and other binary payloads are not inspected or redacted.
- Stream review covers only the configured leading window. Text after that window reaches the client before asynchronous diagnosis.
- A provider-executed server tool has already run before the gateway sees its result; the gateway can withhold the result but cannot undo the execution.
- The decision model is optional, provider-dependent and returns calibrated answers without a rationale.
- The gateway sees only traffic that passes through it.

## Related

- [Analysis](analysis.md)
- [Policy](policy.md)
- [Audit](audit.md)
- [Papers](papers.md)
