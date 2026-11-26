# Acceptance lab

`testenv/` is a disposable environment for changes that need a real provider, real agent CLIs and a browser. It is not part of the product deployment. PostgreSQL, four agent CLIs and the headless browser run in Docker; the gateway runs on the host so a backend or console change can be rebuilt without an image build. A local fault upstream covers failures a real provider cannot produce on demand.

The functional track checks protocol conversion, agent compatibility, enforcement, diagnosis, security, the control plane and console behavior. The performance track is a short relative comparison, not a capacity measurement.

## Topology

| Component                          | Runs in                   | Address                     |
| ---------------------------------- | ------------------------- | --------------------------- |
| Gateway and console                | host                      | `0.0.0.0:8080`              |
| PostgreSQL 17                      | `database` service        | `127.0.0.1:55432`           |
| Claude Code, Codex, Gemini CLI, Pi | `agents` image            | `host.docker.internal:8080` |
| Playwright and Chromium            | `browser` image           | `host.docker.internal:8080` |
| Fault upstream                     | host, during `lab-faults` | `127.0.0.1:18080`           |

Containers join the `safeagent-testenv_lab` network; only the database port is published. Lab channels use `allow_private_network=true` because Docker's embedded DNS returns its fake-IP range, which the gateway otherwise rejects. That exception is lab-only.

## Layout

`testenv/lib` holds the shared layers: `testenv/lib/lab.sh` (paths, environment, upstream lookup, routes and the compose helper), `testenv/lib/console.sh` (console session, API calls, check counters and cleanup hooks), `testenv/lib/protocol.sh` (gateway payloads, responses and call polling) and `testenv/lib/client.mjs` (the client, console and counter layer for the Node suites). `testenv/scripts` holds the operator entry points: `testenv/scripts/setup.sh`, `testenv/scripts/teardown.sh`, `testenv/scripts/restart-gateway.sh`, `testenv/scripts/integration.sh`, `testenv/scripts/run-agents.sh`, `testenv/scripts/run-suite.sh` and `testenv/scripts/acceptance.sh`. The shell suites live in `testenv/suites` (`testenv/suites/control.sh`, `testenv/suites/policy.sh`, `testenv/suites/analysis.sh`, `testenv/suites/security.sh`, `testenv/suites/faults.sh`, `testenv/suites/load.sh`); the Node suites live in `testenv/protocol` (`testenv/protocol/matrix.mjs`, `testenv/protocol/auxiliary.mjs`, `testenv/protocol/trajectory.mjs`). A suite keeps only its cases and sources the layers it needs.

## Environment contract

Setup and suite scripts read `testenv/.env` when it exists; the same keys can come from the process environment. The file is ignored and must not be committed.

### Provider formats

Each configured format gets its own channel when its URL, key and model are present. Setup reuses the seeded built-in provider rows instead of creating a provider per format: it creates a channel with that `provider_id`, the supplied URL as its base URL, the optional headers and the optional output limit. The channel base URL replaces the provider endpoint host, so the provider rows stay untouched. Formats that resolve to the same provider and upstream name share one catalog model, and setup creates the provider's models with one batch request per provider.

| Key | Meaning |
| --- | --- |
| `SAFEAGENT_UPSTREAM_CHAT_URL` | Chat provider base URL; the catalog adds the path |
| `SAFEAGENT_UPSTREAM_CHAT_KEY` | Chat key; falls back to `SAFEAGENT_UPSTREAM_KEY` |
| `SAFEAGENT_UPSTREAM_CHAT_MODEL` | Chat model; falls back to `SAFEAGENT_UPSTREAM_MODEL` |
| `SAFEAGENT_UPSTREAM_CHAT_PROVIDER` | seeded provider id, default `openai` |
| `SAFEAGENT_UPSTREAM_RESPONSES_URL` / `_KEY` / `_MODEL` / `_PROVIDER` | Responses format; provider default `openai` |
| `SAFEAGENT_UPSTREAM_MESSAGES_URL` / `_KEY` / `_MODEL` / `_PROVIDER` | Messages format; provider default `anthropic` |
| `SAFEAGENT_UPSTREAM_GEMINI_URL` / `_KEY` / `_MODEL` / `_PROVIDER` | Gemini format; provider default `google` |

