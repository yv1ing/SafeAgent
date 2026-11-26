# Acceptance lab

`testenv/` is a disposable environment for checks that need a real provider, the four agent CLIs and a headless browser. It is not a product deployment: PostgreSQL, the agents and the browser run in Docker, while the gateway runs on the host so a change can be rebuilt without an image build. The [acceptance lab guide](../docs/agent/testenv.md) owns the topology, environment contract, suite table and troubleshooting.

## Quick start

```sh
cp testenv/env.example testenv/.env   # fill a real upstream per format
make lab-setup
make lab-acceptance                    # every suite in order, then stop the lab
```

At least one provider format is required. A format with no URL is skipped, and the protocol and agent suites size themselves to the routes that exist. Set `SAFEAGENT_UPSTREAM_<FORMAT>_ENDPOINT` for every configured format before the performance suite or a full acceptance run. `FRESH=1 make lab-setup` recreates the database and is mandatory after a schema change.

## Layout

`testenv/lib` holds the shared shell and Node layers, `testenv/scripts` the operator entry points, `testenv/suites` the shell acceptance suites and `testenv/protocol` the Node suites. Each suite keeps its cases and sources the layers it needs. `testenv/agents`, `testenv/browser`, `testenv/load` and `testenv/faults` hold the images and utilities.

## Evidence and secrets

The one-command acceptance log and the protocol report are written to `build/lab` and survive `make lab-down`. Everything else stays under `testenv/runtime`: request and response bodies, agent logs and answers, screenshots and `report.json`, and performance reports. `testenv/.env` holds provider keys, and `testenv/runtime` holds the administrator password, agent keys and the instance key; review both before sharing and never expose port 8080 beyond the development host. The fixed database password and the private-network exception are lab-only and must not be copied into a deployment.

## Related

- [Acceptance lab guide](../docs/agent/testenv.md)
- [Development](../docs/agent/development.md)
- [Verification method](../docs/human/verification.md)
