# Development

Build, configuration, test workflow, contracts and the extension points a change is likely to touch.

## Requirements

- Go 1.27.4, Node.js 22 or newer with npm, GNU Make
- PostgreSQL 17 for integration and end-to-end runs

## Commands

```text
make deps           # go mod download + npm ci
make generate       # OpenAPI -> web/openapi.json and web/src/api/generated
make format         # format Go, frontend, testenv JavaScript and documentation
make check          # formatting, staticcheck, vet, typecheck, prettier and contract drift
make test           # go test -race ./... and the frontend unit tests
make verify         # make check + make test
make build          # web build + embedded Go binary in build/bin/safeagent
make run            # run the backend with CONFIG=config.toml
make dev            # backend and console dev server together
make integration    # repository tests against SAFEAGENT_TEST_DATABASE
make security       # dependency inventories, SBOMs and vulnerability reports
make clean          # remove build output
```

`cmd/safeagent` also provides `schema`, `openapi` and `verify-audit` subcommands. `schema` prints the baseline DDL, `openapi` writes the contract, and `verify-audit` checks stored event chains without creating or changing anything. Global flags precede the command: `safeagent -config config.toml verify-audit -call <id>`. The Docker acceptance lab has its own targets; see [testenv](testenv.md).

## Configuration

`config.example.toml` lists every key. `database_url` and `instance_key` are required; every other key has a default. Unknown keys are rejected, so the file and the struct cannot drift silently.

| Key                    | Meaning                                                         |
| ---------------------- | --------------------------------------------------------------- |
| `listen`               | HTTP listen address                                             |
| `database_url`         | PostgreSQL connection string; required                          |
| `instance_key`         | 32-byte standard-base64 key; required                           |
| `secure_cookies`       | mark the session cookie Secure; keep true outside loopback HTTP |
| `public_origin`        | console origin behind a reverse proxy                           |
| `max_request_bytes`    | client request body cap                                         |
| `max_response_bytes`   | non-streaming response cap                                      |
| `max_stream_bytes`     | upstream stream cap, at most 128 MiB                            |
| `stream_idle_seconds`  | idle timeout between upstream frames                            |
| `trusted_proxies`      | proxy IPs or CIDRs whose forwarded client address is trusted    |
| `max_attempts`         | total upstream attempts for one call                            |
| `shutdown_seconds`     | graceful shutdown budget                                        |
| `operation_log_days`   | operation-log retention                                         |
| `tool_hold_bytes`      | frames queued while a tool call waits for its verdict           |
| `analysis_concurrency` | in-process diagnosis worker count                               |

Changing `schema.sql` changes the fingerprint; an existing database must be recreated. There is no migration path. Stored events must also carry the audit schema version described in [audit](audit.md).

## Tests

Tests live beside the package they cover. `go test ./internal/<area>` runs one area; `internal/app` also holds the architecture and dependency tests plus the documentation contract tests. Repository integration tests require `SAFEAGENT_TEST_DATABASE` and run through `make integration` or the lab. The console tests are Node's `node --test`; browser acceptance is `make lab-browser`. The live protocol, agent, security and fault checks are the acceptance lab, not unit tests.

Test files follow the package they test. A unit test is `<topic>_test.go` beside the package, named after the topic it covers rather than the production file (`admission_test.go`, not `admission_internal_test.go`). A test that only exercises the exported API may use the external `_test` package; a test that needs unexported helpers stays in the package. There is no `_internal_test.go` suffix. Test helpers live with the topic tests that use them; a helper used by several topics belongs in a file named after the helper's topic. `internal/testkit` holds the shared in-memory fixtures and is test-only.

Two tests couple code to documentation and fail a refactor that forgets the docs:

- `internal/app/docs_test.go` checks that every top-level agent document ends with `## Related`, every Markdown link resolves, every backticked repository path exists, every backticked make target exists, human English/Chinese twins have the same section count, and the two READMEs have the same section count.
- `internal/storage/repository/schema_docs_test.go` checks that every table in `schema.sql` appears backticked in [storage](storage.md).

`internal/transport/console/openapi_test.go` pins the console endpoints that the generated client and the testenv scripts rely on.

## Contracts

Treat a change to one of these as a deliberate contract change.

| Contract | Authority | Consumers |
| --- | --- | --- |
| Observation model and item identity | `internal/observation` and its adapter | detectors, policy, diagnosis, console projections |
| Canonical wire model | `internal/llm/model` | all four codecs, the stream engine and the gateway |
| Audit events and chain | `internal/storage/auditevent` | pipeline writer, every reader, `verify-audit`, console timeline |
| Console API | DTO tags and `internal/transport/console/openapi.go` | `web/openapi.json`, generated client, console, testenv |
| Policy document | `internal/analysis/policy` | policy editor, four online checkpoints |
| Classification vocabulary | `internal/classify` | pipeline, storage entities and repository, console queries, diagnosis, gateway |

