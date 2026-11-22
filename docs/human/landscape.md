# Landscape

[中文](landscape_zh.md)

This page places SafeAgent between two kinds of adjacent systems: gateways that solve "the agent speaks several protocols and the upstream speaks one", and guardrails that solve "the agent called a tool it should not have". AxonHub and Sub2API represent the first; AgentGuard and AgentDoG represent the second.

Their optimization goals differ, so feature counts cannot be compared directly. What is worth comparing is where each system puts complexity and what it gives up.

## Method

Only primary material is used: local source snapshots and READMEs of the reference projects, and the arXiv versions of the two papers. Facts were checked on 2026-11-21.

- AxonHub uses the local snapshot `axonhub-1.0.0-rc.4`. Its internal version markers disagree (the build version, CHANGELOG and directory name differ), so this page uses the directory name as a snapshot label.
- Sub2API uses the local snapshot `sub2api-0.2.24`; its `VERSION` file says `0.2.23`. The directory name is again treated as a snapshot label.
- AgentGuard refers to arXiv:2605.28071v1 (27 May 2026).
- AgentDoG refers to arXiv:2601.18491v2 (23 Apr 2026).

This page describes mechanisms and trade-offs. Benchmark numbers are not used as evidence, and a paper's claims are not treated as independently verified results. Reference projects keep changing, so capability statements apply to the named snapshot.

## AxonHub

AxonHub is positioned as a multi-provider AI gateway and development platform: one interface for many models, with routing, load balancing, cost tracking, RBAC and request tracing.

It also uses a unified intermediate representation, shaped like OpenAI Chat Completions, with non-OpenAI semantics carried in extra fields. A request takes one inbound and one outbound conversion, and same-format traffic can pass through raw when no rewrite is needed. Streaming is converted event by event, with SSE, WebSocket and Gemini array framing.

The provider catalog is large and includes many vendor variants and OAuth channels. Routing supports adaptive, failover, circuit-breaker and round-robin strategies.

On security, AxonHub covers the basics at the gateway level: API keys, projects and roles, quotas, IP allowlists, sensitive-header masking, input regex protection and an optional Claude Code tool-call reviewer. Prompt protection is mainly regex replacement or rejection over input text. General tool-level policy, human approval, output-side inspection and a signed audit chain are outside its scope.

Its design center is provider coverage, routing and operations; the agent action boundary is not part of it.

The root is Apache-2.0 and the `llm/` directory is LGPL-3.0, so a derived work needs to respect the directory-level difference.

## Sub2API

Sub2API is positioned as a subscription-quota distribution and resale platform: it pools upstream accounts and manages quota, billing, rate limits and scheduling per user, group and model.

Protocol conversion is mostly pairwise, with Responses as a hub for some paths. The code contains direct bridges and chained paths; the latter avoid a double conversion for some combinations. Not every path is unified into one canonical model.

Streaming uses hand-written state machines, and strict field serialization satisfies clients such as Codex and Grok that require certain fields. Usage normalization handles provider-specific cache fields and TTL buckets.

On security, Sub2API provides input content moderation and an optional prompt audit, an append-only admin audit log, and fairly complete account scheduling and concurrency control. Tool-call gates, human approval, output DLP and agent action audit are outside its scope.

Upstream URL checks are permissive by default, and account credentials and user API keys do not appear to be uniformly encrypted at the application layer.

Its design center is account pools, billing and operations; agent safety is not part of it.

The license is LGPL-3.0-or-later.

## AgentGuard

AgentGuard is a short paper describing a client-server access-control framework. A client hooks the agent framework and its tools, sends the principal, tool, arguments, target and session history to a central policy server, and the server can decide with rules, an LLM-assisted check or manual review: allow, deny or escalate.

It uses attribute-based access-control vocabulary, emphasizes policies over one tool and over chains, and provides a visual policy and audit interface. The paper reports no quantitative evaluation and does not formalize a threat model; the repository README also describes plugins and integrations beyond the short paper.

The license differs between artifacts: the paper says MIT, the repository declares GPL-3.0.

SafeAgent borrows the runtime attribute set and the view that a policy can cover a tool chain; the specific implementation stays with AgentGuard.

## AgentDoG

