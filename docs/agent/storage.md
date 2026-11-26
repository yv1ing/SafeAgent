# Storage

`internal/storage/repository/schema.sql` is the only DDL source. Bun structs are CRUD mappings, not a migration mechanism. The schema fingerprint is stored in `system_state`; a mismatch stops startup and the database must be recreated. A database with tables but no fingerprint is also refused, and startup rejects events whose `schema_version` is not the current audit schema version recorded in [Audit](audit.md).

## Tables

| Group | Tables |
| --- | --- |
| identity | `users`, `sessions`, `projects`, `project_members`, `agent_keys` |
| supply | `providers`, `channels`, `channel_keys`, `models`, `channel_capability_overrides` |
| routing | `model_routes`, `route_aliases`, `route_targets` |
| redaction | `redaction_rules`, `redaction_snapshots` |
| policy | `policy_versions` |
| audit | `calls`, `events`, `call_tools`, `conversations`, `decisions` |
| analysis | `analyses`, `analysis_findings`, `analysis_flows`, `analysis_batches`, `analysis_batch_calls`, `analysis_jobs` |
| decision model | `decision_model_configs`, `decision_model_usage` |
| metrics | `traffic_rollups` |
| operations | `operation_logs`, `quota_usage`, `system_state` |

## Supply

`providers` is one protocol and capability catalog. `source` is `builtin` or `custom`; built-in rows are seeded from the code catalog at startup and stay editable, custom rows are created by the connect flow. `endpoints` holds the serialized endpoint catalog: one entry per wire format with its path, auth style, capabilities, compatibility, discovery and auxiliary declarations. `seed_version` records which code seed a built-in row started from. Built-in rows are never deleted; a reset restores the shipped label and endpoint catalog.

`channels` stores one upstream connection: `provider_id`, name, base URL, headers, forwarded-header names, a default output limit, timeouts, the private-network flag, the enabled flag and a version. Formats are not stored on the channel; they are the provider endpoints for the model kind. `(provider_id, name)` is unique among live rows. Keys live in `channel_keys` and are sealed with the instance key; only the last four characters are kept in clear.

`models` is the global catalog. A row stores `provider_id`, `upstream_name`, an optional display name, the `kind` (`generation`, `embedding` or `moderation`), four prices, currency, enabled, version and soft-deletion time. Formats are derived from the provider endpoints and the kind, never stored. `(provider_id, upstream_name)` is unique among live rows.

`channel_capability_overrides` records a capability fact an upstream rejected for one channel, model and format. The gateway writes it; operators only read it.

## Routing

`model_routes` is a project-facing model name with an enabled flag, a version and soft deletion. `route_targets` are the candidate rows of a route: channel, model, an optional pinned format, priority, weight, an output limit, an enabled flag and the reason it was disabled. `route_aliases` maps an additional model name to a route: renaming a route writes the old name with `source=renamed`, and operators may add `manual` aliases. A project's `default_route_id` is the fallback for side and auxiliary calls whose model has no route.

Routes use soft deletion. Unique route names are enforced by a partial index over live rows, so a deleted name can be reused; targets and aliases are replaced with the route that owns them. Deleting a channel or model never physically removes it: the row is soft-deleted and disabled, and the route targets that point at it are removed or disabled in the same transaction, depending on the `detach_targets` flag. A route is then reported as degraded but keeps its identity and targets. Restoring a channel or model re-enables only the targets it had disabled itself. Deleting a route also removes its aliases and clears a project default that pointed at it; its targets stay so a restore keeps the failover order.

Every provider, channel, model and route row carries a `version`; an update names the version it read and a mismatch returns `version_conflict` instead of overwriting a concurrent edit. A route edit writes the route row, its targets, aliases and enabled flag in one transaction with the version check, so a half-applied edit cannot exist. Soft-deleting or restoring a channel or model advances the version of every route that referenced it, so a stale route edit cannot write back a target that points at the deleted row.

## Calls

A call records `client_format`, `operation`, `purpose`, `purpose_source`, `lane`, `client_transport`, `upstream_format`, `public_model`, `model_id`, `upstream_model`, `channel_id`, `route_id`, `resolution`, `state`, `outcome`, `error_code`, token and cost columns, `redaction_values`, `redaction_labels`, `session_id`, `parent_call_id`, `conversation_hash`, `message_offset`, the audit anchor, response storage fields and the analysis projection. It keeps channel and catalog model ids plus the resolved upstream model name; it never copies display names, so renaming or deleting a catalog entry does not rewrite history.

`operation` is the endpoint family; `purpose` is why a generation call was issued, and `purpose_source` records whether the gateway was told, inferred it or defaulted it. `lane` is `main`, `side` or `auxiliary`: only main and side calls belong to conversations, only main calls are diagnosed and counted in traffic metrics, and auxiliary calls keep an empty `session_id`. `state` is the lifecycle (`running`, `completed`, `failed`, `cancelled`, `interrupted`); `outcome` is the reason (`ok`, `blocked`, `rejected`, `client_error`, `upstream_error`, `unsupported`, `not_found`, `rate_limited`, `cancelled`), so an expected protocol refusal can be a completed call with a non-failure outcome. `resolution` is how the model name resolved: empty, `exact`, `alias`, `explicit` or `fallback`.

`redaction_values` is the number of distinct value ids the call introduced; `redaction_labels` is the sorted distinct labels. `message_offset` is the number of leading history items the parent already stored. `analysis_state` is `pending`, `running`, `completed`, `skipped` or `failed`; `latest_analysis_id`, `finding_count` and `max_severity` are the materialized listing projection.

