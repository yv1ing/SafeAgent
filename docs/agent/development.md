# Development

This document covers the build, configuration and test workflow, and the extension points that a change is most likely to touch.

## Requirements

- Go 1.27.4
- Node.js 22 or newer and npm
- GNU Make
- PostgreSQL 17 for integration and end-to-end runs

## Commands

```text
make deps           # go mod download + npm ci
make generate       # OpenAPI -> web/src/api/generated
make format         # format Go, frontend, testenv JavaScript and documentation
make check          # gofmt, staticcheck, go vet, TypeScript, prettier, contract diff
make test           # go test -race ./..., node --test
make verify         # run check and test
make build          # web build + embedded Go binary
make integration    # repository tests against SAFEAGENT_TEST_DATABASE
make lab-setup      # start the Docker acceptance lab; see testenv.md
make lab-protocol   # live conversion matrix
make lab-auxiliary  # models, counting, Responses lifecycle and WebSocket
make lab-agents     # four agent CLIs against every configured route
make lab-policy     # request, tool-result and tool-call enforcement
make lab-analysis   # diagnosis, redaction, timeline and audit-integrity acceptance
make lab-security   # authentication, quotas, isolation and console permissions
make lab-faults     # failover, truncation, tool holds, headers and default limits
make lab-browser    # desktop console CRUD and mobile page checks
make lab-audit      # verify every stored call audit chain on the lab database
make lab-load       # quick four-protocol performance smoke
make lab-acceptance # every lab suite in order
make security       # SBOM + govulncheck + npm audit
```

`cmd/safeagent` also provides `schema`, `openapi` and `verify-audit` subcommands. `schema` prints the baseline DDL, `openapi` writes the contract to the requested output path, and `verify-audit` reads the stored schema state and HMAC secret and checks call event chains without creating anything. Global flags precede the command: `safeagent -config config.toml verify-audit -call <id>`. `make lab-integration` runs the repository tests against a separate database in the lab.

## Configuration

`config.example.toml` lists every key. All keys except `database_url` have defaults.

