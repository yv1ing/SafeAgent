# Audit events and integrity

`internal/storage/auditevent` is the only package that knows the JSON stored in `events.payload`. The pipeline writes typed payloads, the repository moves rows, and every reader calls `auditevent.Decode`. The current schema version is 8; startup refuses a database whose stored events carry a different version.

## Event kinds

| Kind | Emitted when | Payload |
| --- | --- | --- |
| `request` | A call is admitted | Added items, request parameters, tools, redaction hits, ledger, principal, session source; for an auxiliary body endpoint, the redacted request body |
| `routing` | The public model resolves to a route | Route id and name, resolution, public model |
| `attempt` | An upstream attempt starts | Channel, catalog model, key, format and upstream model |
| `attempt_failed` | An attempt fails | Error code, class, channel and key |
| `attempt_first_token` | The first content arrives | Milliseconds since admission |
| `tool_call` | The model emits a complete tool call | ID, kind, name, arguments, blocked flag |
| `tool_result` | A request adds a tool result | Call ID, tool name, error flag |
| `decision` | A policy or decision-model decision is stored | ID, policy version, stage, action, rule, code, severity, reason |
| `blocked` | The call is refused | The blocking decision |
| `cancellation` | The client cancels the call | Code, reason, committed and truncated flags |
| `response` | A response is stored or withheld | Response, ledger, committed, partial, truncated and blocked flags |
| `quota_failed` | Usage settlement fails | Error |
| `terminal` | The call is sealed | State, code and ledger |

`routing` is written before the first attempt, so a replay can explain a fallback the call row only summarizes as `resolution=fallback`. A `response` event is written for a completed response, for every stream termination (committed, partial or failed), and for a response blocked by the response checkpoint. A blocked response is stored with `blocked=true` and is not retrievable.

The timeline in the console is built from the same events and carries metadata only. Each entry has a sequence, time, kind, category, label, status and optional code; raw text stays behind the role-based call view.

## Redaction hits

`request` events carry the call's own hits, with the identity and both views of each occurrence:

| Field                    | Meaning                                                            |
| ------------------------ | ------------------------------------------------------------------ |
| `Label`                  | the redaction rule label, which is also the placeholder prefix     |
| `Action`                 | `replace`, `block` or `alert`                                      |
| `ValueID`                | the deterministic placeholder digest (see [Model](model.md))       |
| `CallID`                 | the call that owns the stored item                                 |
| `Section`                | the item's section: `request`, `instructions`, `params` or `tools` |
| `Ordinal`                | the stored ordinal of the item inside its section                  |
| `Path`                   | the part inside the item, for example `tool_call.arguments`        |
| `Start`, `End`           | the raw byte range of the occurrence                               |
| `ShownStart`, `ShownEnd` | the same occurrence in the shown representation                    |
| `Secret`                 | whether the rule is a high-entropy secret rule                     |

`ValueID` is the deterministic placeholder digest (see [Model](model.md)), so the session redaction index deduplicates occurrences without reading the rule set again. A hit that belonged to an ancestor item is not written here: the ancestor recorded it when it was admitted.

## Hash chain

Each event carries `prev_hash` and `hash`. The chain is per call; the first event has an empty `prev_hash`.

```text
auditKey = HMAC-SHA256(instanceKey, "SafeAgent/audit-chain/key/v1")

material = canonicalJSON({
  alg: "HMAC-SHA256", v: 1,
  prev_hash, call_id, sequence, project_id, kind,
  schema_version: 8,
  created_at_micros,
  payload: canonicalJSON(sanitizedPayload)
})

hash = lowercaseHex(HMAC-SHA256(auditKey, material))
```

`canonicalJSON` sorts object keys, preserves array order, writes compact UTF-8 without HTML escaping and normalizes numbers. `\u0000` is replaced with the replacement character before signing because PostgreSQL `jsonb` cannot store a NUL escape. The recorder signs the same bytes it stores. The instance key is supplied by the configuration file; it is never stored in the database.

At seal, the call row receives `audit_head` (the last hash) and `audit_events` (the event count) in the same transaction as the terminal event. The anchor detects a deleted tail or a replaced call row even when the remaining prefix verifies. A call left `running` by a previous process is marked `interrupted` at startup with a signed terminal event (`process_restarted`), so it verifies as a complete chain rather than an incomplete one.

## Verification

`repository.VerifyCallAudit` checks the sequence, `prev_hash`, HMAC, the call anchor and the terminal state. It returns a report:

| Status        | Meaning                                                                     |
| ------------- | --------------------------------------------------------------------------- |
| `verified`    | The chain and the call anchor match.                                        |
| `in_progress` | The call is still running.                                                  |
| `incomplete`  | The chain verifies but the call anchor is missing or does not match.        |
| `broken`      | A sequence, payload, hash, terminal event or terminal state does not match. |
| `unavailable` | The instance key or the schema is not available.                            |

Run the offline check with the service configuration:

```sh
build/bin/safeagent -config config.toml verify-audit
build/bin/safeagent -config config.toml verify-audit -project <id> -since 24h
build/bin/safeagent -config config.toml verify-audit -call <call-id> -json
build/bin/safeagent -config config.toml verify-audit -limit 100
```

`-since` accepts a duration or a `YYYY-MM-DD` date; `-limit 0` verifies every selected call. The command only reads the schema state and uses the configured instance key; it never creates or migrates a schema. The console shows the same status on the call detail page.

## Ordering

The recorder assigns the sequence and signs under one mutex. `EventWriter` preserves per-call FIFO order: the background flusher takes pending rows in order, and seal waits for that call's in-flight rows before writing the remainder in the seal transaction. The chain is per call, so unrelated calls are not serialized.

## Retention

Events are deleted with their call by the retention job. Deleting a call removes its chain; the console reports a missing call as not found and never as a verified chain. The anchor is stored on the call, so an event row cannot be removed while leaving a verified call.

## Related

- [Storage](storage.md)
- [Analysis](analysis.md)
- [Development](development.md)
