# Acceptance lab

`testenv/` is a disposable environment for changes that need a real provider, real agent CLIs and a browser. It is not part of the product deployment. PostgreSQL, four agent CLIs and the headless browser run in Docker; the gateway runs on the host so a backend or console change can be rebuilt without an image build.

The lab has two acceptance tracks. The functional track checks protocol conversion, agent compatibility, enforcement, diagnosis and console behavior. The performance track compares the gateway with a direct provider baseline under the same bounded workload. Both tracks use a real provider key supplied by the operator.

## Topology

| Component                          | Runs in            | Address                     |
| ---------------------------------- | ------------------ | --------------------------- |
| Gateway and console                | host               | `0.0.0.0:8080`              |
| PostgreSQL 17                      | `database` service | `127.0.0.1:55432`           |
| Claude Code, Codex, Gemini CLI, Pi | `agents` image     | `host.docker.internal:8080` |
| Playwright and Chromium            | `browser` image    | `host.docker.internal:8080` |

The containers join the `safeagent-testenv_lab` network. Only the database port is published; the agent and browser containers reach the gateway through the Docker host gateway. The lab creates its channels with `allow_private_network=true` because Docker's embedded DNS resolves external names to its fake-IP range, which the gateway otherwise rejects. That exception belongs to the lab only.

## Environment contract

The setup and load scripts read `testenv/.env` when it exists; the same keys can come from the process environment. The file is ignored and must not be committed.

| Key | Required | Meaning |
| --- | --- | --- |
| `SAFEAGENT_UPSTREAM_KEY` | yes | provider key used by the three lab channels and the direct performance baseline |
| `SAFEAGENT_UPSTREAM_MODEL` | yes | upstream model name; also prefixes the three lab route names |
| `SAFEAGENT_UPSTREAM_CHAT_URL` | yes | Chat Completions endpoint |
| `SAFEAGENT_UPSTREAM_RESPONSES_URL` | yes | Responses endpoint |
| `SAFEAGENT_UPSTREAM_MESSAGES_URL` | yes | Anthropic Messages endpoint |
| `SAFEAGENT_ADMIN_PASSWORD` | no | initial administrator password; generated when empty |

The provider must expose all three wire formats under the same account and model. The lab does not assume a vendor or fill in endpoints. The setup script reads the three URLs once and stores them in the lab channels; changing an endpoint later requires recreating the lab with `FRESH=1 make lab-setup`. It also writes `runtime/pi-models.json` from the upstream model, so a different model does not require an image rebuild.

`make lab-setup` writes generated state to `testenv/runtime`:

| Path                     | Contents                                                         |
| ------------------------ | ---------------------------------------------------------------- |
| `runtime/env`            | base URL, public model names, agent keys, administrator password |
| `runtime/state.json`     | the same values plus project and channel ids                     |
| `runtime/config.toml`    | generated gateway configuration                                  |
| `runtime/pi-models.json` | Pi provider and model list generated from the upstream model     |
| `runtime/gateway.log`    | gateway stdout and stderr                                        |
| `runtime/cookies.txt`    | administrator session used by setup                              |
| `runtime/workspace/`     | agent workspaces and their `answer.txt` files                    |
| `runtime/logs/`          | one log per agent and route                                      |
| `runtime/screenshots/`   | browser screenshots and `report.json`                            |
| `runtime/load/`          | performance reports and the compiled load generator              |

The script prints only masked keys. `testenv/.env` and `testenv/runtime` are ignored.

## Lifecycle

```sh
cp testenv/env.example testenv/.env   # fill the key, model and three endpoints
make lab-setup
# functional suites
make lab-down                         # or PURGE=1 make lab-down
```

`make lab-setup` starts PostgreSQL, builds the console and the embedded gateway, starts the gateway on the host, and creates the `Protocol Lab` project, three channels, three model routes, five agent keys and the agent workspaces. The project has no request, concurrency or token limit, so functional and performance runs measure the gateway rather than quota enforcement; the repository integration tests cover quotas. The initial administrator password is changed at the end of setup.

`FRESH=1 make lab-setup` first removes the container and its volume, then recreates the schema and all lab accounts. Use it after a schema change or when the setup script reports that an administrator already exists. A schema change requires a fresh lab because the product has no migration path. Changing the agent image requires a rebuild: run `PURGE=1 make lab-down` before the next setup, or build the agent services with Compose.

