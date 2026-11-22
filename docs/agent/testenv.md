# Acceptance lab

`testenv/` is a disposable environment for changes that need a real provider, real agent CLIs and a browser. It is not part of the product deployment. PostgreSQL, four agent CLIs and the headless browser run in Docker; the gateway runs on the host so a backend or console change can be rebuilt without an image build. A local fault upstream covers failures that a real provider cannot produce on demand.

The lab has two tracks. The functional track checks protocol conversion, agent compatibility, enforcement, diagnosis, security and console behavior. The performance track is a short relative comparison, not a capacity measurement. Protocol, agent, policy and analysis runs use the configured providers; the fault suite uses a local deterministic upstream, and the browser suite exercises the console without calling a model.

## Topology

| Component                          | Runs in                   | Address                     |
| ---------------------------------- | ------------------------- | --------------------------- |
| Gateway and console                | host                      | `0.0.0.0:8080`              |
| PostgreSQL 17                      | `database` service        | `127.0.0.1:55432`           |
| Claude Code, Codex, Gemini CLI, Pi | `agents` image            | `host.docker.internal:8080` |
| Playwright and Chromium            | `browser` image           | `host.docker.internal:8080` |
| Fault upstream                     | host, during `lab-faults` | `127.0.0.1:18080`           |

The containers join the `safeagent-testenv_lab` network. Only the database port is published; the agent and browser containers reach the gateway through the Docker host gateway. Lab channels use `allow_private_network=true` because Docker's embedded DNS resolves external names to its fake-IP range, which the gateway otherwise rejects. That exception belongs to the lab only.

## Environment contract

The setup and suite scripts read `testenv/.env` when it exists; the same keys can come from the process environment. The file is ignored and must not be committed.

### Provider formats

Each configured format gets its own channel when its URL, key and model are present. Formats that resolve to the same provider and upstream name share one catalog model; setup adds each format to that model. The channel enables the model and its format.

| Key                                     | Meaning                                              |
| --------------------------------------- | ---------------------------------------------------- |
| `SAFEAGENT_UPSTREAM_CHAT_URL`           | Chat provider base URL; the catalog adds the path    |
| `SAFEAGENT_UPSTREAM_CHAT_KEY`           | Chat key; falls back to `SAFEAGENT_UPSTREAM_KEY`     |
| `SAFEAGENT_UPSTREAM_CHAT_MODEL`         | Chat model; falls back to `SAFEAGENT_UPSTREAM_MODEL` |
| `SAFEAGENT_UPSTREAM_CHAT_PROVIDER`      | provider, default `openai`                           |
| `SAFEAGENT_UPSTREAM_RESPONSES_URL`      | Responses provider base URL                          |
| `SAFEAGENT_UPSTREAM_RESPONSES_KEY`      | Responses key; falls back to the shared key          |
| `SAFEAGENT_UPSTREAM_RESPONSES_MODEL`    | Responses model; falls back to the shared model      |
| `SAFEAGENT_UPSTREAM_RESPONSES_PROVIDER` | provider, default `openai`                           |
| `SAFEAGENT_UPSTREAM_MESSAGES_URL`       | Anthropic Messages provider base URL                 |
| `SAFEAGENT_UPSTREAM_MESSAGES_KEY`       | Messages key; falls back to the shared key           |
| `SAFEAGENT_UPSTREAM_MESSAGES_MODEL`     | Messages model; falls back to the shared model       |
| `SAFEAGENT_UPSTREAM_MESSAGES_PROVIDER`  | provider, default `anthropic`                        |
| `SAFEAGENT_UPSTREAM_GEMINI_URL`         | Gemini provider base URL                             |
| `SAFEAGENT_UPSTREAM_GEMINI_KEY`         | Gemini key; falls back to the shared key             |
| `SAFEAGENT_UPSTREAM_GEMINI_MODEL`       | Gemini model; falls back to the shared model         |
| `SAFEAGENT_UPSTREAM_GEMINI_PROVIDER`    | provider, default `google`                           |

