# Storage

## One database, one writer

PostgreSQL 17 is the only durable store. One process owns a database through a session advisory lock; a second process refuses to start. The schema is a single baseline in [internal/storage/repository/schema.sql](../../internal/storage/repository/schema.sql). There is no migration path: a database whose fingerprint differs is refused and must be recreated.

## Source and derived tables

`trajectory_events` is the only write model. Content and the projector's materialised views are stored separately:

- `trajectory_events`: the append-only log, one chain per trajectory (source).
- `segments`: content-addressed references with integrity/confidentiality labels (content).
- `blobs`: encrypted bytes, deduplicated globally by content hash (content).
- `edges`: the causal graph (derived).
- `trajectory_states`: the materialised reducer state, state hash and head event (derived).
- `checkpoints`: signed Merkle roots over the event range (derived).

Every other runtime table is a **projection** or configuration. Projections are rebuildable from the log and are never the authority for an authorization or audit decision:

- `calls`: a call interval, its metrics, tool ids, response id and latest finding summary;
- `findings`: one row per event/check/fingerprint;
- `trajectory_summaries`: listing and lifecycle;
- `traffic_rollups`: pre-aggregated traffic.

## Configuration

Configuration tables are ordinary operator-owned state:

- `users`, `sessions`, `projects`, `project_members`, `agent_keys`;
- `providers`, `channels`, `channel_keys`, `models`, `channel_capability_overrides`;
- `model_routes`, `route_aliases`, `route_targets`;
- `redaction_rules`, `policy_versions`;
- `decision_model_configs`, `decision_model_usage`;
- `operation_logs`, `quota_usage`, `system_state`.

Deleting a project removes its trajectory rows in one transaction; retention prunes old trajectories. An evidence bundle is a snapshot: exporting it does not pin the rows against retention.

## Append transaction

`repository.Append` runs in one transaction:

1. lock the `trajectory_summaries` row;
2. read the last event sequence and hash;
3. insert new blobs and upsert segments, strengthening a label when the same content arrives with a stronger one;
4. assign contiguous sequence numbers, canonicalise payloads and HMAC-sign each event;
5. insert `trajectory_events`;
6. reduce the state and accumulate findings, edges and call updates;
7. upsert `trajectory_states`, `calls` and `findings`, insert `edges`;
8. recompute each call's finding count and maximum severity from `findings`;
9. recompute the summary counts, coverage, risk and lifecycle state;
10. create a signed cumulative `checkpoints` row over every event of the trajectory.

A failure rolls the whole batch back. The gateway treats an append failure as a gate failure and does not release the action. The batch project must match the trajectory's project, and an approval grant or denial is rejected inside the same transaction when the approval is no longer pending.

## Projections

The projector is deterministic. It never reads a codec or a network. Given the same event range it produces the same state hash, finding fingerprints and edges. `RebuildTrajectory` deletes the projection rows, replays the log and reinserts them, including gate-check findings; it never modifies `trajectory_events`.

Call rows are keyed by call id with `trajectory_id` and `call_seq`. The call projection carries:

- classification axes (operation, purpose, lane), formats, route and channel;
- state, outcome, error and timing;
- token usage, cost and conversion losses;
- redaction counts/labels and finding count/max severity;
- session, parent call, conversation hash, tool call ids and response id.

## Retention

The maintenance loop prunes every minute in bounded batches and rebuilds the traffic rollups every five minutes:

- `trajectory_events`, `segments`, `edges`, `findings`, `checkpoints`, `trajectory_states`, `trajectory_summaries` and `calls` follow the project retention window;
- auxiliary calls use `auxiliary_retention_days`, or the main window when zero;
- blobs are removed when no segment refers to them;
- operation logs use `operation_log_days`;
- `traffic_rollups` is recomputed from `calls` for the last 90 hourly and 31 daily buckets.

A trajectory whose events were pruned reports `incomplete`, not `broken`. Checkpoints are pruned with their trajectory; there is no separate audit window.

## Recovery

On startup:

- the instance lease is acquired before any schema work;
- the schema fingerprint is verified;
- quota reservations are cleared;
- running calls are marked interrupted and a `trajectory.end` event is appended to their trajectories;
- the admin account is bootstrapped if no administrator exists;
- provider seeds, health and policy compilation are checked.

## Related

- [Model](model.md)
- [Audit](audit.md)
- [Providers](providers.md)
- [Development](development.md)