`make lab-restart` is the normal loop while working on the gateway or the console. The lab gateway uses its generated configuration, not `config.toml`.

## Functional acceptance

### Preconditions

- The lab was created from the current build with `FRESH=1 make lab-setup`.
- No other suite is running; the suites mutate policy and create calls that later suites inspect.
- The provider key is valid for all three endpoints and the provider is not rate limiting the account.
- The host is idle enough that the browser suite does not time out. Performance numbers are taken separately.

### Order and criteria

Run the suites in the order below. The policy suite leaves blocked calls that the browser suite checks, and the agent suite leaves multi-call conversations that the browser suite walks.

| Step | Command | Coverage | Pass criterion | Evidence |
| --- | --- | --- | --- | --- |
| 1 | `make verify` | formatting, vet, typecheck, generated contract, Go race tests, frontend tests | exit 0 | terminal output |
| 2 | `make lab-integration` | repository transactions, cascades, quotas, retention, idempotency | exit 0 | `go test` output |
| 3 | `make lab-protocol` | 3 client formats × 3 channel formats × 2 stream modes × 5 cases | `90 passed, 0 failed` | terminal output; `runtime/logs` |
| 4 | `make lab-agents` | Claude Code, Codex, Gemini CLI and Pi against each of the three routes | `12/12 PASS` | `runtime/logs/*.log`, `runtime/workspace/*/answer.txt` |
| 5 | `make lab-policy` | request blocking, tool-call blocking and rollback to the empty policy | `3/3 PASS` | terminal output |
| 6 | `make lab-analysis` | asynchronous diagnosis, finding codes, coverage, verdict and redaction | `PASS` | `runtime/analysis-response.json` |
| 7 | `make lab-browser` | 16 console pages at desktop and mobile widths, plus primary interactions | exit 0, `report.json` has no failures | `runtime/screenshots` |

`make verify` is the contract and unit gate. It does not start the lab and does not require a provider. `make lab-integration` uses a second database, `safeagent_integration`, on the lab PostgreSQL instance; each integration test drops and recreates its public schema, so it must not point at the gateway database.

### Suite mechanics

`make lab-protocol` sends each case through every client protocol to every channel protocol. The cases are plain text, one tool call, parallel tool calls, a tool-result continuation that replays reasoning, and reversible redaction. A case passes only when the response is protocol-valid, the expected content or tool call survives conversion, and a stream ends with its terminal event. The matrix makes up to three attempts per case because the upstream model chooses tools nondeterministically; a case that fails all attempts is a defect.

`make lab-agents` runs each CLI through every route. Claude Code speaks Anthropic Messages, Codex speaks Responses, Pi speaks Chat Completions, and Gemini CLI speaks the Gemini protocol. Each CLI uses its own provider configuration and receives the route name as its model, so the same three public routes exercise every client protocol against every channel protocol. Each run must read `hello.txt`, use its file tools and write `answer.txt` with the expected line. A CLI that exits successfully without writing the file fails.

Gemini CLI is pinned to `@google/gemini-cli@0.70.0`. The lab sets `GEMINI_API_KEY` to the Gemini agent key, points `GOOGLE_GEMINI_BASE_URL` at `host.docker.internal:8080`, and sets `GOOGLE_GENAI_API_VERSION=v1beta`. The CLI identifies itself as the gateway auth type when a base URL is present, so the key arrives at the gateway through the Gemini API-key path. `GEMINI_CLI_TRUST_WORKSPACE=true` avoids the folder-trust prompt, and `config/gemini-settings.json` selects the `gateway` auth type with external auth, disables auto-update and disables telemetry. External auth is required because this release validates only the built-in Google methods; the gateway method skips that validation and uses the API key against the configured base URL. The approval mode is `yolo` because the container is disposable and only the workspace volume is mounted.

`make lab-policy` saves a request-phase blocking rule and a tool-call-phase blocking rule through the console API. It expects HTTP 403 with the matching error code for each, then restores the empty policy and verifies that no rules remain. The browser suite depends on the blocked calls this step leaves behind.

`make lab-analysis` first verifies that the active policy is empty, then sends a prompt that contains an injection phrase, an e-mail address and an exfiltration URL. It polls the console until the call has a completed analysis, then checks four things: the call records at least one redaction hit, the analysis covers the whole trajectory, the findings include `prompt_injection` and `exfiltration_link`, and the verdict is `unsafe`. This is the functional check that diagnosis is reading stored events, running detectors and producing evidence rather than only recording online decisions.