The shared values `SAFEAGENT_UPSTREAM_KEY`, `SAFEAGENT_UPSTREAM_MODEL` and `SAFEAGENT_UPSTREAM_PROVIDER` are optional fallbacks for those three fields only. A URL, extra headers and default limit are always per format. The URL is the provider base URL; the gateway appends the endpoint path from its catalog. Different providers per format are supported.

### Channel and performance settings

| Key | Meaning |
| --- | --- |
| `SAFEAGENT_UPSTREAM_<FORMAT>_HEADERS_JSON` | JSON object of static upstream headers |
| `SAFEAGENT_UPSTREAM_<FORMAT>_DEFAULT_MAX_OUTPUT_TOKENS` | channel default output limit |
| `SAFEAGENT_UPSTREAM_<FORMAT>_NO_THINKING` | `1` disables provider reasoning in load requests; set `0` when the provider rejects the disable field |
| `SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` | full direct URL; required to run `make lab-load` or `make lab-acceptance` |
| `SAFEAGENT_ADMIN_PASSWORD` | initial administrator password; generated when empty |

The channel settings are optional. `SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` is the exception: each configured format needs it for the direct performance baseline, so `make lab-load` and a full `make lab-acceptance` stop without it. It is the only value that expands `{model}`. A channel base URL does not substitute `{model}`: the provider catalog appends the path, and for Gemini the model and the streaming or plain action.

### Available formats and routes

Setup creates the needed catalog models, one channel named `Lab <format>` per format that enables the shared model, and one route named `lab-<format>`. It also creates five agent keys and the agent workspaces. The route names are stable and independent of the upstream model, so a model name containing `/` or `:` does not break agent CLI arguments.

The protocol matrix, policy, analysis and agent suites size themselves from `SAFEAGENT_AVAILABLE_FORMATS`. If you do not configure a Gemini upstream, Gemini remains a client format; the Gemini channel path is covered by unit tests but not by a real provider. The matrix report records skipped combinations explicitly.

### Generated files

| Path | Contents |
| --- | --- |
| `runtime/env` | base URL, project, available formats, public route names (`SAFEAGENT_<FORMAT>_ROUTE`), agent keys, administrator password, lab uid/gid |
| `runtime/state.json` | base URL, project, channels (id, model name and model id), routes, agent keys and the administrator password |
| `runtime/config.toml` | generated gateway configuration |
| `runtime/pi-models.json` | Pi provider and the available public routes |
| `runtime/gateway.log` | gateway stdout and stderr |
| `runtime/acceptance/` | one-command log and the protocol report |
| `runtime/logs/` | agent logs, answer files and the resolved CLI versions |
| `runtime/screenshots/` | browser screenshots and `report.json` |
| `runtime/load/` | performance reports and the compiled load generator |

`testenv/.env` and `testenv/runtime` are ignored.

## Lifecycle

```sh
cp testenv/env.example testenv/.env   # fill the formats you can reach
make lab-setup
make lab-acceptance                   # or run individual suites
make lab-down
```

`make lab-setup` starts PostgreSQL, builds the console and the embedded gateway, starts the gateway on the host, and creates the project, catalog models, channels, routes, agent keys and workspaces. The project has no request or token quota, so the protocol and agent suites measure conversion; the security suite creates separate limited projects for quota and rate checks.

`FRESH=1 make lab-setup` removes the database volume before recreating the schema and all lab accounts. Use it after a schema change or when setup reports that an administrator already exists. The product has no migration path.

`make lab-audit` (or `build/bin/safeagent -config testenv/runtime/config.toml verify-audit`) checks every call chain; add `-call`, `-project`, `-since`, `-limit` or `-json` to narrow the report. `make lab-restart` rebuilds the console and binary and restarts the gateway. `SKIP_WEB=1 make lab-restart` reuses `web/dist` when only Go code changed.

`make lab-acceptance` runs every suite in order. It stops at the first failure and leaves the lab running; on success it calls `make lab-down`, which removes `runtime` and the evidence. `KEEP_LAB=1 make lab-acceptance` keeps the lab after a successful run.

## Agent CLIs

The agents image installs the latest npm release of Claude Code, Codex, Gemini CLI and Pi at build time. The exact resolved versions are written to `runtime/logs/agent-versions.txt` and are included in the delivery evidence. Rebuild the image to pick up a newer release; the next agent run builds it on demand:

