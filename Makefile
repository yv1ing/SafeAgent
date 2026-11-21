SHELL := /bin/sh
.SHELLFLAGS := -eu -c
.DEFAULT_GOAL := help

export GOFLAGS ?= -buildvcs=false

CONFIG ?= config.toml
GENERATED := web/src/api/generated
DOCS_EN := README.md testenv/README.md $(shell find docs -name '*.md' ! -name '*_zh.md' | sort)
DOCS_ZH := README_zh.md $(shell find docs -name '*_zh.md' | sort)
LAB_JS := $(shell find testenv -name '*.mjs' | sort)

.PHONY: help deps generate format check contract staticcheck test integration verify build run web dev security clean \
	lab-setup lab-restart lab-agents lab-protocol lab-auxiliary lab-policy lab-analysis lab-security lab-faults lab-browser lab-integration lab-load lab-audit lab-acceptance lab-down

help:
	@printf '%s\n' \
		'Usage: make <target> [CONFIG=config.toml]' \
		'  deps         Install locked Go and frontend dependencies' \
		'  generate     Regenerate the OpenAPI contract and the frontend client' \
		'  format       Format Go, frontend and documentation sources' \
		'  check        Check formatting, staticcheck, vet, types and that the contract is up to date' \
		'  test         Run Go race tests and frontend unit tests' \
		'  integration  Run repository tests against SAFEAGENT_TEST_DATABASE' \
		'  lab-setup    Build and start the acceptance lab: make lab-setup [FRESH=1]' \
		'  lab-restart  Rebuild the console and binary, then restart the lab gateway' \
		'  lab-agents   Run Claude Code, Codex, Gemini CLI and Pi through every lab route' \
		'  lab-protocol Run the live client-by-channel protocol matrix' \
		'  lab-auxiliary Run model, counting, Responses lifecycle and WebSocket checks' \
		'  lab-policy   Check request and tool-call enforcement across client protocols' \
		'  lab-analysis Check asynchronous diagnosis and reversible redaction' \
		'  lab-security Check authentication, quotas, isolation and console permissions' \
		'  lab-faults   Check failover, truncation, tool holds, headers and default limits' \
		'  lab-browser  Run the Playwright console acceptance suite' \
		'  lab-integration Run repository integration tests on the lab database' \
		'  lab-audit    Verify every stored call audit chain on the lab database' \
		'  lab-load     Run the quick four-protocol performance smoke' \
		'  lab-acceptance Run every lab suite in order, then clean up on success' \
		'  lab-down     Stop the lab and remove its containers, volume and runtime files' \
		'  verify       Run check and test' \
		'  build        Build build/bin/safeagent with the console embedded' \
		'  run          Run the backend with CONFIG; serves the console from web/dist' \
		'  web          Run the console dev server on :5173, proxying the API to :8080' \
		'  dev          Run the backend and the console dev server together' \
		'  security     Write dependency inventories, SBOMs and vulnerability reports to build/security' \
		'  clean        Remove build output'

deps:
	go mod download
	npm --prefix web ci --ignore-scripts

generate:
	go run ./cmd/safeagent -output web/openapi.json openapi
	npm --prefix web run generate

format:
	gofmt -w cmd internal testenv web/*.go
	npm --prefix web run format
	npx --prefix web prettier --write --single-quote --trailing-comma all --print-width 100 $(LAB_JS)
	npx --prefix web prettier --write --prose-wrap never --print-width 100 $(DOCS_EN)
	npx --prefix web prettier --write --prose-wrap preserve --print-width 100 $(DOCS_ZH)

check: contract staticcheck
	@unformatted=$$(gofmt -l cmd internal testenv web/*.go); \
		if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi
	go vet ./...
	npm --prefix web run typecheck
	npm --prefix web run format:check
	npx --prefix web prettier --check --single-quote --trailing-comma all --print-width 100 $(LAB_JS)
	npx --prefix web prettier --check --prose-wrap never --print-width 100 $(DOCS_EN)
	npx --prefix web prettier --check --prose-wrap preserve --print-width 100 $(DOCS_ZH)

staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...

# Fails when the committed OpenAPI document or generated client differs from what the code produces.
contract:
	@mkdir -p build
	@trap 'rm -rf build/generated build/openapi.json' EXIT; \
	go run ./cmd/safeagent -output build/openapi.json openapi; \
	cmp -s build/openapi.json web/openapi.json || { echo 'web/openapi.json is stale: run make generate'; exit 1; }; \
	rm -rf build/generated && cp -r $(GENERATED) build/generated; \
	npm --prefix web run generate; \
	diff -r -q build/generated $(GENERATED) || { echo 'generated client is stale: run make generate'; exit 1; }

test:
	go test -race ./...
	npm --prefix web run test

integration:
	@test -n "$$SAFEAGENT_TEST_DATABASE" || { echo 'SAFEAGENT_TEST_DATABASE is not set; use make lab-integration for a disposable database'; exit 1; }
	go test -tags integration -count=1 ./internal/storage/repository/

lab-setup:
	testenv/scripts/setup.sh

lab-restart:
	testenv/scripts/restart-gateway.sh

lab-agents:
	testenv/scripts/run-agents.sh

lab-protocol:
	@set -a; . testenv/runtime/env; set +a; node testenv/protocol/matrix.mjs

lab-auxiliary:
	@set -a; . testenv/runtime/env; set +a; node testenv/protocol/auxiliary.mjs

lab-policy:
	testenv/scripts/policy-acceptance.sh

lab-analysis:
	testenv/scripts/analysis-acceptance.sh

lab-security:
	testenv/scripts/security-acceptance.sh

lab-faults:
	testenv/scripts/faults-acceptance.sh

lab-browser:
	@set -a; . testenv/runtime/env; set +a; \
		docker compose -f testenv/docker-compose.yml build browser && \
		docker compose -f testenv/docker-compose.yml run --rm browser

lab-integration:
	testenv/scripts/integration.sh

lab-audit:
	build/bin/safeagent -config testenv/runtime/config.toml verify-audit

lab-load:
	testenv/scripts/load-acceptance.sh

lab-acceptance:
	testenv/scripts/acceptance.sh

lab-down:
	testenv/scripts/teardown.sh

verify: check test

build:
	npm --prefix web run build
	CGO_ENABLED=0 go build -trimpath -tags embedded -o build/bin/safeagent ./cmd/safeagent

run:
	go run ./cmd/safeagent -config "$(CONFIG)" serve

web:
	npm --prefix web run dev

dev:
	@trap 'kill 0' INT TERM EXIT; \
		go run ./cmd/safeagent -config "$(CONFIG)" serve & \
		npm --prefix web run dev & \
		wait

security:
	@mkdir -p build/security
	@status=0; \
	go list -m -json all > build/security/go-dependencies.json || status=1; \
	go mod verify || status=1; \
	go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0 mod -json -output build/security/backend.cdx.json || status=1; \
	npm --prefix web sbom --sbom-format cyclonedx --package-lock-only > build/security/frontend.cdx.json || status=1; \
	npm --prefix web audit --omit=dev --json > build/security/npm-audit.json || status=1; \
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 -json ./... > build/security/go-vulnerabilities.json || status=1; \
	go run ./cmd/securitycheck build/security/go-vulnerabilities.json || status=1; \
	exit $$status

clean:
	rm -rf build web/dist