AgentDoG is a diagnostic guardrail paper for agent trajectories. It separates risk into three orthogonal dimensions: risk source (where it entered), failure mode (how the agent failed) and real-world harm (what followed). A fine-tuned guard model labels a whole trajectory and produces fine-grained diagnoses.

The paper also proposes step- and sentence-level attribution and a trajectory-level benchmark.

The input is text trajectories, and evaluation depends on the benchmark built for the paper; the paper itself notes that fine-grained failure-mode diagnosis remains difficult. The models and benchmark are released under Apache-2.0, but the paper and repository are separate artifacts whose versions and numbers should be checked independently.

SafeAgent borrows the taxonomy and the idea that a trajectory is the unit of analysis; the trained model and benchmark remain AgentDoG's.

## Dimensions

| Dimension | AxonHub | Sub2API | AgentGuard | AgentDoG | SafeAgent |
| --- | --- | --- | --- | --- | --- |
| Primary goal | multi-provider gateway and platform | subscription quota, billing and relay | tool-call access control | trajectory-level diagnosis | agent behavior security gateway |
| Conversion model | unified OpenAI-shaped IR | pairwise + Responses hub | not applicable | not applicable | item/part canonical model + capabilities |
| Streaming | event conversion + raw pass-through | hand-written pairwise state machines | not applicable | not applicable | commit boundary + tool hold + raw pass-through |
| Tool boundary | optional Claude Code reviewer | no tool-level gate | client hook + server policy | trajectory verdict | request, tool-result, tool-call and response checkpoints, plus an optional streaming review window |
| Policy | gateway config + input regex | gateway config + input moderation | attribute rules / LLM / manual | fine-tuned guard model | typed rules, four phases, model veto only |
| Audit and evidence | request/attempt lineage | append-only admin audit + usage logs | rule matches and audit metadata | step/sentence attribution | signed event chain + timeline + source-to-sink flows |
| Deployment | Go service + database/cache options | Go + PostgreSQL + Redis | client SDK + server | model inference service | one Go process + PostgreSQL |

The table compares design centers without ranking them. AxonHub has broader provider coverage and routing. Sub2API has more complete account pools, billing and operations. AgentGuard places policy next to the agent runtime. AgentDoG's diagnostic strength comes from trained models, which SafeAgent does not attempt to reproduce.

SafeAgent places protocol conversion and the behavior boundary in one process and one observation model, accepting a narrower protocol scope in exchange for consistent online enforcement and audit.

## What SafeAgent borrows

- **From AxonHub**: provider catalog and endpoint presets, same-format raw pass-through, and visible request/attempt lineage. The OpenAI-shaped IR and the client-specific reviewer remain AxonHub choices.
- **From Sub2API**: strict field and terminal-event handling, usage normalization, and the idea of an immutable management audit. Account pools, billing and pairwise conversion remain Sub2API's.
- **From AgentGuard**: the runtime attributes principal, tool, arguments, target and history, and a policy view that covers a tool chain. The client SDK, DSL and manual review queue remain with AgentGuard.
- **From AgentDoG**: the 8/14/10 taxonomy, the trajectory as the unit of analysis, and the separation of diagnosis from enforcement. The trained guard model and benchmark remain AgentDoG's; SafeAgent makes no reproduction claim.

## Claims not made

- This page does not claim that SafeAgent is more accurate, faster or broader than any reference project; no matched benchmark was run.
- It does not claim that any design originates in SafeAgent. Unified IRs, gateway-level policy and trajectory taxonomies all have precedents; SafeAgent's contribution is an engineering combination and a boundary choice.
- External facts apply to the versions checked on the stated date. Licenses can change; a derived work should re-check them.
- "No such capability" means it was not found in that snapshot's code and docs, not that a future version will never provide it.

## References

- AxonHub, snapshot `1.0.0-rc.4`, local source tree, README and the `llm/` module.
- Sub2API, snapshot `0.2.24`, local source tree, README and backend services.
- Jiaqi Luo et al., _AgentGuard: An Attribute-Based Access Control Framework for Tool-Use LLM-Based Agent_, arXiv:2605.28071v1, 2026.
- Dongrui Liu et al., _AgentDoG: A Diagnostic Guardrail Framework for AI Agent Safety and Security_, arXiv:2601.18491v2, 2026.

## Related

- [Architecture](architecture.md)
- [Protocol conversion](protocol-conversion.md)
- [Agent safety](agent-safety.md)
- [Verification](verification.md)