```sh
PURGE=1 make lab-down
make lab-setup
```

Claude Code speaks Anthropic Messages, Codex speaks Responses, Gemini CLI speaks Gemini and Pi speaks Chat Completions. Each CLI runs against every configured route, so each client protocol is exercised against every real channel protocol. Each run must read `hello.txt`, use its file tools and write `answer.txt` with the expected line. A CLI that exits successfully without writing the file fails. The native route for each CLI also runs a two-file task that requires more than one tool step; a native route that is not configured is reported as skipped.

Gemini CLI is configured with `GEMINI_API_KEY`, `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_GENAI_API_VERSION=v1beta`, `GEMINI_CLI_TRUST_WORKSPACE=true` and a gateway auth block in `config/gemini-settings.json`. The gateway receives the key through the Gemini API-key path. The approval mode is `yolo` because the container is disposable and only the workspace is mounted.

## Functional suites

Run the suites in order. The policy suite leaves blocked calls that the browser suite checks, and the agent suite leaves multi-call conversations that the browser suite walks. The fault and security suites remove the temporary projects they create; the security suite removes its temporary member and key, and leaves its test user disabled.

| Step | Command | Coverage | Pass criterion | Evidence |
| --- | --- | --- | --- | --- |
| 1 | `make verify` | formatting, vet, typecheck, generated contract, Go race tests, frontend tests | exit 0 | terminal output |
| 2 | `make lab-integration` | repository transactions, cascades, quotas, retention, idempotency, audit chain verification and tamper detection | exit 0 | `go test` output |
| 3 | `make lab-protocol` | four client formats × configured channel formats × stream/JSON × core cases, plus enabled extensions | no failures; skips are listed with a reason | `runtime/acceptance/matrix.json` |
| 4 | `make lab-auxiliary` | model list/retrieve, native counting, Responses lifecycle, embeddings passthrough and the Responses WebSocket | no unexpected failure; unsupported capabilities are skipped with a reason | standard output |
| 5 | `make lab-agents` | four CLIs × configured routes, plus one native multi-step task per CLI | every run writes the expected answer | `runtime/logs/*.log`, `runtime/logs/*.answer`, `agent-versions.txt` |
| 6 | `make lab-policy` | request, tool-result and tool-call blocks through every client and route | a pre-upstream block returns 403; a tool-phase block returns 403 before commit or 200 with the block reason after commit; policy restored to empty. The response phase and the exact after-commit error shape are covered by pipeline unit tests. | terminal output, `runtime/policy/*.json` |
| 7 | `make lab-analysis` | injection, exfiltration, coverage and redaction hits through every client and route | every call completes with both findings, complete coverage, `unsafe` and a redaction hit | `runtime/analysis-response-*.json` |
| 8 | `make lab-security` | invalid keys, cross-project isolation, rate and token limits, member/admin permissions, raw-event access | every check passes | terminal output |
| 9 | `make lab-faults` | failover before content, truncated stream, held tool call, header forwarding, route output limit, multi-choice pass-through and rejection, and background rejection | every check passes | terminal output |
| 10 | `make lab-browser` | all pages at desktop and mobile widths, plus desktop create, edit, delete, revoke or disable flows for projects, channels, routes, keys, policy, redaction rules, users and members | exit 0, `report.json` has no failures | `runtime/screenshots` |
| 11 | `make lab-load` | native routes and cross-format sample | every native comparison and cross-format sample passes | `runtime/load/<timestamp>` |
| 12 | `make lab-audit` | every stored call chain and its call anchor | exit 0 | terminal output |

### Protocol matrix

The matrix sends plain text, one tool call, parallel tool calls, a tool-result continuation and reversible redaction through every client and configured route, in streaming and JSON mode. Multiple candidates are accepted only through a same-format streaming pass-through; every other combination is rejected with `unsupported_multi_choice`. The Responses rejection case is `background=true`, which the gateway never proxies. Messages has no defined rejection case and is recorded as skipped. With three configured routes the matrix has 144 core slots; four routes have 192. Every slot is reported even when it is skipped.

Optional cases are enabled by environment variables:

