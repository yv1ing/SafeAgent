# Threat engine

## Layers

The threat path is deterministic first:

```text
event
  -> state reducer (labels, capabilities, obligations, approvals, coverage, risk)
  -> invariant checks (I1-I10)
  -> deterministic detectors (schema, content, flow)
  -> findings + gate decisions
  -> optional semantic decision model (ambiguous tail only)
```

The `internal/threat` package owns findings, the code catalogue, the detector registry and the ten invariants. It does not import storage, services, transports or the wire model. The `internal/state` package owns the machines the invariants read. The ordered policy lives in `internal/policy`; the optional semantic model boundary lives in `internal/decision` and its project service in `internal/service/guard`. A detector that cannot name its transfer and invariant is not admitted.

## Invariants

| ID | Invariant | Code |
| --- | --- | --- |
| I1 | untrusted content cannot reach an external send without approval or declassification | `untrusted_to_external_send` |
| I2 | private or user-identity content cannot enter a public sink | `private_to_public_sink` |
| I3 | a delegated capability is a subset of the parent capability | `capability_escalation` |
| I4 | every pinned constraint survives compaction | `constraint_lost_in_compaction` |
| I5 | a memory entry's authority is at least its source's authority | `memory_authority_collapse` |
| I6 | a rollback with missing, opaque or mismatched cache state is incomplete | `rollback_cache_mismatch` |
| I7 | an approval is consumed at most once for one invocation hash | `approval_replay` |
| I8 | coverage below the configured threshold cannot be safe | `capture_gap` |
| I9 | delegation may only attenuate capabilities | `capability_escalation` |
| I10 | every executed action has a proposal and an authorizing gate or approval event | `unattributed_action` |

Invariants are pure functions of the pre-event state, post-event state and current event. A finding points at one event and, when relevant, the segments that justify it. The fingerprint includes the event, code and invariant, so two invariants firing on one event are both retained.

## Deterministic detectors

The built-in detector set is:

- `schema`: validates tool arguments against the declared tool schema, including required fields, types, enums, ranges and patterns; it also detects undeclared tools and tool-choice violations.
- `content`: detects prompt injection, hidden Unicode instructions, dangerous commands, sensitive paths, private network targets, credential material and suspicious links.
- `flow`: detects untrusted values and secret placeholders reused as arguments to acting tools.

The online path runs the detector subset each gate needs and records the checks on the gate decision. The projector persists every check as a finding in the `findings` projection. A check failure is a coverage error, not a clean result.

Detection and enforcement are not the same thing. The online gates enforce the detector findings they are configured to act on plus the approval defaults; the invariant projector records the full I1-I10 result for audit and for the trajectory verdict. A trajectory can therefore contain a finding that did not block the call that produced it.

## Findings

A finding carries:

- stable `fingerprint` (detector, code, origin, sink, semantic location, ordinal);
- code, origin, trust, sink and the three axes (source, failure mode, harm);
- severity, confidence, detector id and version;
- evidence locators and safe quotes;
- optional invariant, transfer, control and segment ids.

Findings never quote a redacted range. They are stored in `findings`, keyed by trajectory, event and fingerprint.

## Coverage and verdict

The console derives one verdict from the stored findings and the trajectory coverage:

- `unsafe` if a high-severity finding exists;
- `unknown` if coverage is below the threshold, a detector failed, the finding cap was hit, or a medium finding remains;
- `safe` only when coverage meets the threshold and no high or medium finding exists.

The verdict is computed when the trajectory is read. It is not a stored source of truth and it does not add a second decision path. A per-request detector pass is a check, not a verdict.

## Decision model

The optional decision model is a semantic tail. It is configured per project, receives an attributed short context, and may add a block to an already-authorized tool call. It is never the sole gate for an irreversible action. The gateway records the veto outcome, confidence and rule id; it does not store the model prompt or output. A timeout, parse error or exhausted budget leaves the deterministic result intact and records `decision_model_unavailable`.

## Risk

Risk is a cumulative trajectory score:

- each high check adds 1.0, medium adds 0.3, a blocked action or gate adds 0.5, and an executed action with an error adds 0.05;
- the score is capped at 10;
- the trajectory summary records the strongest finding severity.

The score is part of `trajectory_states` and is updated by the reducer. Risky-action escalation based on the score is not implemented; the score is diagnostic.

## Related

- [Model](model.md)
- [Policy](policy.md)
- [Audit](audit.md)
- [Storage](storage.md)
