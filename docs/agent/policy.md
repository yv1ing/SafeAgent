# Gates and policy

## Gate phases

The project policy is one ordered list of CEL rules. Rules select a phase:

| Phase         | Gate    | Inputs                                           |
| ------------- | ------- | ------------------------------------------------ |
| `request`     | ingress | principal, tool specs, request trace, findings   |
| `tool_call`   | action  | pending tool call, its target and tags, findings |
| `tool_result` | ingress | new tool results, findings                       |
| `response`    | release | response text/status/finish, findings            |

The same document is evaluated at the four phases. A rule matches when its `when` expression is true. `block` stops the event; `record` stores a decision but keeps going. Rules are evaluated by descending priority and then rule id.

## Typed surface

CEL sees `principal`, `tool`, `target`, `trace`, `findings`, `results`, `response`, `env` and `args`. `target` defaults to the shown target, so a policy can never act on a value that redaction removed from the caller's view. `findings` are the deterministic checks of the current pass.

## Gates

The online pipeline blocks at three gates; recovery is recorded and projected rather than blocking an append:

- **Ingress** admits the request and the new tool results. Redaction runs before the gate. A blocking redaction rule is an ingress block.
- **Action** evaluates a proposed tool call. A block prevents the tool call from being released. If the action gate requires approval, the gateway emits `approval.requested` and returns `approval_required`; a granted approval bound to the invocation hash lets the retry through, emits `approval.consumed` and `action.authorized`.
- **Release** evaluates a response before it leaves the gateway. A block becomes a protocol-shaped error or a withheld stream.
- **Recovery** covers rollback and delegation return. A harness or console append is reduced and checked by the invariant projector; an opaque or mismatched cache produces an obligation and a finding, not an online veto.

The policy itself is compiled in `internal/policy`. Approval state and coverage come from `state.State`; the request and tool views come from `observation.Trace`. A gate never calls a codec, a provider or a console service.

## Approvals

An approval is an event, not a prompt field:

- `approval.requested`: approval id, action id, invocation hash, expiry and scope;
- `approval.granted`: approver, nonce, single-use marker and expiry;
- `approval.denied`: reason;
- `approval.consumed`: execution count and invocation hash.

The invocation hash is the tool name, exact arguments and target. A rollback or retry must present the same approval; a second consumption is an `approval_replay` invariant finding. The console API exposes grant and deny endpoints; the first cut has no approval queue UI beyond the trajectory page.

## Failure semantics

- **Policy unavailable at startup** is fatal: the service does not start with a document that does not compile.
- **Guard failure online** is fail-closed only when the project declares a blocking rule for that phase. A project with only `record` rules continues and records the failure.
- **Audit append failure** blocks the gate. The action is not released.
- **Decision-model failure** never releases an action on its own and never blocks a deterministic allow except through its explicit veto path.
- **Coverage below threshold** requires approval for a side-effecting tool and prevents `safe`. The check excludes the current call's own unfinished actions, so a normal in-flight call is not treated as a capture gap.

## Limits

- at most 200 rules, 16 KiB per condition and 256 KiB per document;
- 50 ms evaluation budget per phase;
- rule phases cannot introduce new hook points;
- no human-approval UI beyond the trajectory page;
- no cross-trajectory policy state; all state is per trajectory.

## Related

- [Model](model.md)
- [Threat](threat.md)
- [Storage](storage.md)
- [Audit](audit.md)