| Key | Meaning |
| --- | --- |
| `listen` | HTTP listen address |
| `database_url` | PostgreSQL connection string; required |
| `secure_cookies` | mark the session cookie Secure; keep true outside loopback HTTP |
| `public_origin` | console origin behind a reverse proxy, for example `https://safeagent.example.com` |
| `max_request_bytes` | client request body cap |
| `max_response_bytes` | non-streaming response cap; streaming is bounded by `max_stream_bytes` |
| `max_stream_bytes` | upstream stream cap, at most 128 MiB (the SSE reader's own limit) |
| `stream_idle_seconds` | idle timeout between upstream frames |
| `trusted_proxies` | IPs or CIDRs whose forwarded client address may be trusted |
| `max_attempts` | total upstream attempts for one call, across keys and channels |
| `shutdown_seconds` | graceful shutdown budget |
| `operation_log_days` | operation-log retention |
| `tool_hold_bytes` | frames queued while a tool call waits for its verdict (1 MiB default) |
| `analysis_concurrency` | in-process diagnosis worker count |

Streaming response text is stored and checked for concealed Unicode asynchronously. A non-streaming response also passes the response checkpoint before it is written. Changing `schema.sql` changes the fingerprint. An existing database must be recreated; there is no migration path.

## Tests

| Package | Coverage |
| --- | --- |
| `internal/analysis` | finding identity, normalization, verdict rules, input hashing, analysis diff, typed CEL, finding predicates, axis derivation, policy document bounds and evaluation deadlines |
| `internal/analysis/builtin` | role split, schema checks, trailing JSON, flow relations, secret flow, redacted evidence |
| `internal/analysis/decision` | answer interpretation, both adapters against fake servers, private endpoint rejection, malformed answer rejection |
| `internal/observe` | origin and trust, sink inference, two views, stored-event replay, history expansion |
| `internal/pipeline` | admission, redaction, four checkpoints, tool hold, veto, streaming, failover, quotas, session linking, auxiliary endpoints, audit |
| `internal/service/diagnosis` | authorization-state exclusions and decision-state construction; veto behavior is covered by pipeline tests, and idempotency by repository integration tests |
| `internal/service/console` | permission rules, name bounds, password policy, administrator-only policy editing |
| `internal/transport/console` | forwarded headers, format defaults, project summary rendering and error labels |
| `internal/llm/model` | assembler invariants, journal reconstruction, signatures, losses, repairs |
| `internal/llm/codec` | 4×4 stream and JSON conversion, loss ledger, clean EOF, truncation, media, citations, headers, defaults |
| `internal/llm/provider` | built-in URL, format, auth, compatibility and discovery snapshots |
| `internal/llm/stream` | commit boundary, tool hold/release/reject, overflow, restoration, raw forwarding |
| `internal/app` | direct and transitive dependency boundaries |
| `internal/storage/auditevent` | canonical JSON, NUL sanitization, signing, chain verification, decoding and timeline |
| `internal/storage/repository` | schema documentation consistency, audit chain verification and tamper detection; integration tests require `SAFEAGENT_TEST_DATABASE` and are run by the operator |
| `testenv/load` | SSE and JSON response validation, percentile calculation and the performance smoke |

Browser acceptance is automated by `make lab-browser`; see the [acceptance lab](testenv.md).

## Acceptance lab

[`testenv/`](../../testenv/) is a disposable Docker lab for live protocol, agent, security and console acceptance. PostgreSQL, the agent CLIs and the Playwright browser run in containers; the gateway and the fault upstream run on the host so a change can be rebuilt without an image build. The complete topology, provider contract, suite expectations, performance smoke and troubleshooting guide is [the acceptance lab guide](testenv.md). The lab is not part of the product deployment; its runtime directory and `testenv/.env` are ignored.

## Contracts

Five contracts define what the rest of the system may rely on. Treat a change to one as a deliberate contract change.

| Contract | Authority | Consumers |
| --- | --- | --- |
| Observation model | `internal/observation` | detectors, policy, console read models |
| Canonical wire model | `internal/llm/model` | all four codecs, the stream engine and the gateway |
| Audit events and chain | `internal/storage/auditevent` | pipeline writer, every reader, `verify-audit`, console timeline |
| Console API | DTO tags and `internal/transport/console/openapi.go` | `web/openapi.json`, generated client, console |
| Policy document | `internal/analysis/policy` | policy editor, four online checkpoints |

The observation and audit contracts carry explicit version numbers. A stored event records the schema version it was written with. Startup checks the DDL fingerprint and refuses a database whose events carry a different `auditevent.SchemaVersion`; changing either contract therefore requires recreating the database. Every event is HMAC-chained; see [Audit](audit.md). The console contract is generated from Go types: `make generate` rewrites `web/openapi.json` and `web/src/api/generated`, and `make check` fails when either is stale. Neither generated file is edited by hand.

A protocol conversion that cannot carry a field must emit a `model.Loss` with a code and class, plus a path and direction when they are known. A new field is added to the canonical model first, then to each codec and to the matrix fixture. A same-format raw overlay may preserve the original bytes, but it does not replace a canonical mapping and an explicit loss for other targets.

## Conventions

- A package has one responsibility. `llm/model` describes the wire, `llm/codec` converts, `llm/provider` catalogs upstreams, `llm/stream` drives a response, `pipeline` orchestrates, `observation` models a call, `observe` adapts protocols, `analysis` judges, `storage` persists, `service` holds use cases and `transport` serves them.
- Package names are singular nouns that state the responsibility. Protocol names such as `messages` and `responses` keep the provider's spelling. A package that groups utilities is named for the group (`platform`, `storage`), not for a layer of the product.
- File names describe one topic in lower case; an underscore separates words only when the joined form would be ambiguous (`channel_keys.go`, not `channelkeys.go`). Exported identifiers use full words and the usual Go initialisms (`ID`, `URL`, `HTTP`).
- Do not add a second mechanism for an existing concept. If a field, table or endpoint has no reader, remove it; do not document it.
- DTOs use `View` for responses and `Input` for requests. Their `json` tags and `required` markers are the contract. `web/openapi.json` and `web/src/api/generated` are produced by `make generate` and are never edited by hand.
- Repository methods are verbs (`Create`, `Save`, `Prune`); domain types are nouns. Errors that a caller must distinguish are sentinel values or typed failures, not strings.
- Comments explain a non-obvious invariant or an external constraint. They do not restate the code.

## Adding a detector

1. Add the code to `analysis.CodeCatalogue` with its severity and allowed origins.
2. Extend `AxisFor` if the code needs a new mechanism or harm mapping. Leave an axis empty when it cannot be justified.
3. Implement `analysis.Detector` in `internal/analysis/builtin` and register it in `init`.
4. Use `analysis.NewFinding` so the fingerprint, axes and evidence stay consistent.
5. Add a unit test.

A detector that cannot finish must emit `analysis_incomplete`, not nothing. The console carries code labels in `web/src/i18n`; a new code falls back to the raw string until a label is added.

## Adding a policy field

1. Add the field to the struct in `internal/analysis/policy/policy.go` with a `cel` tag.
2. Add it to the activation in `activationFor`.
3. Document it in `policy.md` and add a compile test for the field name.

Policy values are server-side. Text arguments and targets are supplied in their redacted form when redaction changed them; the principal contains only authenticated key attributes.

## Adding a protocol field

Add the field to the canonical model, decode it in each codec that has it, encode it where an equivalent exists, and add an explicit loss where one does not. Extend the matrix fixture; raw overlays must not replace a canonical mapping. A new protocol also needs a gateway route in `internal/transport/http/gateway.go` and a codec registration in `pipeline.New`.

## Adding a provider

Add one file under `internal/llm/provider/builtin` returning a `provider.ProviderSpec`, and one line in `builtin.All()`. Declare only capabilities the official documentation guarantees; a compatibility flag belongs in `Compat`, not in a codec branch. Add the provider to the catalog snapshot test. The console gets the provider in its dropdown from the registry, and channel creation needs nothing beyond the provider id and a key (plus a base URL for a custom provider).

## Security review

Before delivery run `make security` and review:

- agent-key authentication, console session cookies, Origin and request-header checks;
- project and administrator checks in the service layer before any state change;
- raw payload access is administrator-only and written to the operation log;
- decision model endpoints reject private and carrier-NAT addresses unless explicitly allowed, and the dialer checks the address it connects to;
- channel keys and the decision-model key are sealed; extra headers are not sealed and must not be used for credentials;
- redaction placeholders are HMAC-derived and bound to the call namespace;
- request, response, stream, tool hold, worker and detector limits are in place;
- evidence never quotes a redacted value.

Raw audit data is plaintext by design. Access is controlled by role-based visibility and operation logging.

## Related

- [Storage contract](storage.md)
- [Analysis model](analysis.md)
- [Protocol layer](protocol.md)
- [Audit](audit.md)
- [Roadmap](roadmap.md)
- [Agent guide](README.md)
