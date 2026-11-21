# Audit events and integrity

`internal/storage/auditevent` is the only package that knows the JSON stored in `events.payload`. The pipeline writes typed payloads, the repository moves rows, and every reader calls `auditevent.Decode`.

## Event kinds

| Kind | Emitted when | Payload |
| --- | --- | --- |
| `request` | A call is admitted | Added items, request parameters, tools, redaction hits, session source |
| `attempt` | An upstream attempt starts | Channel, key, format and upstream model |
| `attempt_failed` | An attempt fails | Error code, class, channel and key |
| `attempt_first_token` | The first content arrives | Milliseconds since admission |
| `tool_call` | The model emits a complete tool call | ID, kind, name, arguments, blocked flag |
| `tool_result` | A request adds a tool result | Call ID, tool name, error flag |
| `decision` | A policy or decision-model decision is stored | Phase, action, rule, code, reason |
| `blocked` | The call is refused | The blocking decision |
| `cancellation` | The client cancels the call | Code, reason, committed and truncated flags |
| `response` | A response is stored or withheld | Response, ledger, committed, partial, truncated and blocked flags |
| `quota_failed` | Usage settlement fails | Error |
| `terminal` | The call is sealed | State, code and ledger |

`response` events are written for a completed response, a cancelled or truncated committed stream, and a response blocked by the response checkpoint. A blocked response is stored with `blocked=true` and is not retrievable.

## Hash chain

Each event carries `prev_hash` and `hash`. The chain is per call; the first event has an empty `prev_hash`.

```text
auditKey = HMAC-SHA256(instanceSecret, "SafeAgent/audit-chain/key/v1")

material = canonicalJSON({
  alg: "HMAC-SHA256", v: 1,
  prev_hash, call_id, sequence, project_id, kind,
  schema_version: 5,
  created_at_micros,
  payload: canonicalJSON(sanitizedPayload)
})

hash = lowercaseHex(HMAC-SHA256(auditKey, material))
```

`canonicalJSON` sorts object keys, preserves array order, writes compact UTF-8 without HTML escaping and normalizes numbers. `\u0000` is replaced with the replacement character before signing because PostgreSQL `jsonb` cannot store a NUL escape. The recorder signs the same bytes it stores.

At seal, the call row receives `audit_head` (the last hash) and `audit_events` (the event count) in the same transaction as the terminal event. The anchor detects a deleted tail or a replaced call row even when the remaining prefix verifies.

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
```

The command only reads the schema state and the instance secret; it never creates or migrates a schema. The console shows the same status on the call detail page.

## Ordering

The recorder assigns the sequence and signs under one mutex. `EventWriter` preserves per-call FIFO order: the background flusher takes pending rows in order, and seal waits for that call's in-flight rows before writing the remainder in the seal transaction. The chain is per call, so unrelated calls are not serialized.

## Retention

Events are deleted with their call by the project retention job. Deleting a call removes its chain; the console reports a missing call as not found and never as a verified chain. The anchor is stored on the call, so an event row cannot be removed while leaving a verified call.

## Related

- [Storage](storage.md)
- [Analysis](analysis.md)
- [Development](development.md)
