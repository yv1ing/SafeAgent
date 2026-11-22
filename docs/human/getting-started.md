# Getting started

[中文](getting-started_zh.md)

This guide starts from a fresh checkout and ends with an agent sending traffic through SafeAgent.

## Prerequisites

Requirements:

- Go 1.27.4
- Node.js 22 or newer
- GNU Make
- a PostgreSQL 17 database that already exists

SafeAgent creates its tables on the first start, but not the database itself. Install the locked dependencies first:

```sh
make deps
```

## Configure and start

Copy the example configuration and set `database_url`:

```sh
cp config.example.toml config.toml
```

```toml
listen = "127.0.0.1:8080"
database_url = "postgres://safeagent:change-me@localhost:5432/safeagent?sslmode=disable"
secure_cookies = false
```

Every other key has a default. Two settings depend on the deployment:

- `secure_cookies`: set to `false` only while the console is reached over plain HTTP on loopback. Any other deployment uses HTTPS and sets it to `true`; a Secure cookie is dropped on plain HTTP.
- `sslmode`: `disable` is for a local database only. A remote database should use TLS.

An unrecognized configuration key stops startup, so a typo is not ignored.

Build and run:

```sh
make build
build/bin/safeagent -config config.toml serve
```

On its first start against an empty database, the process creates the schema and prints the administrator account and password once:

```text
Initial administrator: admin / <password>
```

Keep this password; later starts do not print it again. If it is lost, reset it from another administrator account, or recreate the database.

The same process serves the console at `/console/`, the gateway at `/v1/` and `/v1beta/`, and a health check at `/health`.

## Set up the console

Sign in as `admin` at <http://127.0.0.1:8080/console/>. The first sign-in requires a new password. The console opens on Monitor, which aggregates every visible project.

Create a project. A project name is sufficient; the defaults can be changed later. New projects start with the common redaction detectors enabled and an empty, inspectable policy document.

Opening a project changes the sidebar to that project's pages. Use Settings to change the name and, as an administrator, the retention period and usage limits.

| Role | Can manage |
| --- | --- |
| Project member | routes, non-blocking redaction rules, agent keys; policy documents are read-only |
| Administrator | models, channels, users, the global log, projects, members, policy editing and restore, blocking redaction rules, the decision model, retention and limit fields |

Trust levels and non-default roles are administrator-only. The Activity page lists changes made to the project.

Only administrators maintain the model catalog and channels. A model belongs to a provider and records its upstream name, kind, supported formats and prices per million tokens. The same model can be enabled on several channels, and a channel can enable several formats:

- Choose the provider and one or more API formats. The order is the fallback order. Anthropic, OpenAI, Google, DeepSeek, Z.AI and Qwen offer their supported formats, endpoints, authentication and capability profiles; a custom provider speaks standard Chat Completions and needs a base URL.
- Each format defaults to the provider endpoint. Override a base URL or path only when the provider or a proxy requires it.
- Select the catalog models this channel exposes. A model can be selected only when it shares an enabled format with the channel.
- "Discover" runs per format and lists the names the provider reports. Importing a name creates a missing catalog model or adds the format to an existing generation model.
- Add the upstream key. Only its last characters are shown afterwards. The key is encrypted with the instance key. A channel without a key is accepted by the API but cannot serve traffic, so add one before enabling the route.
- Turn on "allow private network" only for a provider on a private address, such as a self-hosted inference server.

Create a model route. A route maps a public model name to one or more channel models. Each target resolves the client format when the channel and model support it, otherwise the first enabled format. The target with the lowest priority number is tried first; targets with equal priority are chosen in weighted random order. All targets of a route must use models of the same kind.

| Outcome | Falls through to the next target |
| --- | --- |
| transient failure, authentication failure, rate limit, target-format conversion failure | yes |
| policy block, invalid request | no |

