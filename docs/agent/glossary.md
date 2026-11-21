# Glossary

One English term per concept, with the Chinese term used in `README_zh.md` and `docs/human/*_zh.md`.

| English | Chinese | Meaning |
| --- | --- | --- |
| gateway | 网关 | The process and HTTP surface that admits, converts, checks and records agent traffic. |
| channel | 渠道 | One upstream provider configuration: provider id, keys, endpoint overrides and limits. |
| upstream | 上游 | The model provider or endpoint a channel points at. |
| route | 路由 | The project-visible mapping from a public model name to one or more channel targets. |
| public model | 公开模型 | The model name an agent sends; it is resolved by a route. |
| upstream model | 上游模型 | The provider's model name after route mapping. |
| attempt | 尝试 | One upstream request to one channel and key. |
| call | 调用 | One admitted client request, including its attempts, response and audit events. |
| session | 会话 | A grouping of calls from one agent key; it is not an identity. |
| conversation | 会话 | The console view of the calls that share a session. |
| trace | 轨迹 | The protocol-neutral observation of one call and its bounded ancestors. |
| turn | 轮次 | One role's contiguous items in a trace. |
| item | 条目 | One message, reasoning block, tool call, tool result, media or native object. |
| origin | 来源 | Where content entered: system, developer, user, assistant, tool result, tool description or environment. |
| trust | 信任级别 | Whether content may be treated as instructions. Tool output and tool descriptions are untrusted. |
| sink | 去向 | What a tool call can do: send, execute, read, write, access credentials, pay or unknown. |
| finding | 发现 | One deterministic observation with a code, severity, evidence and optional three-axis labels. |
| flow | 关系 | An edge from external content to a later side-effecting tool call inside a call. |
| decision | 决策 | One online policy or decision-model result stored with the call. |
| checkpoint | 检查点 | A point where policy runs: request, tool call, tool result or non-streaming response. |
| tool hold | 工具缓冲 | Holding a complete tool call until its verdict is ready. |
| loss | 损失 | A field or semantic the target format cannot carry, recorded with a code and class. |
| repair | 修复 | A request invariant restored before conversion, recorded in the ledger. |
| redaction | 脱敏 | Replacing sensitive values before egress and restoring them in the response. |
| evidence | 证据 | A byte range and a display-safe quote supporting a finding. |
| verdict | 判定 | `safe`, `unsafe` or `unknown`, with coverage attached. |
| coverage | 覆盖 | Whether every requested detector completed; incomplete coverage cannot be `safe`. |
| analysis | 分析 | The asynchronous diagnosis of a finished call. |
| signed audit chain | 签名审计链 | Per-event HMAC chaining plus a per-call head and event count. |
| timeline | 时间线 | The ordered, metadata-only view of a call's events. |

## Related

- [Agent guide](README.md)
- [Human architecture](../human/architecture.md)
