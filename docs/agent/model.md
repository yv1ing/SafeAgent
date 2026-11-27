# Trajectory model

## Unit of security

A **trajectory** is one delegated task, from `trajectory.start` to `trajectory.end`. It can contain hundreds of model calls and tool calls. A **call** is an interval inside it, opened by `action.proposed` and closed by `action.executed` or a terminal `gate.decision`. There is no call row as the source of truth.

A **state transfer** is the atomic audit event: content entering the context, a compaction, a memory write or read, an action proposal, an approval, a rollback or a delegation. The event log is the only source of truth; `calls`, `findings`, `trajectory_states`, `edges` and `trajectory_summaries` are derived.

## Event record

Every event carries:

- identity: `id`, `trajectory_id`, contiguous `seq`, `kind`, `boundary`, `actor`, `principal`;
- optional `call_id` and `action_id`, which anchor projections without becoming separate sources;
- typed `inputs` and `outputs` (`segment`, `role`);
- optional `auth` (capability, approval, invocation hash, execution count, lease);
- typed `checks` and `obligations`;
- optional `snapshot` and `capture`;
- a kind-specific `payload`;
- `prev_hash` and `hash`.

The typed fields are hashed together with the canonical payload. A consumer may ignore a payload kind it does not know, but must not infer a gate decision from payload text alone.

## Boundaries and kinds

| Boundary | Kinds |
| --- | --- |
| `root` | `trajectory.start`, `trajectory.end` |
| `ingress` | `observation` |
| `context` | `context.assemble`, `context.compact` |
| `memory` | `memory.read`, `memory.write`, `memory.consolidate` |
| `action` | `action.proposed`, `action.authorized`, `action.executed`, `external.effect`, `gate.decision`, `approval.*`, `delegation.spawn` |
| `recovery` | `rollback`, `delegation.return` |
| `control` | `tool.registry.change`, `policy.change`, `model.change`, `capture.heartbeat` |

`checkpoint` exists as a kind for a harness that records a checkpoint reference, but the gateway writes checkpoints to their own table and does not emit it.

`action.proposed` carries the call metadata (operation, purpose, lane, format, public model) for a model call, or the tool name, sink, target, arguments hash, input segment ids, required capabilities and invocation hash for a tool call. Provider attempts are `action.proposed` with `action_type=provider_attempt`; each gets its own `action.executed`.

A tool call that passes the action gate emits `action.authorized`. It carries the deciding approval, if any, and the invocation hash; this is the authorization edge a later execution is checked against.

## Segments

A segment is a content-addressed piece of text or JSON. The raw bytes live in `blobs`, encrypted with the instance key; a segment row records the media type, size, integrity and confidentiality labels, and the id of the event that first produced it. The canonical request and response of a call are stored as JSON segments bound to the proposal and execution event ids, so history and stored-response retrieval do not copy content into every event.

Labels travel with segments:

- integrity: `trusted`, `untrusted`, `unknown`;
- confidentiality: `public`, `private`, `user_identity`.

Tool descriptions and tool results are untrusted. User content is user-identity. System and developer content is private. A label can only become stronger, never silently weaker.

## Edges

`edges` is the only place causality is expressed. The writer currently emits `derived_from`, `used_data` and `authorized_by`; `executed_by`, `delegated_to`, `supersedes`, `revoked_by` and `checkpoint_of` are defined for adapters but not produced by the online path. Consumers must not infer causality from event ordering or timestamps.

## State

`state.State` is the reducer output:

- goal; compaction constraint losses appear as obligations, while a general constraint list is not populated by the current recorder;
- labels by segment;
- capabilities and their delegation parent;
- obligations (`pending`, `satisfied`, `violated`, `expired`);
- approvals and execution counts;
- actions and their status;
- memory entries and authority;
- risk score and max severity;
- coverage counters, gaps and threshold;
- policy and redaction-rule versions in force.

The reducer is pure. The same event range produces the same state hash; `trajectory_states` stores the full JSON, the head event and the state hash.

## Coverage

Coverage is measured, not assumed:

- every `action.proposed` expects an `action.executed` or a terminal gate decision;
- every observation, context assembly, memory transfer, rollback and delegation boundary is counted when its event is present;
- `capture.heartbeat` reports recorder gaps, which add to the denominator;
- the project has a `coverage_threshold`.

The stored coverage is the trajectory-wide value. A gate that runs while a call is still open uses `CoverageForCall`, which removes that call's own unfinished actions from the denominator; an action left open by an earlier call still lowers it. A trajectory below the threshold has a `capture_gap` finding, cannot be reported `safe`, and a side-effecting tool needs an approval before it can run.

## Gates

| Gate     | Event family                                               |
| -------- | ---------------------------------------------------------- |
| ingress  | observation, context assembly, tool results                |
| action   | action proposal, authorization, execution, external effect |
| release  | response leaving the boundary                              |
| recovery | rollback and delegation return                             |

A gate decision is an event (`gate.decision`). It records the gate, action, rule or invariant, severity, reason, checks and whether it terminates the call. A deterministic gate may only read the current state, the current event, the observation trace and a policy compiled from the control plane; it may not call a codec or a service. The optional decision-model veto runs after the deterministic action gate and can only add a block.

The online pipeline enforces ingress, action and release; recovery is reduced and projected. Gate outcomes, policy phases and failure semantics are in [Policy](policy.md); the verdict and invariants are in [Threat](threat.md).

## Identifiers

- trajectory ids: `trj_<random>`;
- event ids: `evt_<random>`;
- approval ids: `apv_<random>`, checkpoint ids: `ckp_<random>`;
- segment and blob ids: SHA-256 of the content;
- invocation hash: SHA-256 of the tool name, exact arguments and target;
- event hash: HMAC-SHA256 over the canonical typed record and `prev_hash`.

## Related

- [Threat](threat.md)
- [Policy](policy.md)
- [Storage](storage.md)
- [Audit](audit.md)
