# Acceptance lab

`testenv/` is a disposable environment for live protocol, agent, security and console acceptance. PostgreSQL, four agent CLIs and a headless browser run in Docker; the gateway runs on the host so a change can be rebuilt without an image build. It is not a product deployment.

The [acceptance lab guide](../docs/agent/testenv.md) covers the full environment contract, suite mechanics and troubleshooting.

## Topology

| Component                          | Runs in                   | Address                     |
| ---------------------------------- | ------------------------- | --------------------------- |
| Gateway and console                | host                      | `0.0.0.0:8080`              |
| PostgreSQL 17                      | `database` service        | `127.0.0.1:55432`           |
| Claude Code, Codex, Gemini CLI, Pi | `agents` image            | `host.docker.internal:8080` |
| Playwright and Chromium            | `browser` image           | `host.docker.internal:8080` |
| Fault upstream                     | host, during `lab-faults` | `127.0.0.1:18080`           |

Only the database port is published. Lab channels set `allow_private_network=true` because Docker resolves external names to its fake-IP range; that exception is test-only.

## Quick start

```sh
cp testenv/env.example testenv/.env   # set a real upstream per format
make lab-setup                         # build, start, create the project, models and routes
make lab-acceptance                    # every suite in order, then stop the lab
```

At least one real provider format is required. A format with a URL but no key or model stops setup; a format with no URL is skipped, and the matrix and agent suite size themselves to the routes that exist. Set `SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` for every configured format when you run the performance suite or the full acceptance run.

`make lab-acceptance` runs `verify`, integration, protocol, auxiliary, agents, policy, analysis, security, faults, browser, the quick performance smoke and audit verification. On the first failure it stops and leaves the lab running. A successful run stops the lab and removes `runtime`. Set `KEEP_LAB=1` to retain it.

Individual suites:

| Command | Scope |
| --- | --- |
| `make lab-protocol` | every client format against every configured channel format, stream and JSON |
| `make lab-auxiliary` | models, counting, Responses lifecycle and the Responses WebSocket |
| `make lab-agents` | four agent CLIs against every configured route, plus a multi-step task on each native route |
| `make lab-policy` | request, tool-result and tool-call blocks through every client and route |
| `make lab-analysis` | asynchronous diagnosis, redaction hits, timeline and audit integrity through every client and route |
| `make lab-security` | invalid keys, project isolation, rate and token limits, console roles, stream review permissions and raw-event access |
| `make lab-faults` | failover, truncation, tool holds, stream review, audit projection, auxiliary model mapping, header forwarding and default limits |
| `make lab-browser` | every console page at desktop and mobile widths, plus desktop CRUD flows |
| `make lab-integration` | repository tests on a separate database in the lab |
| `make lab-audit` | verify every stored call audit chain on the lab database |
| `make lab-load` | native and cross-format performance comparison |

`make lab-restart` rebuilds and restarts the gateway; `SKIP_WEB=1` reuses `web/dist`. `make lab-down` removes the containers, volume and runtime files; `PURGE=1` also removes the lab images.

## Evidence

The suites write to `testenv/runtime`: `acceptance/matrix.json`, agent logs and answers, screenshots and `report.json`, and `load/<timestamp>/`. Review it before sharing; it may contain generated keys or model output.

`testenv/.env` holds provider keys, and `testenv/runtime` holds the administrator password, agent keys and the instance key. Do not expose port 8080 beyond the development host. The fixed database password and the private-network exception are test-only and must not be copied into a deployment.

## Related

- [Acceptance lab guide](../docs/agent/testenv.md)
- [Development](../docs/agent/development.md)
- [Verification method](../docs/human/verification.md)
