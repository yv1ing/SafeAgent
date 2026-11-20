# Acceptance lab

`testenv/` is a disposable environment for live protocol, agent, security and console acceptance. PostgreSQL, four agent CLIs and a headless browser run in Docker; the gateway runs on the host so a change can be rebuilt and restarted without an image build. The lab is not a product deployment.

The detailed environment contract, suite mechanics and troubleshooting notes are in [the acceptance lab guide](../docs/agent/testenv.md).

## Topology

| Component                          | Runs in            | Address                     |
| ---------------------------------- | ------------------ | --------------------------- |
| Gateway and console                | host               | `0.0.0.0:8080`              |
| PostgreSQL 17                      | `database` service | `127.0.0.1:55432`           |
| Claude Code, Codex, Gemini CLI, Pi | `agents` image     | `host.docker.internal:8080` |
| Playwright and Chromium            | `browser` image    | `host.docker.internal:8080` |
| Fault upstream                     | host               | `127.0.0.1:18080`           |

Only the database port is published. The agent and browser containers reach the gateway through the Docker host gateway. Lab channels set `allow_private_network=true` because Docker resolves external names to its fake-IP range; that exception belongs to the lab only.

## Prerequisites

- Docker with Compose v2.
- Go 1.27.4, Node.js 22 or newer, GNU Make.
- `curl`, `jq` and `openssl`.
- At least one real provider format. The lab supports Chat Completions, Responses, Anthropic Messages and Gemini independently; configure the formats you can actually reach.

Each configured model must support the cases you enable. The core matrix needs tool calling; the optional cases need reasoning, vision or structured output.

## Configure

```sh
cp testenv/env.example testenv/.env
```

Set a URL, key and model for each available format. Shared `SAFEAGENT_UPSTREAM_KEY`, `SAFEAGENT_UPSTREAM_MODEL` and `SAFEAGENT_UPSTREAM_VENDOR` values act as fallbacks, so one account can serve several formats. A format with a URL but a missing key or model stops setup with an error; a format with no URL is skipped. The protocol matrix and agent suite size themselves to the routes that were created. A missing Gemini upstream leaves no Gemini channel route; the agent suite records its native Gemini deep task as skipped, and the missing channel coverage is a gap rather than a pass.

A provider that requires an extra header (for example an aggregator-specific key header) can use `SAFEAGENT_UPSTREAM_<FORMAT>_HEADERS_JSON`. A Messages channel can set `SAFEAGENT_UPSTREAM_MESSAGES_COUNT_TOKENS_URL`; a channel default limit is available through `SAFEAGENT_UPSTREAM_<FORMAT>_DEFAULT_MAX_OUTPUT_TOKENS`.

## Run

```sh
make lab-setup          # first run: build, start, create the project and routes
make lab-acceptance     # every suite in order, then stop the lab on success
```

`make lab-acceptance` runs `verify`, integration, protocol, agents, policy, analysis, security, faults, browser and the quick performance smoke. On the first failure it stops and leaves the lab running for inspection. A successful run stops the lab and removes `runtime`, including the evidence; set `KEEP_LAB=1` to retain it.

Individual suites are available while working:

| Command | Scope |
| --- | --- |
| `make lab-protocol` | every client format against every configured channel format, stream and JSON, core and enabled extension cases |
| `make lab-agents` | Claude Code, Codex, Gemini CLI and Pi against every configured route, plus a multi-step task on each native route |
| `make lab-policy` | request and tool-call blocking through every client format and route |
| `make lab-analysis` | asynchronous diagnosis and redaction hits through every client format and route |
| `make lab-security` | invalid keys, project isolation, rate and token limits, console roles and raw-event access |
| `make lab-faults` | deterministic failover, truncation, tool holds, header forwarding and default limits |
| `make lab-browser` | every console page at desktop and mobile widths, plus desktop create, edit, delete, revoke and disable flows |
| `make lab-load` | short relative performance smoke on every native route, plus a cross-format sample |

`make lab-restart` rebuilds the console and binary and restarts the gateway. `SKIP_WEB=1 make lab-restart` reuses `web/dist` when only Go code changed. `make lab-down` stops the lab and removes its containers, volume and runtime files; `PURGE=1 make lab-down` also removes the lab images.

## Evidence

The suites write to `testenv/runtime`:

- `acceptance/matrix.json` — protocol case results, including skipped cases and their reason;
- `logs/` — one log and one answer file per agent run, plus the resolved CLI versions;
- `screenshots/report.json` and the screenshots — console pages and desktop CRUD checks;
- `load/<timestamp>/` — the native comparisons and cross-format sample.

`testenv/runtime` is ignored and may contain generated keys or model output. Review it before sharing outside the lab.

## Security boundary

`testenv/.env` holds provider keys; `testenv/runtime` holds the administrator password and agent keys. The gateway listens on all interfaces so containers can reach it. Do not expose port 8080 beyond the development host. The fixed database password and the private-network exception are test-only and must not be copied into a deployment. Individual fault runs create global test channels because channels are disabled rather than deleted; `make lab-down` removes the lab database.