The shared values `SAFEAGENT_UPSTREAM_KEY`, `SAFEAGENT_UPSTREAM_MODEL` and `SAFEAGENT_UPSTREAM_PROVIDER` are optional fallbacks for those three fields only. A URL, extra headers and default limit are always per format. The provider must be one of the seeded rows the build ships; setup names the known providers when the configured id is unknown and refuses a format the provider does not expose.

A channel base URL replaces the provider endpoint host for that channel, and the catalog keeps its path: it appends the path and, for Gemini, the model and action. The provider rows are never rewritten by setup.

### Channel and performance settings

| Key | Meaning |
| --- | --- |
| `SAFEAGENT_UPSTREAM_<FORMAT>_HEADERS_JSON` | JSON object of static upstream headers |
| `SAFEAGENT_UPSTREAM_<FORMAT>_DEFAULT_MAX_OUTPUT_TOKENS` | channel default output limit |
| `SAFEAGENT_UPSTREAM_<FORMAT>_NO_THINKING` | `1` disables provider reasoning in load requests; `0` when the provider rejects it |
| `SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` | full direct URL; required for `make lab-load` and full acceptance |
| `SAFEAGENT_ADMIN_PASSWORD` | initial administrator password; generated when empty |

`SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` is the only value that expands `{model}`.

### Protocol case toggles

| Key                           | Meaning                                                       |
| ----------------------------- | ------------------------------------------------------------- |
| `SAFEAGENT_TEST_REASONING`    | include the reasoning case in the matrix; default `1`         |
| `SAFEAGENT_TEST_MEDIA`        | include the media case in the matrix; default `1`             |
| `SAFEAGENT_TEST_STRUCTURED`   | include the structured-output case in the matrix; default `1` |
| `SAFEAGENT_TEST_SERVER_TOOLS` | include the server-tool case in the matrix; default `1`       |

All four default to `1`; `0` removes the case from a matrix run.

### Console browser settings

| Key | Meaning |
| --- | --- |
| `SAFEAGENT_ADMIN_NEW_PASSWORD` | new administrator password when the forced change is pending; empty by default |
| `SAFEAGENT_SCREENSHOTS` | screenshot directory inside the browser image; default `/screenshots` |

`make lab-browser` forwards both values to the browser container; the suite also reads them when it runs outside the image.

### Available formats and routes

Setup creates the catalog models, one channel named `Lab <format>` per enabled format and one route named `lab-<format>` per format. Each route has one target pinned to the format's channel model and the alias `lab-<format>-v1`, and the project default route is the chat route when one is configured. Route names are stable and independent of the upstream model, so model names containing `/` or `:` do not break agent CLI arguments. The generated environment exposes `SAFEAGENT_AVAILABLE_FORMATS` and one `SAFEAGENT_<FORMAT>_ROUTE` per configured format.

The protocol, policy, analysis and agent suites size themselves from `SAFEAGENT_AVAILABLE_FORMATS`. An unconfigured format remains a client format and the matrix records the skipped combinations.

### Generated files

| Path | Contents |
| --- | --- |
| `runtime/env` | base URL, project, formats, public route names, agent keys, administrator password, lab uid/gid |
| `runtime/state.json` | project, default route, channels, routes and aliases, agent keys and administrator password |
| `runtime/config.toml` | generated gateway configuration, including the lab instance key |
| `runtime/instance.key` | generated 32-byte instance key |
| `runtime/pi-models.json` | Pi provider and available public routes |
| `runtime/gateway.log` | gateway stdout and stderr |
| `build/lab/acceptance.log` | one-command acceptance log, kept by `make lab-down` |
| `build/lab/matrix.json` | protocol matrix report, kept by `make lab-down` |
| `runtime/logs/` | agent logs, answers and resolved CLI versions |
| `runtime/screenshots/` | browser screenshots and `report.json` |
| `runtime/load/` | performance reports and the compiled load generator |
| `runtime/control/`, `runtime/policy/`, `runtime/analysis/`, `runtime/security/`, `runtime/faults/` | request and response bodies of the shell suites |