Issue an agent key on the project's keys page. The full key is shown once, so copy it then. A key can carry its own rate and concurrency limits. Only an administrator can set a trust level or a role other than the default agent.

If those defaults do not suit the project, adjust redaction and policy. The redaction page can test a rule against sample text. The policies page maintains one ordered rule table; saving validates it and makes it active at once, and the history can restore an earlier version. Only administrators can edit or restore a policy.

A new project starts with no policy rules, so requests are forwarded and only diagnosed in the background. Add a blocking rule to make the gateway stop traffic. Project settings also offer an optional decision model, which can veto a side-effecting tool call after the rules have allowed it.

## Connect an agent

Give the agent the gateway address and an agent key, and use the public model name from the route. Upstream names stay inside the channel configuration.

| Agent protocol     | Base URL                   | Where the key goes                  |
| ------------------ | -------------------------- | ----------------------------------- |
| Chat Completions   | `http://127.0.0.1:8080/v1` | `Authorization: Bearer <key>`       |
| Responses          | `http://127.0.0.1:8080/v1` | `Authorization: Bearer <key>`       |
| Anthropic Messages | `http://127.0.0.1:8080`    | `x-api-key: <key>`                  |
| Gemini             | `http://127.0.0.1:8080`    | `x-goog-api-key: <key>`, or `?key=` |

An optional `X-SafeAgent-Session` header groups calls from one agent key into one conversation. Identity comes from the agent key. Use `previous_response_id` when the client supports it.

Verification request:

```sh
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer <agent-key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"public-model","messages":[{"role":"user","content":"hello"}]}'
```

To verify redaction, send a prompt that contains an e-mail address and open the call in the console. The request sent to the provider contains a placeholder such as `[EMAIL_…]`, while the reply received by the client contains the original address.

## Container deployment

Build the image and mount the configuration:

```sh
docker build -t safeagent .
docker run -d --name safeagent -p 127.0.0.1:8080:8080 \
  -v "$PWD/config.toml:/app/config.toml:ro" safeagent
```

Inside a container the service has to listen on all interfaces and reach the database by a name the container can resolve:

```toml
listen = "0.0.0.0:8080"
database_url = "postgres://safeagent:change-me@db-host:5432/safeagent?sslmode=require"
secure_cookies = true
```

With `secure_cookies = true`, the console requires HTTPS. Serve it through a reverse proxy that handles TLS, and set `public_origin` to the address users open in the browser, for example `https://safeagent.example.com`. If the reverse proxy connects directly to the application, list its CIDR in `trusted_proxies` so the console can read the client address for login rate limiting. The image has no shell or HTTP client, so check `/health` from outside.

## Troubleshooting

| Message or symptom | What to do |
| --- | --- |
| `database_url is required` | Set it in the configuration file, or check that the process is reading the file you edited. |
| Unknown configuration key at startup | A key is misspelled. Use the name reported in the error. |
| `the database schema does not match this build` | The database belongs to another build. Recreate it; there is no migration. |
| `the database has tables but no schema fingerprint` | The database was created by another tool or a partial start. Recreate it; guessing at the missing state is unsafe. |
| `another SafeAgent instance owns this database` | Only one instance may use a database. Stop the other process. |
| The console returns 404 | The frontend is not built. Run `make build`. |
| Sign-in works but a reload signs the session out | The deployment uses HTTP with `secure_cookies = true`. Use HTTPS, or set it to false on loopback. |
| Console writes fail behind a proxy | Set `public_origin` to the origin users open in the browser. |
| A request returns 403, or a stream ends with a tool-call error | A blocking redaction rule, a policy rule or the decision model blocked it. A request-phase block uses HTTP 403; a tool call held after the stream started ends with a protocol-native error. The call record names the source; asynchronous analysis still runs. |

## Related

- [Architecture](architecture.md)
- [Protocol conversion](protocol-conversion.md)
- [Agent safety](agent-safety.md)
- [Project README](../../README.md)