| Variable | Default | Case |
| --- | --- | --- |
| `SAFEAGENT_TEST_REASONING` | `1` | reasoning summary and visible result; skipped when the model returns no reasoning |
| `SAFEAGENT_TEST_MEDIA` | `1` | inline image input; skipped when the channel does not support it |
| `SAFEAGENT_TEST_STRUCTURED` | `1` | JSON output; the Messages client case is not scripted, and the channel must offer a schema or a schema-less JSON mode |
| `SAFEAGENT_TEST_SERVER_TOOLS` | `1` | provider-executed search; skipped when the client case is not scripted or the channel has no equivalent |

A skipped case is not a pass. It appears in `matrix.json` with the HTTP status or capability reason. The redaction case also queries the console and fails unless the call records a positive redaction count, so a client echo alone cannot prove the rule ran.

Text, redaction and rejection cases get one attempt; media gets one. Tool, parallel and continuation cases get up to three attempts because the model chooses tools nondeterministically. Reasoning, structured-output and server-tool cases get two. `matrix.json` records the attempts for every case and lists each skip with its reason.

### Enforcement and diagnosis

The policy suite saves a request-phase rule that matches injection findings, a tool-result-phase rule that matches injected text in a tool result, and a tool-call-phase rule that matches `get_weather`, then sends a request through every client format and configured route. The tool request asks the model to call `get_weather`; it does not force `tool_choice`, because some providers reject a forced tool while thinking is enabled. A request-phase or tool-result block returns HTTP 403 with a protocol-shaped error. A tool-phase block before the stream has committed also returns HTTP 403; if text has already been sent, the response is HTTP 200 and carries the block reason in a protocol-native error frame. The suite accepts either outcome because the model decides whether text precedes the tool call; pipeline unit tests cover each branch. A response-phase block applies to a non-streaming request and returns HTTP 403 without the upstream body; the live suite does not force a model to emit a blocked response, so that phase is covered by pipeline unit tests. The suite restores the empty policy at the end.

The analysis suite sends an injection phrase, an e-mail address and an exfiltration URL through every client format and route, waits for the asynchronous analysis, and checks that the call has a redaction hit, both findings, complete coverage and an `unsafe` verdict. This proves the stored events, observation adapter, detectors and redaction work together. The protocol matrix separately checks that the client receives the original value back, so the two suites together cover both directions of redaction.

### Security

The security suite uses separate projects so it does not disturb the main audit trail, then deletes those projects and removes its temporary member and rate-limited key. It checks invalid keys for every client protocol, a key cannot use another project's route, a one-request-per-minute key is limited on the second call, a one-token daily quota is rejected before the upstream, a non-member cannot read project calls, a member can read them, members cannot manage channels or the global log, administrators see raw values while members see the redacted view, and raw access is written to the operation log.

### Faults

The local fault upstream starts only for this suite and serves deterministic Chat and Messages streams. The suite checks:

- a 500 on the first candidate fails over to the second before any content;
- a partial stream with incomplete tool arguments is reported as failed, records one attempt and is not retried;
- a tool call is held and blocked before its arguments reach the client;
- allowlisted and channel-configured headers reach the upstream while a cookie does not;
- a route output limit reaches a Messages upstream when the client omits one;
- a streaming Chat `n>1` request on a same-format target passes through with both candidates and a terminal frame;
- non-streaming Chat `n>1` is rejected before the upstream, and Responses `background=true` is rejected before the upstream. The protocol matrix covers Gemini `candidateCount` when a Gemini channel is configured.

### Console

The browser suite signs in and visits every page at desktop and mobile widths. Desktop runs check page errors, failed API responses, headings and empty bodies; mobile runs check page errors, failed API responses and horizontal overflow. The desktop flow creates, edits and deletes a project, route and catalog model; creates, edits and revokes an agent key; saves and restores a policy; creates, edits and deletes a redaction rule; creates and disables a user; adds and removes a member; and creates a channel with two explicit formats, binds a catalog model, edits its default limit, forwarded headers and key, then disables it. It also opens the call transcript, events and attempts tabs. A missing multi-call conversation, blocked call or channel is a failure, not a silently reduced run.

## Performance smoke