## Audit columns

`events` stores `sequence`, `kind`, `schema_version`, `payload`, `prev_hash` and `hash`. The payload is canonical JSON and `schema_version` is the audit schema version recorded in [Audit](audit.md). Events are HMAC-chained per call. `request` events carry the items the call added, the request parameters, the tools and its redaction hits; each hit names the call, section, ordinal, inner path and its deterministic value id, plus the raw and shown byte ranges.

`calls` stores `audit_head` and `audit_events`, written in the seal transaction. They anchor the event chain even when a tail is removed.

`redaction_snapshots` keeps the rule set and its version and hash for one call, so a historical call is shown and re-analysed with the rules of its time. Every lane writes the snapshot.

## Ownership and cascades

- A project owns its keys, routes, aliases, rules, policy versions, calls, analyses, batches and traffic rollups. Most project-owned rows cascade from `projects`; `calls.agent_key_id` also cascades so the order of the two project cascades cannot make the delete fail.
- A call owns its events, tool index, redaction snapshot, decisions and analyses. Deleting the call removes all of them.
- A route owns its targets and aliases. A provider owns its channels, models and capability overrides; the supply rows are soft-deleted rather than removed, so a channel or model keeps its provider reference for history.
- `analyses` owns findings and flows. A failed analysis is a row, not an absent one.
- `analysis_jobs` is the outbox: a job is written in the call's seal transaction with a dedupe key, so a crash between seal and enqueue does not lose the work. A batch and its jobs are written in one transaction.

## Identity and idempotency

- `analyses` has a unique key on `(call_id, input_hash, detector_version, decision_model)`. A retried job or a repeated re-analysis returns the existing row with its findings and flows.
- `calls.latest_analysis_id`, `analysis_state`, `finding_count` and `max_severity` are a materialized projection for listings. A later analysis updates it only when it is newer.
- `policy_versions` stores every save; the highest `version` is active and the project pointer is updated in the same transaction. Saves lock the project row before choosing the next version.
- `decision_model_usage` holds the daily token total reserved and settled by the decision model; `quota_usage` holds the project's own daily token reservation and usage.
- `analysis_batches` records the kind, progress and failures of a grouped re-analysis. It never merges findings.

## Single writer and recovery

PostgreSQL is the only coordination point, so a second process must not use the same database. Startup takes a session-scoped advisory lock before it prepares the schema, seeds the providers or serves traffic; a failed acquisition stops with `another SafeAgent instance owns this database`. The lock lives on one connection, is checked every five seconds, and losing it stops the process instead of letting two writers diverge. Shutdown runs in a fixed order: stop accepting and drain in-flight calls, flush the audit queue, then release the lock.

Startup repair runs before the first request:

- `ResetReservations` clears the daily quota reservations left over from a previous process, so a crash does not strand quota.
- `InterruptRunning` marks every call left in `running` as `interrupted`. It derives the next event sequence and previous hash from the stored events, appends a signed `terminal` event (`state=interrupted`, `code=process_restarted`) and updates the call anchor in one transaction, so the chain stays valid even when the flusher only persisted part of the events before the crash. A call interrupted this way is not analysed.
- The diagnosis worker recovers expired job leases, re-enqueues sealed main calls that never produced an analysis, and recomputes the metric rollups. A worker heartbeats its job lease; when the lease has been recovered by another worker, the heartbeat returns `ErrLeaseLost` and the worker stops instead of analysing the call twice.

## Rollups and indexes

`traffic_rollups` is the monitor read model. It is recomputed from main-lane calls for hour and day buckets; the lookback is aligned to a bucket edge, so a partial bucket is never overwritten with a truncated count. The hourly window recomputes 90 days and the daily window 31 days, so a one-minute tick does not scan all history. Project statistics, finding views and model views read `calls` and the stored analyses directly.

The finding aggregates join `analysis_findings` to `calls.latest_analysis_id`, so a call that was analysed twice is counted once. The monitor series exposes `duration_p95_max`, the maximum of the pre-aggregated per-channel p95 values in a bucket. A percentile cannot be merged from percentiles, so this is not the p95 of the whole bucket; the per-project statistics compute a true `duration_p95` from `calls` instead. The name says what the number is.

Indexes cover the paths that grow with traffic: project and agent-key call listings including the lane dimension, analysis identity, flows by analysis, agent and call, batch and job maintenance, and the monitor series. The index list is part of `schema.sql`; adding an index is a schema change and recreates the database.

## Maintenance and retention

One maintenance loop runs once a minute and deletes in batches of 1,000 rows. Auxiliary calls are removed after the project's `auxiliary_retention_days`; main and side calls after `retention_days`, together with their analyses, findings, flows, decisions, snapshots and tool index via the call cascade. Conversations with no remaining calls are removed. Policy history and the decision-model configuration last for the project's lifetime. `analysis_batches` and `traffic_rollups` follow the project retention window; `operation_logs` use the configured operation-log retention; `quota_usage` and `decision_model_usage` keep seven days. Finished, failed and cancelled jobs are removed after seven days; pending and running jobs are not pruned by age. Audit chains of retained calls stay verifiable until the call itself is pruned.

## Related

- [Model](model.md)
- [Audit](audit.md)
- [Analysis](analysis.md)
- [Providers](providers.md)