`testenv/.env` and `testenv/runtime` are ignored; `build/lab` is ignored with the rest of `build/`.

## Lifecycle

```sh
cp testenv/env.example testenv/.env   # fill the formats you can reach
make lab-setup
make lab-acceptance                   # or run individual suites
make lab-down
```

`make lab-setup` starts PostgreSQL, builds the console and embedded gateway, starts the gateway on the host, and builds the control plane through the console API: it creates one channel per format with its base URL, keys, headers and output limit, imports the catalog models, creates one route per format with an alias and sets the project default route, then creates the agent keys and workspaces. The project has no quota so the protocol and agent suites measure conversion; the security suite creates its own limited projects. `FRESH=1 make lab-setup` removes the database volume and recreates everything; use it after a schema change. The product has no migration path.

`make lab-audit` (or `build/bin/safeagent -config testenv/runtime/config.toml verify-audit`) checks every call chain; `-call`, `-project`, `-since`, `-limit` and `-json` narrow the report. `make lab-restart` rebuilds and restarts the gateway; `SKIP_WEB=1` reuses `web/dist`. `make lab-acceptance` runs every suite in order, stops at the first failure and leaves the lab running; on success it calls `make lab-down`, which removes `runtime` but keeps the log and report under `build/lab`, unless `KEEP_LAB=1`.

## Agent CLIs

The agents image installs the latest npm release of Claude Code, Codex, Gemini CLI and Pi at build time. Resolved versions are written to `runtime/logs/agent-versions.txt` and included in delivery evidence. Rebuild the image to pick up a newer release.

Claude Code speaks Anthropic Messages, Codex speaks Responses, Gemini CLI speaks Gemini and Pi speaks Chat Completions. Each CLI runs against every configured route, so each client protocol is exercised against every real channel protocol. Each run must read `hello.txt`, use its file tools and write `answer.txt` with the expected line; a native route also runs a two-file task. A CLI that exits successfully without writing the file fails. Gemini CLI is configured with `GEMINI_API_KEY`, `GOOGLE_GEMINI_BASE_URL`, `GOOGLE_GENAI_API_VERSION=v1beta`, `GEMINI_CLI_TRUST_WORKSPACE=true` and a gateway auth block; approval mode is `yolo` because the container is disposable.

## Functional suites

Run in order. The policy suite leaves blocked calls and the agent suite leaves multi-call conversations for the browser suite. The fault and control-plane suites remove the global providers, channels and models they create; the security suite removes the projects it creates.

| Step | Command | Coverage | Pass criterion |
| --- | --- | --- | --- |
| 1 | `make verify` | formatting, vet, typecheck, contract diff, Go race tests, frontend tests | exit 0 |
| 2 | `make lab-integration` | repository transactions, cascades, quotas, retention, idempotency, audit chain | exit 0 |
| 3 | `make lab-control-plane` | connect wizard, route aliases, project default route, impact acknowledgement, channel/model disable, soft delete and restore, degraded configuration health across a restart, built-in provider reset | every check passes |
| 4 | `make lab-protocol` | client formats × configured routes × stream/JSON × core cases | no failures; skips listed with a reason |
| 5 | `make lab-auxiliary` | model list/retrieve, native counting, Responses lifecycle, embeddings, Responses WebSocket, and their console accounting | no unexpected failure; unsupported capabilities skipped |
| 6 | `make lab-agents` | four CLIs × configured routes, plus one multi-step task per CLI | every run writes the expected answer |
| 7 | `make lab-policy` | request, tool-result and tool-call blocks through every client and route | pre-upstream block 403; tool-phase block 403 before commit or 200 with block reason after commit |
| 8 | `make lab-analysis` | injection, exfiltration, coverage, distinct redaction values, conversation redaction index and call classification through every client and route | findings, complete coverage, `unsafe`, matching redaction values, correct lanes |
| 9 | `make lab-security` | invalid keys, cross-project isolation, rate/token limits, member/admin permissions, raw-event access, stream review settings | every check passes |
| 10 | `make lab-faults` | failover, truncation, tool hold, stream review, header forwarding, output limit, multi-choice, background rejection, auxiliary mapping | every check passes |
| 11 | `make lab-trajectory` | a multi-turn tool-loop conversation, its trajectory nodes, edges, replay steps and stats, and the absence of auxiliary calls | every check passes |
| 12 | `make lab-browser` | every console page at desktop and mobile widths, plus desktop flows for provider connect, channel, model and route soft delete/restore, policy, redaction, keys, users and members | exit 0, `report.json` has no failures |
| 13 | `make lab-load` | native routes and cross-format sample | every native comparison and cross-format sample passes |
| 14 | `make lab-audit` | every stored call chain and its anchor | exit 0 |