`make lab-browser` starts the Chromium image, signs in, visits every page, opens the call tabs, the conversation tree and a blocked call, switches language and theme, and checks the mobile menu. It fails on a page error, a failed API request, a horizontal overflow greater than 2 pixels, a missing heading or an empty page. Screenshots and `report.json` are written to `runtime/screenshots`.

### Acceptance rule

Functional acceptance passes only when every row succeeds on the same fresh lab. A failure is not waived because the failing assertion looks unrelated to the change. Record the failing command, the report path and the first relevant error in the delivery evidence; fix the cause or, when the environment is at fault, rebuild the lab and run the full sequence again.

## Performance acceptance

### Model

The performance suite compares the complete gateway path with a direct provider request. The direct baseline is the same model, prompt, temperature, token cap and concurrency sent straight to the provider. The gateway run uses the same workload through a model route. The difference is the gateway's decoding, redaction, observation, policy, audit write and streaming overhead, plus any queueing it causes.

The provider is real, so absolute throughput is provider-bound and varies with region, time of day and account limits. The gate is therefore relative: it asks whether the gateway adds material latency or loses a material share of throughput. Absolute numbers are recorded for trend analysis but are not a capacity claim.

Run `make lab-load` after the functional suites have finished and the host has been idle for at least a minute, with no other traffic to the provider. The script waits up to two minutes for `analysis_jobs` to empty before it measures, so backlog from the browser suite does not overlap the run. The gateway must be healthy at `/health`. The suite uses `SAFEAGENT_UPSTREAM_KEY` from `testenv/.env` or the environment for the direct path and the lab agent key for the gateway path; neither key is written to a report.

### Workload

The suite covers Chat Completions in non-streaming and streaming modes. The prompt is short and the completion cap is 16 tokens, so the run measures request handling rather than generation length. Each run is closed-loop: a fixed number of workers send the next request as soon as the previous one finishes.

| Parameter                 | Default | Environment override |
| ------------------------- | ------- | -------------------- |
| measured requests per run | 300     | `LOAD_REQUESTS`      |
| concurrent workers        | 4       | `LOAD_CONCURRENCY`   |
| warm-up requests per run  | 3       | `LOAD_WARMUP`        |
| completion token cap      | 16      | `LOAD_MAX_TOKENS`    |

The default suite makes four measured runs: baseline and gateway for non-streaming, then gateway and baseline for streaming. The order alternates so that neither path always runs first. About 1200 measured requests are made in total, plus warm-up. Three hundred requests per run keep provider variance from deciding the gate; a smaller run can swing more than ten percentage points in throughput. Lower the defaults when the account has a low quota; keep both sides of a scenario identical.

Warm-up requests are excluded from the metrics and are expected to succeed. A failed warm-up stops the run with the HTTP status or transport error; it is not silently treated as an empty measurement.

### Metrics

The load generator records, per run:

- succeeded and failed requests, success rate and error classes (`http_4xx`, `http_429`, `http_5xx`, `timeout`, `network`, `protocol`);
- status counts and up to three sanitized samples for each error class;
- mean, P50, P95, P99 and maximum latency of successful requests;
- throughput as all completed requests, including failures, divided by wall-clock duration;
- first-half and second-half P95, for detecting a sustained slowdown;
- prompt and completion token totals when the provider returns usage;
- host OS, architecture, CPU count and Go version.

The report is JSON and is written to `testenv/runtime/load/<timestamp>`. A protocol failure means the response was not valid Chat Completions: non-JSON, no choices, no text, malformed SSE, no content delta or no terminal event. These count as failures, not as successful requests with high latency.

### Comparison

The `compare` step reads the baseline and gateway reports and applies these checks:

| Check             | Rule                                                                        |
| ----------------- | --------------------------------------------------------------------------- |
| Same scenario     | stream mode, request count and concurrency match                            |
| Baseline validity | baseline success at least 99%, no HTTP 429, no protocol failure             |
| Gateway errors    | gateway success at least 99%, no HTTP 5xx, no HTTP 429, no protocol failure |
| Error delta       | gateway error rate at most 0.5 percentage points above baseline             |
| P95 latency       | `gateway P95 ≤ baseline P95 × 1.20 + 100 ms`                                |
| Throughput        | `gateway throughput ≥ baseline throughput × 0.90`                           |

