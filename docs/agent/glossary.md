# Glossary

| English | Chinese | Meaning |
| --- | --- | --- |
| trajectory | 轨迹 | One delegated task, represented by one event chain. |
| state transfer | 状态转移 | A boundary crossing recorded as an event. |
| call | 调用 | An interval inside a trajectory, between a model action proposal and its execution or terminal decision. |
| event | 事件 | One immutable, signed entry in `trajectory_events`. |
| segment | 内容片段 | A content-addressed piece of text or JSON with integrity/confidentiality labels. |
| blob | 内容块 | Encrypted bytes shared by segments with the same hash. |
| edge | 关系边 | A causal/provenance/authorization relation between nodes. |
| gate | 关卡 | An online enforcement point: ingress, action or release. Recovery is projected rather than enforced online. |
| principal | 主体 | The authenticated agent key at admission. |
| lane | 通道 | `main`, `side` or `auxiliary`; decides trajectory and metrics membership. |
| purpose | 用途 | Why a request exists (chat, subagent, compaction, memory, ...). |
| trust | 信任 | Segment integrity: trusted, untrusted or unknown. |
| confidentiality | 机密性 | Segment confidentiality: public, private or user_identity. |
| capability | 能力 | A scoped permission to act on a resource, with attenuation. |
| obligation | 义务 | A temporal requirement with pending/satisfied/violated/expired states. |
| approval | 批准 | A single-use event bound to an invocation hash. |
| invocation hash | 调用哈希 | SHA-256 of tool name, exact arguments and target. |
| finding | 发现 | A deterministic or semantic security result tied to an event. |
| invariant | 不变量 | I1-I10 properties evaluated by the threat engine. |
| coverage | 覆盖度 | Observed boundaries divided by expected boundaries plus capture gaps. |
| checkpoint | 检查点 | A signed Merkle root over an event range. |
| evidence bundle | 证据包 | A portable offline-verifiable package for a trajectory range. |
| projection | 投影 | A rebuildable table derived from the event log (`calls`, `findings`, summaries). |
| change | 变更 | A `policy.change`, `model.change` or `tool.registry.change` event. |
| capture heartbeat | 采集心跳 | Recorder liveness and sequence range used for gap detection. |

## Related

- [Model](model.md)
- [Threat](threat.md)
- [Storage](storage.md)
- [Audit](audit.md)
