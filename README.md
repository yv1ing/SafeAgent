<h1 align="center">SafeAgent</h1>

<p align="center"><strong>Make every agent call usable, controllable and traceable</strong></p>

<p align="center">English · <a href="README_zh.md">中文</a></p>

SafeAgent is a self-hostable security gateway for governing agent behavior. It sits between agent clients and model providers, accepts the four APIs agents commonly speak, masks sensitive values before they leave the organization, checks each request and tool call against project policy, and records every call in a signed, replayable audit trail. Operators use one console to watch traffic, understand what an agent did, and stop a risky action before it reaches a business system.

## Three common deployment problems

Agent deployments tend to fail in three places.

**Fragmented interfaces.** Chat Completions, Responses, Anthropic Messages and Gemini coexist, and every new client or provider adds another pair of adapters to maintain. SafeAgent converts all four through one canonical model, so the integration work does not grow with every combination.

**Sensitive data exposure.** Requests carry customer records, internal identifiers and credentials. Masking implemented separately in each client is inconsistent at best. SafeAgent masks values at the boundary and restores them on the way back; policy and diagnosis work on the masked text.

**Behavior that cannot be explained later.** A tool call can read a file, send a message or change a system. Ordinary logs cannot show who triggered it, what it touched, or whether the record was altered afterwards. SafeAgent traces content from its origin to the tool that used it, blocks disallowed actions while the call is in flight, and signs every audit event.

## Core capabilities

### Protocol conversion

Chat Completions, Responses, Anthropic Messages and Gemini convert in both directions through one canonical model. The client and the upstream may use different formats, and the gateway encodes each side in its own protocol. Streaming and non-streaming calls share the same semantics. When a target format cannot carry a field, the call records the loss instead of dropping it silently.

### Sensitive data boundary

Detectors replace sensitive values before a request leaves the gateway, so the model and the provider only see masked text. The response restores the original values from the call's mapping. A masked value keeps a stable identity across a conversation, which lets the console count and display it once instead of accumulating a copy on every continuation. Each call stores the rule set it ran under; historical calls are shown with those rules.

### Behavior tracing and online policy

Every call becomes a protocol-neutral trace: where content came from, whether it is trusted, and what a tool call can do. Project rules run before the upstream call, before a tool call is released, before tool output returns to the model, and before a response is written. A project may also review the leading part of a streaming response before the first byte reaches the client.

### Editable model supply and routing

Providers, channels, models, routes and targets are configuration records. Operators connect a provider once, select the models it exposes, and map a public model name to one or more channel-and-model pairs. A channel or model can be removed with an impact preview; routes that depended on it degrade visibly instead of disappearing.

### Operator console

The console shows traffic and failures, calls and conversations, risk findings and data flows, policy history, redaction rules, model supply and routing, and access control. Raw audit data is visible to administrators only, and operations that change state are recorded.

## Call path

1. The gateway authenticates the agent key, decodes the request, and classifies it by endpoint operation, purpose and lane.
2. Sensitive values are masked; the call is recorded as a trace of content origins, trust and tool destinations.
3. The public model name resolves to a route. The request checkpoint and any new tool results are checked before the upstream sees them.
4. The upstream streams a response. Text and reasoning pass through, while a complete tool call waits for its verdict.
5. The response checkpoint runs before the body or the first byte is written, masked values are restored, and the call is sealed into the audit chain. A background worker diagnoses finished main calls.

## Quick start

SafeAgent stores its data in PostgreSQL 17. Building it needs Go 1.27.4, Node.js 22 or newer, and GNU Make.

```sh
make deps
cp config.example.toml config.toml   # set database_url and instance_key
make build
build/bin/safeagent -config config.toml serve
```

On first start the service initializes its database and prints the administrator password once. Open <http://127.0.0.1:8080/console/>, sign in, add a channel and a route, and issue an agent key. The [getting-started guide](docs/human/getting-started.md) covers the whole path.

## Deployment and scope

SafeAgent is deployed inside the operator's infrastructure. Agents reach models through one address, and call records stay inside the operator's environment. Coverage depends on how much of an agent's traffic is routed through the gateway. Images, audio and other binary payloads are not inspected or rewritten. Stream review covers only the configured leading window. Tool destinations and data flows are inferred and need human confirmation. Human approval queues and cross-call correlation are not implemented.

## Documentation

- [Getting started](docs/human/getting-started.md)
- [Architecture](docs/human/architecture.md)
- [Protocol conversion](docs/human/protocol-conversion.md)
- [Agent safety](docs/human/agent-safety.md)
- [Landscape](docs/human/landscape.md)
- [Verification](docs/human/verification.md)
- [Development guide](docs/agent/README.md)

## License

[Apache License 2.0](LICENSE)
