# Acceptance lab

The acceptance lab is a disposable environment for checks that need a real provider, real protocol clients and a headless browser. It is not a product deployment. PostgreSQL, agent CLIs and the browser run in Docker; the gateway runs on the host so a change can be rebuilt without an image.

## Topology

```text
host gateway :8080
  ├── PostgreSQL :55432 (Docker)
  ├── provider upstreams configured by testenv/.env
  ├── agent CLIs (Docker)
  └── Playwright browser (Docker)
```

The database is recreated by `FRESH=1 make lab-setup`; a schema change requires a fresh database because there is no migration.

## Layout

- `testenv/lib`: shared shell and Node clients.
- `testenv/scripts`: setup, restart, suite runner, integration and teardown.
- `testenv/suites`: shell acceptance suites.
- `testenv/protocol`: Node protocol suites.
- `testenv/agents`: agent CLI images and configs.
- `testenv/browser`: Playwright console suite.
- `testenv/load`: four-protocol performance smoke.
- `testenv/faults`: fault-injection helper.

## Environment contract

`testenv/env.example` lists the variables. At least one provider format needs an upstream URL and a route. A format without a route is skipped, and protocol suites size themselves to the routes that exist. `SAFEAGENT_ADMIN_PASSWORD` is written by setup and read by the console suites.

## Lifecycle

```sh
cp testenv/env.example testenv/.env
make lab-setup                 # build gateway and start the lab
make lab-restart               # rebuild and restart only the gateway
make lab-down                  # stop containers and remove runtime state
FRESH=1 make lab-setup         # recreate the database
```

## Suites

| Target | What it checks |
| --- | --- |
| `lab-agents` | Claude Code, Codex, Gemini CLI and Pi through every configured route |
| `lab-protocol` | client-by-channel matrix, streaming and non-streaming, classification and session trajectory |
| `lab-auxiliary` | model listing, counting, Responses lifecycle, embeddings/moderations and WebSocket |
| `lab-trajectory` | tool-loop session, stable event/call ids, edges, coverage and chain verification |
| `lab-control-plane` | connect, soft delete/restore, impact, aliases and degraded configuration health |
| `lab-policy` | request, tool-call, tool-result and response enforcement |
| `lab-threat` | invariants, findings, coverage, redaction index and trajectory integrity |
| `lab-security` | authentication, quotas, isolation, console permissions and stream review |
| `lab-faults` | failover, truncation, tool holds, stream review and limits |
| `lab-browser` | Playwright console smoke and traffic flows |
| `lab-integration` | repository integration tests on a disposable database |
| `lab-load` | short four-protocol performance comparison |
| `lab-audit` | `verify-trajectory` on every stored trajectory |
| `lab-acceptance` | all suites in order, then cleanup on success |

A skip is reported with its reason and is not a pass. The acceptance runner keeps the lab on failure and writes `build/lab/acceptance.log`.

## Evidence and secrets

Screenshots, response bodies, agent logs and reports stay under `testenv/runtime`; the acceptance log survives `make lab-down`. `testenv/.env` holds provider keys and `testenv/runtime` holds the admin password, agent keys and instance key. Review both before sharing.

## Failure triage

- A 401 means the key or session cookie is wrong; re-run `make lab-setup`.
- A schema fingerprint error means the database was not recreated after a schema change; use `FRESH=1 make lab-setup`.
- A protocol case with no route is skipped; add the route or remove the format from the run.
- A `verify-trajectory` failure names the sequence and property; use `export-bundle` and `verify-bundle` to inspect it offline.
- A browser failure writes `report.json` and screenshots; the failed API call is in the page error list.

## Security boundary

The lab database password, private-network exception and fixed keys are lab-only. Port 8080 is bound to the development host. Do not copy `testenv/.env` or `testenv/runtime` into a deployment.

## Related

- [Development](development.md)
- [Audit](audit.md)
- [Storage](storage.md)
- [Verification](../human/verification.md)
