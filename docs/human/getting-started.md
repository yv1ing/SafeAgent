# Getting started

[中文](getting-started_zh.md)

This guide starts from a fresh checkout, brings up the gateway, configures one provider and one route, and ends with an agent sending traffic that you can inspect in the console.

## Prerequisites

- Go 1.27.4
- Node.js 22 or newer
- GNU Make
- a PostgreSQL 17 database that already exists

SafeAgent creates its tables on the first start but never creates the database itself. Install the locked Go and frontend dependencies first:

```sh
make deps
```

## Configure and start

Copy the example configuration and fill in the two required values:

```sh
cp config.example.toml config.toml
```

```toml
listen = "127.0.0.1:8080"
database_url = "postgres://safeagent:change-me@localhost:5432/safeagent?sslmode=disable"
instance_key = "<output of: openssl rand -base64 32>"
secure_cookies = false
```

The instance key is required and must be 32 bytes encoded as standard base64. It authenticates the audit chain and seals stored credentials, and it is never written to the database. Keep it: without it, existing channel keys and audit chains cannot be read. Every other key has a default. Two settings depend on the deployment:

- `secure_cookies` is `true` by default. Set it to `false` only while the console is reached over plain HTTP on loopback; any other deployment should use HTTPS and keep it `true`, because a Secure cookie is dropped over plain HTTP.
- `sslmode=disable` is for a local database only. A remote database should use TLS.

An unrecognized configuration key stops startup, so a typo is not silently ignored.

Build and run:

```sh
make build
build/bin/safeagent -config config.toml serve
```

The build produces one binary with the console embedded. `make run` starts the same server from source but serves the console from `web/dist`, so run `make build` at least once before using it. `make dev` runs the backend and the console dev server together.

On the first start against an empty database, the process creates the schema and prints the administrator account and password once:

```text
Initial administrator: admin / <password>
```

Later starts do not print it again. If the password is lost, reset it from another administrator account or recreate the database. The same process serves the console at `/console/`, the gateway at `/v1/` and `/v1beta/`, and a health check at `/health`.

## Sign in and create a project

Open <http://127.0.0.1:8080/console/> and sign in. The first sign-in requires a new password of 14 to 72 bytes with lower case, upper case and a digit. The console opens on Monitor, which aggregates every project the account can see.

Create a project. A name is enough; the defaults can be changed later. A new project enables the common redaction detectors, starts with an empty policy document and keeps main calls for 30 days and auxiliary calls for 3 days.

Opening the project switches the sidebar to its pages. A project member can manage routes, non-blocking redaction rules and agent keys; an administrator additionally manages providers, channels, models, users, members, policy editing and restore, blocking redaction rules, the decision model, retention and limits. Configuration changes are recorded in the operation log: the global Log lists all of them, and a project's Activity page lists the changes made inside that project.

## Connect a provider

Providers, channels and models are global; routes belong to a project. A provider describes protocol endpoints and capabilities, a channel binds a provider to a base address, credentials and limits, and a model in the catalog names the upstream model, its kind and its price per million tokens.

Open Providers under Administration. Built-in providers are already there as editable rows. To connect a new provider, choose a built-in one or add a custom provider with its base address, enter the upstream API key, run discovery and select the models the endpoint reports. The wizard creates the provider, the channel, its credentials and the imported models in one transaction; the key is sealed with the instance key and only its last characters are shown afterwards.

If the endpoint is self-hosted on a private address, enable private-network access on that channel. Each channel has its own timeouts, output limit and forwarded headers; the same provider can be served by several channels with different keys.

Models can be added or corrected on the Models page. A model belongs to a provider and records an upstream name, a kind (generation, embedding or moderation), prices and an enabled flag. Its usable formats are derived from the provider's endpoints and the kind, so there is no separate format list to maintain.

## Create a route

A route maps a public model name to one or more targets in the project. Create it on the project's Model routes page:

- the name is what agents send in the `model` field; aliases let an old or alternative name keep working;
- each target is a channel plus a catalog model, with a priority, a weight and optionally a format and an output limit;
- targets of one route must use the same model kind;
- the lowest priority number is tried first; equal priorities are chosen in weighted random order.

A route resolves the client's format when the channel and model support it, and otherwise uses the first enabled format. If a project serves background calls from agents that use model names you did not configure, set a default route so those calls fall back to it; the call detail marks such a call as a fallback.

## Issue an agent key

Open the project's Agent keys page and issue a key. The full key is shown once, so copy it then. A key has a label, optional request-rate and concurrency limits, and a role and trust level that policy rules can read. Only an administrator can set a non-default role or trust level.

