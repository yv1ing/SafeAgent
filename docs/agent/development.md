# Development

## Toolchain

- Go 1.27
- Node 22+ and npm
- PostgreSQL 17 for integration and acceptance
- `make deps` installs the locked Go modules and frontend dependencies

## Commands

| Command | Purpose |
| --- | --- |
| `make generate` | regenerate `web/openapi.json` and the generated frontend client |
| `make format` | gofmt, Prettier and documentation formatting |
| `make check` | contract freshness, staticcheck, gofmt, vet, frontend types and formatting |
| `make test` | Go race tests and frontend unit tests |
| `make integration` | repository integration tests against `SAFEAGENT_TEST_DATABASE` |
| `make lab-setup` | build and start the disposable acceptance lab |
| `make lab-protocol` | four-protocol client/channel matrix |
| `make lab-trajectory` | trajectory, tool loop and evidence verification |
| `make lab-policy` | request, tool and response enforcement |
| `make lab-threat` | threat checks, findings and coverage |
| `make lab-audit` | verify every stored trajectory chain |
| `make lab-acceptance` | run every suite and clean up on success |
| `make build` | build `build/bin/safeagent` with the console embedded |

## Contract

`go run ./cmd/safeagent openapi` produces the OpenAPI document from the transport DTOs. The generated client is checked in under `web/src/api/generated`. `make contract` fails when the committed document or client differs from the code. There is no hand-written API client.

## Tests

Unit tests beside packages cover:

- trajectory canonicalisation, chain verification and tamper detection;
- reducer state, labels, approvals and coverage;
- every invariant;
- evidence bundle build and offline verification;
- the harness recorder;
- policy compilation and evaluation;
- protocol codecs, streaming, routing and redaction.

Integration tests use a real PostgreSQL database and exercise the append transaction, projections, checkpoint verification and retention. Acceptance suites live in `testenv/`.

## Test layout

- Unit tests live beside the package that owns the behaviour. Use `package <name>` only when the test needs unexported state; otherwise use `package <name>_test` for the public contract.
- One test file covers one responsibility: `finding_test.go`, `engine_test.go`, `invariants_test.go` under `internal/threat`, for example. Do not create a new file for every exported function.
- Cross-package fixtures belong in `internal/testkit` and may only be imported from `_test.go` files.
- PostgreSQL tests use the `integration` build tag and `SAFEAGENT_TEST_DATABASE`; they do not run in `make test`.
- Frontend tests live in `web/tests` and cover pure functions and contract validation. Rendered flows belong in the Playwright suite.
- End-to-end gateway and console behaviour belongs in `testenv`; do not repeat acceptance coverage as unit tests.

## Package conventions

- Domain packages never import transports, services or storage. The architecture test enforces this.
- The deterministic security libraries are `trajectory`, `state`, `threat`, `policy`, `decision`, `evidence` and `harness`; none of them imports a service, a repository or the wire model. The ledger uses its own loss, repair and usage records, and the pipeline converts to and from the wire model.
- `internal/llm/model` depends only on the standard library.
- The console must not name the wire model; `internal/observation/adapter` is the only bridge.
- `internal/testkit` is for tests only.
- A new event kind must be added to `internal/trajectory`, the reducer, the projector, the console DTO and the testenv contract in the same change.

## Adding an event kind

1. Add the kind and payload type in `internal/trajectory`.
2. Teach `internal/state` what it does to labels, actions, obligations, approvals or coverage.
3. Add a threat invariant if the event can violate one.
4. Persist checks/findings in the projector.
5. Expose what the console needs; do not add a second traversal. Console and transport DTOs are generated from the same declaration.
6. Add a unit test and, when it crosses the gateway, a lab scenario.

## Related

- [README](README.md)
- [Storage](storage.md)
- [Testenv](testenv.md)
- [Roadmap](roadmap.md)
