# Papers and implementation mapping

External facts and the full comparison live in [the landscape](../human/landscape.md). This page records only what the code adopts and where it lives.

## AgentDoG

Three-axis taxonomy: risk source (where), failure mode (how) and real-world harm (what). SafeAgent keeps the vocabulary and treats one call with its tool chain as the unit of analysis.

| Adopted | Code |
| --- | --- |
| 8 risk sources, 14 failure modes, 10 harms | `internal/analysis/taxonomy.go` |
| Axis derivation from code, origin and sink | `internal/analysis/catalog.go` (`AxisFor`) |
| Trajectory labels as a separate optional model answer | `internal/analysis/decision`, `internal/service/diagnosis` |
| Coverage-aware verdict | `internal/analysis/analysis.go` |

Not adopted: the fine-tuned guard models, benchmark numbers, sentence-level attribution and multimodal trajectories.

## AgentGuard

Runtime attributes (principal, tool, arguments, target, history) and policy over a tool chain.

| Adopted                             | Code                                               |
| ----------------------------------- | -------------------------------------------------- |
| Attribute vocabulary                | `internal/observation`, `internal/analysis/policy` |
| Policy over one call and the chain  | `internal/analysis/policy` (`trace.steps`)         |
| Central policy view and audit trail | `internal/pipeline`, `internal/storage/repository` |
| Pre-execution mediation             | request, tool-call and tool-result checkpoints     |

Not adopted: a client SDK, a separate DSL or visual rule builder, a manual review queue.

## Non-claims

- The project does not reproduce either paper's model, benchmark, numbers or attribution algorithm.
- The relation detector is heuristic: value reuse is evidence to review, not proof of an attack.
- A decision-model answer never overwrites a deterministic finding, and its failure cannot release a block.

## Related

- [Roadmap](roadmap.md)
- [Analysis](analysis.md)
- [Human landscape](../human/landscape.md)