The thresholds can be overridden on the `load compare` command, but the delivery gate uses the defaults above for both scenarios. A missing baseline, a baseline with rate limiting, or a scenario mismatch fails the comparison rather than producing a pass.

### Validity and retries

A performance result is invalid when the direct baseline has more than 1% failures, any HTTP 429, or any protocol failure. Stop, lower `LOAD_CONCURRENCY` or `LOAD_REQUESTS`, and rerun the whole scenario on both paths. Do not rerun only the gateway side to obtain a passing number, and do not compare a report taken on a different host, network path or provider account.

A single repeat is allowed when the host was demonstrably busy or the provider had a transient incident. One request that stalls for tens of seconds lowers wall-clock throughput even when P95 stays healthy; treat that run as a provider transient, keep the report and rerun both sides. Keep both reports and state why the first run was discarded. A gateway failure under a valid baseline is a finding, not a reason to retry.

### Scope

The performance suite covers the chat text path, including request decoding, redaction, observation, online policy, audit persistence and streaming relay. It does not cover:

- Responses, Anthropic Messages or Gemini conversion;
- tool-call holding and the tool checkpoint;
- console API latency or browser rendering;
- analysis worker throughput under a large backlog;
- retention pruning, quota settlement or multi-instance behavior.

Those paths remain covered functionally or are outside the current design. A capacity test that fixes the provider as a constant requires a local fake upstream and is not part of this suite.

## Failure triage

| Symptom | Check |
| --- | --- |
| The database already has an administrator | The named volume survived a previous run. Use `FRESH=1 make lab-setup`; it destroys the lab database. |
| The gateway rejects the upstream with a private-address error | Docker returned a fake-IP address for the provider host. Confirm that the lab channels still have `allow_private_network=true`. |
| An agent cannot reach the gateway | The compose file maps `host.docker.internal` to the Docker host gateway. Confirm that the gateway listens on `0.0.0.0:8080` and port 8080 is free. |
| Claude Code exits with a permission error | The agent image runs as uid 1000 and `run-agents.sh` passes an explicit tool allowlist. Do not run the container as root and do not use permission-skipping flags. |
| Pi returns 401 | The run script passes the key with `--api-key`; confirm that `runtime/env` exists and setup completed. |
| Gemini CLI returns 401 or 403 | Confirm that `SAFEAGENT_GEMINI_KEY` is present in `runtime/env` and that `GOOGLE_GEMINI_BASE_URL` still points at the lab gateway. The CLI uses `GEMINI_API_KEY` from the compose service, which the run script sets from that agent key. |
| Gemini CLI cannot read or write the workspace | Check that `GEMINI_CLI_TRUST_WORKSPACE=true` is set and that the run uses `--approval-mode=yolo`. The workspace is the only mounted directory. |
| The browser suite reports no blocked call | Run `make lab-policy` before `make lab-browser`. |
| The browser suite reports no multi-call conversation | Run `make lab-agents` before `make lab-browser`. |
| The diagnosis suite reports a non-empty policy | Run `make lab-policy`; it restores the empty baseline at the end. |
| The diagnosis suite times out | Check `runtime/gateway.log` and the call detail. A missing worker, a schema mismatch or a detector failure leaves the analysis running or failed. |
| A protocol case fails intermittently | The matrix makes up to three attempts per case and the upstream model chooses tools nondeterministically. Re-run and inspect `runtime/logs` and the failing response body before treating it as a conversion defect. |
| The load baseline hits 429 | Lower `LOAD_CONCURRENCY`, rerun both sides and keep the report that records the rate limit as invalid evidence. |
| The gateway P95 or throughput fails | Check `runtime/gateway.log`, database latency and analysis worker backlog. The measured path includes audit writes and job creation, so a saturated database is a real gateway result. |

## Security boundary

`testenv/.env` holds the provider key and `testenv/runtime` holds the administrator password and agent keys. Both paths are ignored. The gateway listens on all interfaces so containers can reach it; do not expose port 8080 beyond the development host. The lab's private-network exception and its fixed database password are test-only and must not be copied into a deployment.

Reports contain endpoint URLs, model names, error samples and token counts. They do not contain the provider key or the agent keys. Review error samples before sharing a report outside the lab.

## Related

- [Development](development.md)
- [Protocol](protocol.md)
- [Agent guide](README.md)
- [Acceptance lab quick start](../../testenv/README.md)