## Point an agent at the gateway

Give the agent the gateway address and the key, and use the route name as the model name. Upstream model names stay inside the catalog and never reach the client.

| Agent protocol     | Base URL                   | Where the key goes                  |
| ------------------ | -------------------------- | ----------------------------------- |
| Chat Completions   | `http://127.0.0.1:8080/v1` | `Authorization: Bearer <key>`       |
| Responses          | `http://127.0.0.1:8080/v1` | `Authorization: Bearer <key>`       |
| Anthropic Messages | `http://127.0.0.1:8080`    | `x-api-key: <key>`                  |
| Gemini             | `http://127.0.0.1:8080`    | `x-goog-api-key: <key>`, or `?key=` |

Most clients accept a custom base URL, so Claude Code, Codex, Gemini CLI or a plain HTTP client can all use the same endpoint. An optional `X-SafeAgent-Session` header groups calls from one agent key into one conversation; identity still comes from the key. Use `previous_response_id` when the client supports it.

A first request:

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <agent-key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"public-model","messages":[{"role":"user","content":"hello"}]}'
```

## Observe the console

The call now appears on the project's Calls page. Each row carries the lane, operation, purpose and outcome, so a main call, a background call and an auxiliary protocol call are distinguishable at a glance. Auxiliary calls are hidden until you switch them on, while side calls carry a lane badge and can be folded together.

Open a conversation to see the trajectory: a flow graph by default, with a tree and a flat list over the same nodes. Select a node to inspect its call, item or finding, step forward and backward through the audit events, and open the redaction summary to see each distinct masked value and where it appears. The conversation header reports how many distinct values were masked and how many occurrences the conversation produced.

The project's Risk page aggregates findings by risk source, failure mode, harm and severity, ranks finding codes and agent keys, and links recent high-risk calls; opening an agent key shows the data flows detected for it. Monitor gives traffic, failures and recent high-risk calls across projects. Policies and Redaction hold the ordered rule table and the detector rules; both are validated when saved and take effect immediately.

To check redaction end to end, send a prompt that contains an e-mail address. The request that reaches the provider contains a placeholder such as `[EMAIL_…]`, the console shows the same placeholder, the reply the client receives contains the original address, and the call reports one distinct redacted value.

## Container deployment

Build the image and mount the configuration:

```sh
docker build -t safeagent .
docker run -d --name safeagent -p 127.0.0.1:8080:8080 \
  -v "$PWD/config.toml:/app/config.toml:ro" safeagent
```

Inside a container the service has to listen on all interfaces and reach the database by a resolvable name:

```toml
listen = "0.0.0.0:8080"
database_url = "postgres://safeagent:change-me@db-host:5432/safeagent?sslmode=require"
instance_key = "<output of: openssl rand -base64 32>"
secure_cookies = true
```

With `secure_cookies = true` the console requires HTTPS. Serve it through a reverse proxy that terminates TLS and set `public_origin` to the address users open in the browser, for example `https://safeagent.example.com`. If the proxy connects directly to the application, list its address or CIDR in `trusted_proxies` so the console can read the client address for login rate limiting. The runtime image has no shell or HTTP client, so check `/health` from outside.

## Troubleshooting

| Message or symptom | What to do |
| --- | --- |
| `database_url is required` | Set it in the configuration file, or check that the process reads the file you edited. |
| `instance_key is required` | Add a 32-byte base64 `instance_key`. |
| Unknown configuration key at startup | A key is misspelled; use the name reported in the error. |
| `the database schema does not match this build` | The database belongs to another build. Recreate it; there is no migration. |
| `the database has tables but no schema fingerprint` | The database was created by another tool or a partial start. Recreate it. |
| `another SafeAgent instance owns this database` | Only one instance may use a database. Stop the other process. |
| The console returns 404 | The frontend is not built. Run `make build`. |
| Sign-in works but a reload signs the session out | The deployment uses HTTP with `secure_cookies = true`. Use HTTPS, or set it to false on loopback. |
| Console writes fail behind a proxy | Set `public_origin` to the origin users open in the browser. |
| A provider or model shows a warning in the console | A route references a deleted or disabled object. Restore it, or remove the affected target. |
| A request returns 403, or a stream ends with a tool-call error | A blocking redaction rule, a policy rule or the decision model blocked it. The call record names the source, and asynchronous diagnosis still runs. |
| A background call works but is missing from the conversation | Side and auxiliary calls are folded or hidden by design; switch on the lane filter. |

## Related

- [Architecture](architecture.md)
- [Protocol conversion](protocol-conversion.md)
- [Agent safety](agent-safety.md)
- [Project README](../../README.md)
