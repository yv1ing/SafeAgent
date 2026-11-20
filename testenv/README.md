# Acceptance lab

`testenv/` provides a disposable environment for functional and performance acceptance. It runs PostgreSQL, four agent CLIs and a headless browser in Docker, and runs the gateway on the host so a backend or console change can be rebuilt without an image build. The lab is not part of a product deployment.

The detailed topology, suite mechanics, environment contract and troubleshooting notes are in [the acceptance lab guide](../docs/agent/testenv.md).

## Topology

| Component                          | Runs in            | Address                     |
| ---------------------------------- | ------------------ | --------------------------- |
| Gateway and console                | host               | `0.0.0.0:8080`              |
| PostgreSQL 17                      | `database` service | `127.0.0.1:55432`           |
| Claude Code, Codex, Gemini CLI, Pi | `agents` image     | `host.docker.internal:8080` |
| Playwright and Chromium            | `browser` image    | `host.docker.internal:8080` |

Only the database port is published. The lab creates its channels with `allow_private_network=true` because Docker resolves external names to its fake-IP range; that exception is for the lab only.

## Prerequisites

- Docker with Compose v2.
- Go 1.27.4, Node.js 22 or newer, GNU Make.
- `curl`, `jq` and `openssl`.
- A real provider key that works with the Chat Completions, Responses and Anthropic Messages endpoints. The performance suite does not use a fake upstream; it sends the same key to the provider for the direct baseline.

Fill `testenv/.env` with the key, the upstream model name and one endpoint for each wire format. The lab does not assume a provider; any account that exposes all three formats works.

## Setup

```sh
cp testenv/env.example testenv/.env   # fill the key, model and three endpoints
make lab-setup
```

`make lab-setup` starts PostgreSQL, builds the console and the embedded gateway, starts the gateway on the host, and creates the project, channels, routes, agent keys and workspaces. The lab project has no request or token quota, so the functional and performance suites measure the gateway rather than its limits; quota enforcement is covered by the repository integration tests. Generated credentials go to `testenv/runtime`; `testenv/.env` and `testenv/runtime` are ignored.

`FRESH=1 make lab-setup` removes the database volume, recreates the schema and creates all lab accounts again. Use it after a schema change or when setup reports that an administrator already exists. An agent image change needs a rebuild; run `PURGE=1 make lab-down` before the next setup, or build the agent services with Compose.

## Functional acceptance

Run the suites in the order below. The policy suite leaves blocked calls that the browser suite checks, and the agent suite leaves multi-call conversations that the browser suite walks. Run the lab on an otherwise idle host and do not run two suites at the same time.

| Command | Scope | Pass criterion | Evidence |
| --- | --- | --- | --- |
| `make verify` | Go race tests, frontend tests, formatting, types and generated contract | exit 0 | command output |
| `make lab-integration` | repository integration tests on a separate database in the lab | exit 0 | `go test` output |
| `make lab-protocol` | 3 client formats × 3 channel formats × 2 stream modes × 5 cases | `90 passed, 0 failed` | command output |
| `make lab-agents` | Claude Code, Codex, Gemini CLI and Pi against each of the three routes | `12/12 PASS` | `testenv/runtime/logs` |
| `make lab-policy` | request blocking, tool-call blocking and restore to the empty policy | `3/3 PASS` | command output |
| `make lab-analysis` | asynchronous diagnosis, finding codes, coverage and reversible redaction | `PASS` | `testenv/runtime/analysis-response.json` |
| `make lab-browser` | 16 console pages at desktop and mobile widths, plus primary interactions | exit 0, `report.json` has no failures | `testenv/runtime/screenshots` |

The protocol matrix covers plain text, one tool call, parallel tool calls, a tool-result continuation and reversible redaction. The agent suite requires each CLI to read `hello.txt`, use its file tools and write the expected `answer.txt`. The diagnosis suite sends an injection and an exfiltration link, then checks that the analysis stores both findings, reports complete coverage, returns `unsafe`, and records a redaction hit.

Functional acceptance is complete when every row passes on a fresh lab and the evidence files exist. A single failure blocks delivery; do not accept a suite because the failure looks unrelated without a diagnosis recorded in the report.

## Performance acceptance

Run `make lab-load` after the functional suites have finished and the host has been idle for at least a minute. It measures the gateway against a direct provider baseline, using Chat Completions in non-streaming and streaming modes with the same prompt, model settings and concurrency on both paths. The direct run removes the gateway from the path, so the comparison isolates gateway overhead from provider latency.

The default profile is 300 measured requests per run, concurrency 4, three warm-up requests and a 16-token completion cap. The suite makes four measured runs, about 1200 requests in total. Three hundred requests per run keep provider variance from deciding the gate. Override the profile with `LOAD_REQUESTS`, `LOAD_CONCURRENCY`, `LOAD_WARMUP` and `LOAD_MAX_TOKENS`.

The suite alternates which path runs first between the two scenarios, then compares each gateway report with its direct baseline:

| Check             | Pass criterion                                                           |
| ----------------- | ------------------------------------------------------------------------ |
| Baseline validity | direct run has at least 99% success, no HTTP 429 and no protocol failure |
| Gateway errors    | at least 99% success, no HTTP 5xx, no HTTP 429 and no protocol failure   |
| Error delta       | gateway error rate is at most 0.5 percentage points above the baseline   |
| P95 latency       | gateway P95 is at most `baseline P95 × 1.20 + 100 ms`                    |
| Throughput        | gateway throughput is at least 90% of the baseline                       |
| Scenario coverage | both non-streaming and streaming scenarios pass                          |

Reports are written to `testenv/runtime/load/<timestamp>`. Each report contains success counts, error classes, P50/P95/P99 latency, throughput, first-half and second-half P95, token usage and host information. A run is invalid if the direct baseline hits a rate limit or has protocol failures; lower the concurrency and run it again. Do not rerun only the gateway side to obtain a passing number.

The performance suite covers the chat text path and the gateway's online and audit path. It does not measure the other three protocols, tool-call holding, console traffic, retention jobs or multi-instance behavior. Those remain functional or design limits, and the guide states them explicitly.

## Rebuilding and stopping

```sh
make lab-restart              # rebuild the console and binary, then restart the gateway
SKIP_WEB=1 make lab-restart   # reuse web/dist when only Go code changed
make lab-down                 # stop the gateway, remove containers and volume, delete runtime
PURGE=1 make lab-down         # also remove the lab images
```

## Evidence

Keep the following files with the delivery record:

- `testenv/runtime/screenshots/report.json` and the screenshots;
- `testenv/runtime/logs/*.log` from the agent suite;
- `testenv/runtime/load/<timestamp>/*.json` from both scenarios;
- the terminal output of `make verify`, `make lab-integration`, `make lab-protocol`, `make lab-policy` and `make lab-analysis`.

These paths are ignored by version control and may contain generated keys or model output. Review them before sharing outside the lab.

## Security boundary

`testenv/.env` holds the provider key; `testenv/runtime` holds the administrator password and agent keys. The gateway listens on all interfaces so containers can reach it. Do not expose port 8080 beyond the development host. The fixed database password and the private-network exception are test-only and must not be copied into a deployment.

## Related

- [Acceptance lab guide](../docs/agent/testenv.md)
- [Development](../docs/agent/development.md)
- [Protocol](../docs/agent/protocol.md)
- [Agent guide](../docs/agent/README.md)
