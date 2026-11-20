# Threat modeling

[中文](threat-modeling_zh.md)

Agent risk unfolds along a trajectory: content enters from a source, the model decides how to use it, and a tool acts on the result. SafeAgent models that trajectory directly and keeps the description aligned with the evidence in the console.

## Two research lineages

The three-axis taxonomy comes from AgentDoG, which labels where risk entered, which failure mode the agent exhibited and what harm could follow. SafeAgent uses the taxonomy. Whole-trajectory labels come from an optional decision model, and the paper's fine-tuned guard models are outside this project.

The policy view comes from AgentGuard, an attribute-based access-control design for tool-using agents. SafeAgent uses its runtime attributes (principal, tool, arguments, target and trust) and the idea of rules over tool calls and tool chains. This project implements the idea independently. The papers, licenses and the points where this project differs are documented in [the agent guide](../agent/papers.md).

## Origin and sink

A finding answers two questions: where the content came from and where it can go. A deterministic finding records its origin and sink, and fills only the three-axis fields that its code, origin and sink support.

| Origin                  | Meaning                                              |
| ----------------------- | ---------------------------------------------------- |
| system, developer, user | instructions and task text supplied with the request |
| assistant               | the model's own text and tool calls                  |
| tool result             | output returned by a tool                            |
| tool description        | the description of a declared tool                   |
| environment             | a coverage problem raised by the analyzer            |

A sink describes what a tool call can do: send data externally, execute a shell command, read or write a file, access credentials, make a payment, do nothing observable, or remain unknown. The gateway infers it from the tool's name, arguments and target. The result may require confirmation.

A flow connects external content inside one call to a later side-effecting call. Value reuse means a specific token from the content appears in the arguments. Instruction following means a token from the sentence that carries an injected instruction appears there. Co-occurrence by itself creates no relation. Flows stay inside one call.

## Two label sets

A deterministic finding carries only the axes its detector can justify. An empty axis means "not established"; it says nothing about safety.

The optional decision model answers fixed questions about the whole trajectory: whether it is unsafe, where the risk came from, which failure mode applies, what harm is plausible, and how severe it is. Its answers are stored beside the findings and leave the deterministic labels unchanged.

## Verdicts

A high-severity finding makes a call `unsafe`. With complete coverage, a sufficiently probable unsafe answer from the decision model also makes it unsafe; when the provider also reports a confidence, that value must clear the threshold as well.

Without a high-severity finding, a call is `unknown` if coverage is incomplete, only medium findings are present, the model is uncertain, or the model call failed or exhausted its budget.

A call is `safe` only with complete coverage, no medium or high finding and no unsafe model answer. The absence of findings by itself does not establish safety.

## Online authorization

The model has one online role: veto. It is asked after deterministic rules allow a side-effecting tool call. A low authorization probability with enough confidence blocks the call; a timeout, malformed answer or exhausted budget leaves the earlier policy outcome in place. The model can add a block and cannot remove one.

The authorization question excludes tool output, earlier assistant text and earlier tool calls, so external content cannot influence its own release. It includes the task and instructions, the principal, the declared tools, non-assistant text and the pending tool call.

## Current design trade-offs

- **Heuristic relations** — Value reuse is more reliable than instruction following. Both match specific tokens, so paraphrases and semantic similarity are invisible.
- **Inferred capabilities** — A sink comes from the name, arguments and target; a misleading tool name can misclassify it.
- **Opaque model** — The decision model returns calibrated answers without a rationale, and its provider may not treat input as hostile. SafeAgent isolates untrusted text, excludes it from authorization, defaults to the redacted view and records the model name. It cannot make the provider trustworthy.
- **Text-only redaction** — Images and audio are outside the redaction engine.
- **Gateway scope** — SafeAgent sees only traffic that passes through it.

## Related

- [Architecture](architecture.md)
- [Console](console.md)
- [Agent guide](../agent/README.md)
