# Verification

[中文](verification_zh.md)

SafeAgent is verified in layers: static and contract checks, unit tests, tests against a real database, and an acceptance lab that drives a running gateway with real clients and a real upstream. The first layers run on any checkout; the lab needs Docker and provider credentials.

## What the checks cover

- **Static and contract checks** cover formatting, static analysis, the console's type checking, and whether the API contract and client match the code.
- **Unit tests** cover all four protocols in both directions, streaming boundaries and tool hold, stream review, the four online checkpoints, the signed audit chain, policy evaluation, every detector, call classification, the trajectory view and redaction. The console's own tests cover its view helpers.
- **Documentation stays in step.** The bilingual structure and local links are checked, so a stale document fails the build.
- **Database tests** run against a real PostgreSQL and cover call settlement, cascading deletion, route resolution, quota reservation and settlement, retention by lane, and tamper detection in an audit chain.
- **The acceptance lab** runs the real protocols end to end: a client message through a configured channel to a real upstream, the auxiliary endpoints, the four agent CLIs, online policy, asynchronous diagnosis, security boundaries, failure handling, the trajectory projection, the browser console, a short performance comparison and the stored audit chains. A case it cannot run is recorded as a skip with a reason instead of passing silently.

The commands for each layer, and the conventions the tests follow, are in the [development guide](../agent/development.md). The lab's contract, suites and troubleshooting are in the [acceptance lab guide](../agent/testenv.md) and its [README](../../testenv/README.md).

## What the checks cannot show

- Model behavior varies between runs and providers. Tool and agent cases are not reproducible the way unit tests are; the lab checks that a client works unchanged through the gateway, not the quality of an answer.
- Some cases skip by design: media input on a text-only channel, reasoning content when the model returns none, a native Gemini deep task without a Gemini upstream, or an auxiliary capability no configured channel declares. A skip is a statement about coverage, not a pass.
- Stream review covers the configured leading window only. Text after it is diagnosed, not blocked, and that boundary is covered by unit tests.
- Performance numbers compare against a direct baseline in the same environment. They are a regression signal, not a capacity claim; the short smoke run makes no claim at all.
- The security checks cover the application and its dependencies. They do not audit the operator's network, database or key management.
- Coverage depends on how much of an agent's traffic is routed through the gateway.

## Further reading

- [Architecture](architecture.md)
- [Agent safety](agent-safety.md)
- [Landscape](landscape.md)
- [Acceptance lab guide](../agent/testenv.md)
