# Glossary

One English term per concept. The Chinese column records the wording the human documents use, or the wording reserved for a term they do not mention yet.

| English | Chinese | Meaning |
| --- | --- | --- |
| gateway | 网关 | The process and HTTP surface that admits, converts, checks and records agent traffic. |
| call | 调用 | One admitted client request, including its attempts, response, decisions and audit events. |
| operation | 操作 | The endpoint family a call addressed: generation, count_tokens, models, model, Responses lifecycle, embeddings or moderations. |
| purpose | 用途 | Why a generation call was issued: chat, subagent, compaction, title, description, review, memory, suggestion and so on. |
| purpose source | 用途来源 | Whether the purpose was declared by the client, inferred from the request or defaulted. |
| lane | 通道 | The role of a call: main, side or auxiliary. It decides conversation membership, diagnosis and counting. |
| outcome | 结果 | The terminal reason of a call, independent of whether the HTTP exchange completed. |
| resolution | 解析 | How a model name matched a route: exact, alias, explicit header or project-default fallback. |
| provider | 供应商 | A stored protocol catalog: endpoint specs, capabilities, compatibility and discovery. |
| channel | 渠道 | One upstream connection: provider, host, credentials, limits, timeouts and enabled flag. |
| model | 模型 | One global catalog entry: provider, upstream name, kind, formats derived from the endpoints, and prices. |
| route | 路由 | A project-facing model name plus its aliases and ordered targets. |
| target | 目标 | One route candidate: channel, model, optional format, priority, weight and output limit. |
| alias | 别名 | An additional public model name that resolves to a route; a route rename records the old name as one. |
| impact | 影响 | The routes and projects a channel or model removal touches, reported before confirmation. |
| degraded | 降级 | A stored configuration problem that reduces routing instead of preventing startup. |
| upstream | 上游 | The model provider or endpoint a channel points at. |
| upstream model | 上游模型 | The provider's model name after endpoint alias mapping at call time; stored as a call snapshot. |
| public model | 公开模型 | The model name an agent sends; it is resolved by a route. |
| attempt | 尝试 | One upstream request to one channel, catalog model, format and key. |
| session | 会话 | A grouping of calls from one agent key; it is not an identity. |
| conversation | 会话记录 | The stored view of the calls that share a session. |
| trace | 轨迹 | The protocol-neutral observation of one call and its bounded ancestors. |
| trajectory | 行为轨迹 | The console projection of a conversation as typed nodes, edges and replay steps. |
| turn | 轮次 | One role's contiguous items in a trace. |
| item | 条目 | One message, reasoning block, tool call, tool result, media or native object. |
| locator | 定位符 | The stable identity of an item: call id, section, ordinal, inner path and optional tool id. |
| origin | 来源 | Where content entered: system, developer, user, assistant, tool result, tool description or environment. |
| trust | 信任级别 | Whether content may be treated as instructions. Tool output and tool descriptions are untrusted. |
| sink | 去向 | What a tool call can do: external send, shell exec, file read, file write, credential access, payment, none or unknown. |
| finding | 发现 | One deterministic observation with a code, severity, evidence and optional three-axis labels. |
| flow | 关系 | An edge from external content to a later side-effecting tool call inside a call. |
| decision | 决策 | One online policy or decision-model result stored with the call. |
| checkpoint | 检查点 | A point where online policy runs: request, tool call, tool result or response. |
| tool hold | 工具缓冲 | Holding a complete tool call until its verdict is ready. |
| stream review | 流式预审 | Buffering a bounded leading window of a streaming response and running the response checkpoint before the first byte. |
| loss | 损失 | A field or semantic the target format cannot carry, recorded with a code and class. |
| repair | 修复 | A request invariant restored before conversion, recorded in the ledger. |
| redaction | 脱敏 | Replacing sensitive values before egress and restoring them in the response. |
| value id | 确定性标识 | The deterministic digest that identifies one masked value across a project. |
| shown view | 脱敏视图 | The redacted text, with byte spans marking every placeholder. |
| redaction index | 脱敏索引 | The conversation projection that groups occurrences by value id. |
| evidence | 证据 | A byte range, locator and display-safe quote supporting a finding. |
| verdict | 判定 | `safe`, `unsafe` or `unknown`, with coverage attached. |
| coverage | 覆盖 | Whether every requested detector completed; incomplete coverage cannot be `safe`. |
| analysis | 分析 | The analysis layer: detectors, findings, coverage and a verdict, and the stored analysis record it produces. |
| diagnosis | 诊断 | The asynchronous worker that rebuilds a finished main call and runs the analysis on it. |
| signed audit chain | 签名审计链 | Per-event HMAC chaining plus a per-call head and event count. |
| timeline | 时间线 | The ordered, metadata-only view of a call's events. |

## Related

- [Agent guide](README.md)
- [Human architecture](../human/architecture.md)