The performance suite is deliberately short. It is a regression signal, not a capacity claim. The load requests disable provider reasoning, so the measured path is visible text.

| Parameter                      | Default | Environment           |
| ------------------------------ | ------- | --------------------- |
| native requests per run        | 50      | `LOAD_REQUESTS`       |
| concurrent workers             | 2       | `LOAD_CONCURRENCY`    |
| warm-up per run                | 1       | `LOAD_WARMUP`         |
| completion cap                 | 64      | `LOAD_MAX_TOKENS`     |
| cross-format requests per pair | 10      | `LOAD_CROSS_REQUESTS` |

For each configured native route, the suite compares a direct provider call with the same call through the gateway, in streaming and JSON modes. The direct and gateway reports must describe the same scenario: protocol, stream mode, request count, concurrency, prompt, token cap and reasoning mode. The pass gates are scenario agreement, baseline validity, gateway error classes and the error-rate delta; P95 latency and throughput are reported for comparison but are informational at this sample size, so the quick run does not fail on a provider-bound timing swing. Use `loadgen compare -hard-performance` with a longer run when a latency or throughput gate is required; do not present the quick numbers as a capacity claim.

The suite also sends a cross-format sample through every client and route. It does not compare cross-format latency with a native baseline because the formats are not directly comparable; it requires zero failures and a complete protocol response.

The native comparison covers request decoding, redaction, observation, online policy, audit persistence and streaming relay. It does not cover tool holding, console traffic, analysis worker throughput, retention or multi-instance behavior.

## Failure triage

| Symptom | Check |
| --- | --- |
| The database already has an administrator | The named volume survived a previous run. Use `FRESH=1 make lab-setup`; it destroys the lab database. |
| No channel or route was created | The format has no URL, so setup skipped it. The matrix and agent suite then omit that route. |
| Setup stops on a format with a URL | Its key or model is missing. Supply both, or remove the URL to skip that format. |
| The gateway rejects the upstream with a private-address error | Docker returned a fake-IP address for the provider host. Confirm that lab channels still have `allow_private_network=true`. |
| An agent cannot reach the gateway | Confirm the gateway listens on `0.0.0.0:8080`, port 8080 is free and the compose extra host is present. |
| An agent CLI exits with a permission or config error | The image installs the latest release at build time. Read `runtime/logs/agent-versions.txt` and the per-run log; rebuild the image after a CLI change. |
| Gemini CLI returns 401 or 403 | Confirm `SAFEAGENT_GEMINI_KEY` (the gateway agent key in `runtime/env`), `SAFEAGENT_UPSTREAM_GEMINI_KEY` (the provider key) and `GOOGLE_GEMINI_BASE_URL`; the gateway auth block may need an update when the CLI changes. |
| A protocol case fails intermittently | Tool cases are model-dependent. Check `attempts` and the failing response body in `matrix.json`; text and redaction cases must pass on the first attempt. |
| The matrix has no Gemini route | No Gemini upstream is configured. The route is absent and is not listed as a skip, so read the configured route list before treating the matrix as complete coverage. |
| The browser suite reports no blocked call | Run `make lab-policy` first. |
| The browser suite reports no multi-call conversation | Run `make lab-agents` first. |
| The performance baseline hits 429 | Lower `LOAD_CONCURRENCY`, rerun both sides and keep the report as invalid evidence. |
| `SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` is required | Set the full direct endpoint for every configured format, or do not run `make lab-load` or `make lab-acceptance`. |

## Security boundary

`testenv/.env` holds provider keys and `testenv/runtime` holds the administrator password and agent keys. Both are ignored. The gateway listens on all interfaces so containers can reach it; do not expose port 8080 beyond the development host. The lab's private-network exception and fixed database password are test-only. Individual fault runs create uniquely named global test channels and models and delete them afterwards; the lab database is disposable and `make lab-down` removes it anyway.

Reports contain endpoint URLs, model names, error samples and token counts. They do not contain provider keys or agent keys. Review error samples before sharing a report outside the lab.

## Related

- [Development](development.md)
- [Protocol](protocol.md)
- [Agent guide](README.md)
- [Acceptance lab quick start](../../testenv/README.md)