Notes that the pass criteria depend on:

- The protocol matrix sends text, one tool call, parallel calls, a tool-result continuation and reversible redaction in streaming and JSON mode. Multiple candidates are accepted only through a same-format streaming pass-through; everything else is rejected with `unsupported_multi_choice`. Optional cases are enabled with `SAFEAGENT_TEST_REASONING`, `SAFEAGENT_TEST_MEDIA`, `SAFEAGENT_TEST_STRUCTURED` and `SAFEAGENT_TEST_SERVER_TOOLS` (all default `1`); a skipped case is not a pass. Text, reasoning, structured and server-tool cases get two attempts, tool, parallel and continuation cases three, and redaction, media and rejection cases one. At the end the matrix drives one main call and one declared subagent call and checks their lane, purpose, outcome, resolution and shared conversation.
- The control-plane suite connects a custom provider, channel, models and keys in one call, renames a route and checks the old name became an alias, and reads the impact before deleting a referenced channel or model. It disables and soft deletes a referenced channel and model without detaching the route target, restores both, deletes a custom provider that its channel still names, and restarts the gateway: startup logs the warnings and serves traffic instead of refusing to run. It restores the graph and resets one built-in provider to its seeded definition.
- The auxiliary suite checks the console side of the auxiliary lane: entries are hidden from the default call list, appear with `include_auxiliary=true` or `lane=auxiliary`, carry no session, never create a conversation, and never enter the stats or monitor failure counters. Expected refusals are completed calls: a missing response is `not_found`, an embeddings request against a generation route is `unsupported`. An auxiliary-only project has zero main-lane denominators while its call is still listed through the filter.
- The policy suite saves request, tool-result and tool-call rules and sends them through every client. It does not force `tool_choice` because some providers reject a forced tool while thinking is enabled, so the after-commit branch is verified by pipeline unit tests. It restores the empty policy at the end.
- The analysis suite sends an injection phrase, an e-mail address and an exfiltration URL, waits for asynchronous analysis, and checks both findings, complete coverage and `unsafe`. It also compares `redaction_values` with the call's distinct hits, asserts a later call in the same conversation introduces no ancestor hits, and checks that the conversation redaction index reports the same distinct value ids with matching occurrence counts. A main call must be analyzed; a subagent call must be a side call in the same conversation with analysis skipped, and an unrouted side call must resolve through the project default route.
- The fault suite starts a deterministic Chat and Messages upstream and additionally checks first-candidate failover, a partial stream with incomplete tool arguments, a blocked held tool call, header allowlists, route output limits, multi-choice pass-through and rejection, stream review blocking the first byte, and embeddings reaching the auxiliary endpoint with the route's upstream model name. Each fault path is a custom provider cloned from the seeded endpoint definition; the fault, security and control-plane suites remove the providers they create.
- The trajectory suite drives three consecutive calls in one session, the middle one carrying a tool result back to the model. It checks stable node ids for the session, calls, turns, items, tool calls and tool results, the consumes edge that pairs a tool result with its call, ordered replay steps that reference existing nodes, stats that agree with the node list, and that no auxiliary call is projected into the conversation.
- The browser suite checks page errors, console errors, failed API responses, headings and empty bodies for every console page at desktop and mobile widths, and runs desktop create/edit/delete flows for projects, providers, channels, models, routes, keys, policy, redaction rules, users and members. It connects a custom provider through the wizard, imports a model, creates a route with a target and a manual alias, and soft-deletes and restores the channel, model and route with the impact acknowledgement. Traffic-dependent checks (call classification filters, call detail attempts and redactions, transcript raw/shown, trajectory Flow/Tree/Flat, inspector, replay and the redaction panel, risk and overview rows) assert the empty states and log the skipped checks when the lab has no calls, so the suite runs with and without upstream credentials.

