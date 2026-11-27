# Roadmap and current coverage

## Implemented

- one trajectory event log with per-trajectory HMAC chains, cumulative Merkle checkpoints and rebuildable calls/findings/summaries projections;
- a protocol gateway with four codecs, raw pass-through, a conversion loss ledger and reversible redaction;
- security state for labels, capabilities, obligations, approvals and coverage, with ten invariants, deterministic detectors and a console-derived verdict;
- online ingress, action and release gates, bound approvals, `action.authorized` lineage and coverage-aware approval for side-effecting tools;
- the provider/channel/model supply graph, project routes, console, offline evidence commands and the harness SDK;
- the acceptance lab and browser suite in [testenv](testenv.md).

## Deliberate limits

- No historical data or schema migration. A different fingerprint requires a fresh database.
- Single writer and single process per database. No HA design.
- No human approval queue beyond the trajectory page; approvals are API/console actions.
- No compensation executor for `allow_with_compensation`; the action catalogue names it but the first cut has no external-effect adapter.
- Harness adapters are a Go SDK and a repository writer; there are no production Claude Code, Codex, MCP or eBPF adapters in this repository.
- Semantic checks are optional. A model is never the sole gate for an irreversible action.
- Detectors are heuristic and coverage is measured; a `safe` verdict is not a proof.
- Raw content in blobs is encrypted with the instance key, but a database operator can read it.
- No cross-trajectory policy state or cross-session memory beyond the event log.
- An explicit session trajectory stays open until a harness appends `trajectory.end`, the process restarts, or retention removes it; the console has no close action.
- Risk is cumulative and diagnostic; score-based escalation and decay are not implemented.
- Capability attenuation is checked when a parent capability set exists; production delegation adapters are not included.
- The first cut recomputes a cumulative Merkle checkpoint on each append; partitioned event storage and incremental checkpointing are planned for very long trajectories.

## Next work

1. Production harness adapters and a signed recorder identity.
2. A compensation catalogue and external-effect adapters.
3. SIEM export with a stable bundle schema.
4. Partitioned event storage and a streaming projector for very long trajectories.
5. A versioned ground-truth benchmark for the invariants and the decision model.

## Related

- [Model](model.md)
- [Threat](threat.md)
- [Audit](audit.md)
- [Testenv](testenv.md)
