# Verification

[中文](verification_zh.md)

SafeAgent is checked from the inside out: static and contract checks, race-enabled Go tests, repository tests against a real PostgreSQL, and a Docker lab that drives a running gateway with real clients and a real upstream. The local layers run on any checkout. The lab needs Docker and provider credentials.

## What is verified

- **Static and contract checks** cover formatting, static analysis, vet, the console's TypeScript, and the committed API contract and generated client, which must match the code.
- **Unit tests** cover the four codecs in both directions, the stream session's commit boundary and tool hold, stream review, the four online checkpoints, the delivered-only audit projection, the HMAC event chain, policy compilation and evaluation, every detector, the classification axes, the trajectory projection and redaction value identity. The console's own tests cover its view helpers.
- **Documentation is part of the contract**: the bilingual section structure, the local links, and the make targets and repository paths named in the docs are checked, so a stale document fails the build.
- **Repository integration tests** run against a real PostgreSQL and cover what an in-memory fake cannot: transactions that settle a call, soft delete cascading to route targets, route resolution by exact name, alias and project default, quota reservation and settlement, retention by lane, and tamper location in an audit chain.
- **The acceptance lab** runs the real protocols end to end: a client message through a configured channel to a real upstream, the auxiliary endpoints, the agent CLIs, online policy, asynchronous diagnosis, security boundaries, failure handling, the trajectory projection, the browser console, a short performance comparison and the stored audit chains. A case it cannot run is recorded as a skip with a reason instead of passing silently.

The commands for each layer, and the conventions the tests follow, are in the [development guide](../agent/development.md). The lab's contract, suites and troubleshooting are in the [acceptance lab guide](../agent/testenv.md) and its [README](../../testenv/README.md).

## What it does not prove

- Model behaviour varies between runs and providers. Tool and agent cases are not reproducible the way unit tests are; the lab checks that a client works unchanged through the gateway, not the quality of an answer.
- Some cases skip by design: media input on a text-only channel, reasoning content when the model returns none, a native Gemini deep task without a Gemini upstream, or an auxiliary capability no configured channel declares. A skip is a coverage fact, not a pass.
- Stream review covers the configured leading window only. Text after it is diagnosed, not blocked, and that boundary is covered by unit tests.
- Performance numbers compare against a direct baseline in the same environment. They are a regression signal, not a capacity claim; the short smoke run makes no claim at all.
- The security checks cover the application and its dependencies. They do not audit the operator's network, database or key management.
- Coverage depends on how much of an agent's traffic is routed through the gateway.

## Related

- [Architecture](architecture.md)
- [Agent safety](agent-safety.md)
- [Landscape](landscape.md)
- [Acceptance lab guide](../agent/testenv.md)
