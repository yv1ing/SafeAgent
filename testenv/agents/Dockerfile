FROM node:24-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git jq procps ripgrep \
    && rm -rf /var/lib/apt/lists/*

ARG CLAUDE_CODE_VERSION=latest
ARG CODEX_VERSION=latest
ARG GEMINI_CLI_VERSION=latest
ARG PI_VERSION=latest

RUN npm install --global --no-fund --no-audit \
    @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
    @openai/codex@${CODEX_VERSION} \
    @google/gemini-cli@${GEMINI_CLI_VERSION} \
    @mariozechner/pi-coding-agent@${PI_VERSION}

COPY config/codex.toml /home/node/.codex/config.toml
COPY config/gemini-settings.json /home/node/.gemini/settings.json
RUN mkdir -p /home/node/.pi/agent /home/node/.cache /home/node/.config /home/node/.local \
    && printf '#!/bin/sh\nnpm ls -g --depth=0\n' > /usr/local/bin/agent-versions \
    && chmod +x /usr/local/bin/agent-versions \
    && chmod -R 0777 /home/node

WORKDIR /workspace