The observation model is a versioned adapter contract: `internal/observation/adapter` carries `AdapterVersion`, and every analysis records it beside the detector, catalogue and taxonomy versions, so a rebuilt trace can be compared with an older one. Startup does not check that version. The audit event contract does carry a version that startup checks: the DDL fingerprint must match and every stored event must be at the current audit schema version, so a change to that contract requires recreating the database. Every event is HMAC-chained; see [audit](audit.md). The console contract is generated from Go types: `make generate` rewrites `web/openapi.json` and `web/src/api/generated`, and `make check` fails when either is stale. Neither generated file is edited by hand.

A protocol conversion that cannot carry a field must emit a `model.Loss` with a code and class, plus a path and direction when known. Add the field to the canonical model first, then to each codec and to the matrix fixture. A same-format raw overlay may preserve original bytes, but it does not replace a canonical mapping and an explicit loss for other targets.

## Conventions

- File names describe one topic in lower case; an underscore separates words only when the joined form would be ambiguous (`stream_decode.go`, not `streamdecode.go`). Exported identifiers use full words and the usual Go initialisms (`ID`, `URL`, `HTTP`).
- DTOs use `View` for responses and `Input` for requests; their `json` tags and `required` markers are the contract. Generated files are never edited by hand.
- Repository methods are verbs (`Create`, `Save`, `Prune`); domain types are nouns. Errors a caller must distinguish are sentinel values or typed failures, not strings.
- Control-plane updates carry a version the caller read; a multi-row edit runs in one transaction; a semantic deletion reports an impact instead of being rejected outright. A configuration problem degrades at startup; a process prerequisite fails it.
- A use case takes an `access.Actor` and returns an `access.Failure`; transport maps the failure kind to HTTP and never repeats the authorization. `access.Translate` is the one place a repository sentinel becomes a service outcome.
- Do not add a second mechanism for an existing concept. If a field, table or endpoint has no reader, remove it instead of documenting it.
- Comments explain a non-obvious invariant or an external constraint; they do not restate the code.

## Adding a detector

1. Add the code to `analysis.CodeCatalogue` with its severity and allowed origins.
2. Extend `AxisFor` if the code needs a new mechanism or harm mapping; leave an axis empty when it cannot be justified.
3. Implement `analysis.Detector` in `internal/analysis/builtin` and register it in `init`.
4. Use `analysis.NewFinding` so fingerprint, axes and evidence stay consistent, and add a unit test.

A detector that cannot finish must emit `analysis_incomplete`, not nothing.

## Adding a policy field

1. Add the field to the policy struct with a `cel` tag.
2. Add it to `activationFor`.
3. Document it in [policy](policy.md) and add a compile test for the field name.

Policy values are server-side. Text arguments and targets are the redacted form when redaction changed them; the principal contains only authenticated key attributes.

## Adding a protocol field or provider

A protocol field: add it to the canonical model, decode it in each codec that has it, encode it where an equivalent exists, add an explicit loss where none exists, and extend the matrix fixture. A new protocol also needs a gateway route in `internal/transport/gateway/gateway.go` and a codec registration in `pipeline.New`.

A provider: add one file under `internal/llm/provider/builtin` returning a `provider.ProviderSpec` and one line in `builtin.All()`. Declare only capabilities official documentation guarantees; a compatibility quirk belongs in `Compat`, not in a codec branch. Extend the provider endpoint tests with the new format, auth and discovery behavior. A release that changes an existing seed should bump `providerSeedVersion`; a reset then restores the new catalog while an operator edit is preserved. After the seed changes, the built-in row must still load and the configuration health report must stay clean.

A control-plane entity or endpoint: add the use case in `internal/service/control` (transaction, version, permissions), the DTO and registration in `internal/transport/console`, the table and repository methods, then regenerate the contract with `make generate`. Add the new table to [storage](storage.md); the schema-docs test requires it.

## Security review

Before delivery run `make security` and review:

- agent-key authentication, console session cookies, Origin and request-header checks;
- project and administrator checks in the service layer, before any state change and before every diagnosis read; the transport handlers do not authorize;
- raw audit payloads are returned only to administrators; successful state-changing console operations are written to the operation log;
- decision-model and discovery endpoints reject private and carrier-NAT addresses unless explicitly allowed, and the dialer checks the address it connects to;
- channel keys and the decision-model key are sealed; extra headers are not sealed and must not be used for credentials;
- redaction placeholders are HMAC-derived and bound to the project namespace;
- request, response, stream, tool hold, stream review, worker, detector and trajectory budgets are in place;
- the instance key is loaded from configuration and never stored in the database;
- evidence never quotes a redacted value, and a redaction index never carries a raw value.

Raw audit data is plaintext by design; access is controlled by role-based visibility and operation logging.

## Related

- [Storage](storage.md)
- [Analysis](analysis.md)
- [Protocol](protocol.md)
- [Audit](audit.md)
- [Agent guide](README.md)
