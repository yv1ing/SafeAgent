# Storage

`internal/storage/repository/schema.sql` is the only DDL source. Bun structs are CRUD mappings, not a migration mechanism. The schema fingerprint is stored in `system_state`; a mismatch stops startup and the database must be recreated. A database with tables but no fingerprint is also refused. There is no migration path and no retained old data.

## Tables

| Group | Tables |
| --- | --- |
| identity | `users`, `sessions`, `projects`, `project_members`, `agent_keys` |
| routing | `channels`, `channel_keys`, `model_routes` |
| redaction | `redaction_rules`, `redaction_snapshots` |
| policy | `policy_versions` |
| audit | `calls`, `events`, `call_tools`, `conversations`, `decisions` |
| analysis | `analyses`, `analysis_findings`, `analysis_flows`, `analysis_batches`, `analysis_batch_calls`, `analysis_jobs` |
| decision model | `decision_model_configs`, `decision_model_usage` |
| metrics | `traffic_rollups` |
| operations | `operation_logs`, `quota_usage`, `system_state` |

## Ownership and cascades

- A project owns its keys, routes, rules, policy versions, calls, analyses, batches and traffic rollups. Most project-owned rows cascade from `projects`; `calls.agent_key_id` also cascades so the order of the two project cascades cannot make the delete fail.
- A call owns its events, tool index, redaction snapshot, decisions and analyses. Deleting the call removes all of them in the database.
- `analyses` owns findings and flows. A failed analysis is a row, not an absent one.
- `analysis_jobs` is the outbox: a job is written in the call's seal transaction with a dedupe key, so a crash between seal and enqueue does not lose the work. A batch and its jobs are written in one transaction.

## Identity and idempotency

- `analyses` has a unique key on `(call_id, input_hash, detector_version, decision_model)`. A retried job or a repeated re-analysis returns the existing row with its findings and flows.
- `calls.latest_analysis_id`, `analysis_state`, `finding_count` and `max_severity` are a materialized projection for listings. A later analysis updates it only when it is newer.
- `policy_versions` stores every save; the highest `version` is active and the project pointer is updated in the same transaction. Saves lock the project row before choosing the next version.
- `decision_model_usage` holds the daily token total reserved and settled by the decision model.
- `analysis_batches` records the kind, progress and failures of a grouped re-analysis. It never merges findings.

## Rollups and indexes

`traffic_rollups` is the monitor read model. It is recomputed from `calls` for hour and day buckets; the lookback is aligned to a bucket edge, so a partial bucket is never overwritten with a truncated count. The `1d` window recomputes only the last seven days, so a one-minute tick does not scan years. Project statistics, finding views and model views read `calls` and the stored analyses directly, so no rollup tables exist for them.

Indexes cover the paths that grow with traffic: project and agent-key call listings, analysis identity, flows by analysis, agent and call, batch and job maintenance, and the monitor series. The index list is part of `schema.sql`; adding an index is a schema change and recreates the database.

## Retention

Pruning runs in batches of 1,000 rows. Calls older than the project's `retention_days` are deleted with their analyses, findings, flows, decisions, snapshots and tool index; the child rows go with the call cascade, so there is no unbounded standalone delete. Policy history and decision model configuration last for the project's lifetime. `analysis_batches` and `traffic_rollups` follow the project retention window; `operation_logs` use the configured operation-log retention and `quota_usage` keeps seven days. Finished, failed and canceled jobs are removed after seven days; pending and running jobs are not pruned by age.

## Secrets

Audit payloads use event schema version 3: the request and response are stored as item/part structures, together with the effective instructions and tools, the agent-key principal snapshot and the loss/repair ledger. Raw values are stored as sent. Read access is role-based: members see the redacted view, administrators can request raw events, and that request is written to the operation log. Channel keys and the decision model key are sealed with AES-256-GCM under the instance key; only masked prefixes or suffixes are returned by the API. An unreadable channel key is skipped and logged; it is never sent to a provider. Extra channel headers and forwarded-header names are not sealed, so a secret placed in a header is stored in plaintext and should be treated accordingly.

## Related

- [Analysis](analysis.md)
- [Development](development.md)
- [Agent guide](README.md)
