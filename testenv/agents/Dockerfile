FROM node:24-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl git jq procps ripgrep \
    && rm -rf /var/lib/apt/lists/*

RUN npm install --global --no-fund --no-audit \
    @anthropic-ai/claude-code@2.1.329 \
    @openai/codex@0.170.0 \
    @google/gemini-cli@0.70.0 \
    @mariozechner/pi-coding-agent@0.73.1

COPY config/codex.toml /home/node/.codex/config.toml
COPY config/gemini-settings.json /home/node/.gemini/settings.json
RUN mkdir -p /home/node/.pi/agent \
    && chown -R node:node /home/node/.codex /home/node/.gemini /home/node/.pi

WORKDIR /workspace