## Performance smoke

The suite compares a direct provider call with the same call through the gateway per native route, in streaming and JSON modes: 50 requests, 2 workers, 1 warm-up, 256-token cap by default (raise `LOAD_MAX_TOKENS` for a reasoning upstream that spends the cap before it emits text) and 10 cross-format requests per pair by default (`LOAD_REQUESTS`, `LOAD_CONCURRENCY`, `LOAD_WARMUP`, `LOAD_MAX_TOKENS`, `LOAD_CROSS_REQUESTS`). Pass gates are scenario agreement, baseline validity, gateway error classes and the error-rate delta; latency and throughput are informational at this sample size. A well-formed completion without visible text is reported as `empty_text` instead of a protocol error, because a reasoning upstream can spend its whole budget before writing an answer. Use `loadgen compare -hard-performance` for a longer run; do not present quick numbers as capacity. The native comparison covers decode, redaction, observation, online policy, audit persistence and streaming relay, not tool holding, console traffic, worker throughput, retention or multi-instance.

## Failure triage

| Symptom | Check |
| --- | --- |
| Database already has an administrator | named volume survived; use `FRESH=1 make lab-setup` |
| `verify-audit` reports a broken chain after removing runtime | `runtime/instance.key` must match the database; recreate with `FRESH=1` |
| No channel or route was created | the format has no URL, so setup skipped it |
| Setup stops on a format with a URL | its key or model is missing |
| Setup stops on a provider id | the id is not one of the seeded providers, or the provider does not expose that format |
| Gateway rejects the upstream with a private-address error | Docker fake-IP; confirm lab channels still allow private networks |
| The gateway traffic reaches the real provider instead of the configured URL | the channel base URL or its key is wrong; check the channel row and rerun setup |
| An agent cannot reach the gateway | gateway listens on `0.0.0.0:8080`, port free, compose extra host present |
| An agent CLI reports a permission or config error | check `runtime/logs/agent-versions.txt` and the per-run log; rebuild the image |
| Gemini CLI returns 401/403 | check `SAFEAGENT_GEMINI_KEY`, `SAFEAGENT_UPSTREAM_GEMINI_KEY` and `GOOGLE_GEMINI_BASE_URL` |
| A protocol case fails intermittently | tool cases are model-dependent; check `attempts` in `matrix.json` |
| The matrix has no Gemini route | no Gemini upstream configured; check the route list before treating coverage as complete |
| A console page shows configuration warnings | a channel or model is disabled or a provider was removed; the control-plane suite proves the gateway still runs |
| The auxiliary suite cannot find an auxiliary call | the spec passed `include_auxiliary=true` or `lane=auxiliary`; the default list hides auxiliary traffic |
| The trajectory suite finds no tool call | the upstream did not emit a tool call; rerun or pick a tool-capable model |
| Browser suite reports no blocked call / no multi-call conversation | run `make lab-policy` / `make lab-agents` first |
| Performance baseline hits 429 | lower `LOAD_CONCURRENCY`, rerun both sides, keep the report as invalid evidence |

## Security boundary

`testenv/.env` holds provider keys and `testenv/runtime` holds the administrator password, agent keys and instance key; both are ignored. The gateway listens on all interfaces so containers can reach it; do not expose port 8080 beyond the development host. The lab's private-network exception and fixed database password are test-only. Setup sends each configured format's traffic through a channel whose base URL is the supplied upstream, so never point a lab instance at a production database. Individual fault and control-plane runs create uniquely named global providers, channels and models and delete them afterwards. Reports contain endpoint URLs, model names, error samples and token counts, never provider or agent keys; review error samples before sharing.

## Related

- [Development](development.md)
- [Protocol](protocol.md)
- [Agent guide](README.md)
- [Acceptance lab quick start](../../testenv/README.md)
