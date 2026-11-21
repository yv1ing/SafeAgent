# Papers and implementation mapping

External facts and the full comparison live in [the landscape](../human/landscape.md). This page records what the code adopts, what it does not, and where the adopted concept lives.

## AgentDoG

The paper defines a three-axis taxonomy: risk source (where), failure mode (how) and real-world harm (what). SafeAgent keeps the vocabulary and the idea that one call, including its tool chain, is the unit of analysis.

| Adopted | Code |
| --- | --- |
| 8 risk sources, 14 failure modes, 10 harms | `internal/analysis/taxonomy.go` |
| Axis derivation from evidence, with no hand-written labels | `internal/analysis/catalog.go` (`AxisFor`) |
| Trajectory-level labels as a separate optional model answer | `internal/analysis/decision`, `internal/service/diagnosis` |
| Coverage-aware verdict, with no binary label | `internal/analysis/analysis.go` |

Not adopted: the fine-tuned guard models, ATBench results, sentence-level attribution, multimodal trajectories.

## AgentGuard

The paper describes a client-side hook that forwards runtime events to a central policy server using principal, tool, argument, target and history attributes, with allow, deny and review outcomes.

| Adopted | Code |
| --- | --- |
| Principal/tool/argument/target/trust vocabulary | `internal/observation`, `internal/analysis/policy` |
| Policy over one call and the tool chain | `internal/analysis/policy` (`trace.steps`) |
| Central policy view and audit trail | `internal/pipeline`, `internal/storage/repository` |
| Pre-execution mediation | request, tool-call and tool-result checkpoints in `internal/pipeline/policy.go` |

Not adopted: a language-specific client SDK, a separate DSL or visual rule builder, a manual review queue.

## Design mapping

| Project concern | SafeAgent implementation |
| --- | --- |
| Protocol-neutral observation | `internal/observe` translates four wire formats into `internal/observation`; detectors never see a wire type. |
| In-path tool control | `internal/llm/stream` holds complete tool calls; `internal/pipeline` releases or refuses them. |
| Four online phases | `request`, `tool_call`, `tool_result`, `response` in `internal/analysis/policy`. |
| Evidence and replay | `internal/analysis/finding.go`, `auditevent` and `internal/service/diagnosis`. |
| Audit integrity | `auditevent` HMAC chain, `repository.VerifyCallAudit`, `verify-audit`. |
| Optional model answer | `internal/analysis/decision` and the veto-only online question. |

## Non-claims

- The project does not reproduce either paper's model, benchmark, numbers or attribution algorithm.
- The relation detector is heuristic; value reuse is evidence to review, not proof of an attack.
- The decision model's answer never overwrites a deterministic finding, and its failure cannot release a block.
- Paper and repository licenses change independently; verify at the source before deriving work.

## Related

- [Roadmap](roadmap.md)
- [Analysis](analysis.md)
- [Human landscape](../human/landscape.md)
