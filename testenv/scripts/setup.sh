#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
runtime="$lab/runtime"
base="http://127.0.0.1:8080"
api="$base/console/api"

cd "$lab"
if [ -f .env ]; then
  # shellcheck disable=SC1091
  set -a; . ./.env; set +a
fi

: "${SAFEAGENT_UPSTREAM_KEY:?set SAFEAGENT_UPSTREAM_KEY in testenv/.env or the environment}"
: "${SAFEAGENT_UPSTREAM_MODEL:?set SAFEAGENT_UPSTREAM_MODEL in testenv/.env or the environment}"
: "${SAFEAGENT_UPSTREAM_CHAT_URL:?set SAFEAGENT_UPSTREAM_CHAT_URL in testenv/.env or the environment}"
: "${SAFEAGENT_UPSTREAM_RESPONSES_URL:?set SAFEAGENT_UPSTREAM_RESPONSES_URL in testenv/.env or the environment}"
: "${SAFEAGENT_UPSTREAM_MESSAGES_URL:?set SAFEAGENT_UPSTREAM_MESSAGES_URL in testenv/.env or the environment}"
admin_password=${SAFEAGENT_ADMIN_PASSWORD:-"Ag1$(openssl rand -hex 12)"}
upstream_model=$SAFEAGENT_UPSTREAM_MODEL
chat_url=$SAFEAGENT_UPSTREAM_CHAT_URL
responses_url=$SAFEAGENT_UPSTREAM_RESPONSES_URL
messages_url=$SAFEAGENT_UPSTREAM_MESSAGES_URL

mkdir -p "$runtime/workspace/claude" "$runtime/workspace/codex" "$runtime/workspace/gemini" "$runtime/workspace/pi" "$runtime/screenshots"
printf 'bluebird\n' > "$runtime/workspace/claude/hello.txt"
printf 'bluebird\n' > "$runtime/workspace/codex/hello.txt"
printf 'bluebird\n' > "$runtime/workspace/gemini/hello.txt"
printf 'bluebird\n' > "$runtime/workspace/pi/hello.txt"

jq -n --arg model "$upstream_model" '{
  "$schema": "https://pi.dev/schemas/models.schema.json",
  providers: {
    safeagent: {
      baseUrl: "http://host.docker.internal:8080/v1",
      api: "openai-completions",
      apiKey: "${SAFEAGENT_KEY}",
      models: [
        {id: ($model + "-via-chat"), name: ($model + " via chat"), reasoning: true, input: ["text"], contextWindow: 131072, maxTokens: 8192},
        {id: ($model + "-via-responses"), name: ($model + " via responses"), reasoning: true, input: ["text"], contextWindow: 131072, maxTokens: 8192},
        {id: ($model + "-via-messages"), name: ($model + " via messages"), reasoning: true, input: ["text"], contextWindow: 131072, maxTokens: 8192}
      ]
    }
  }
}' > "$runtime/pi-models.json"

chown -R 1000:1000 "$runtime/workspace"
chown 1000:1000 "$runtime/pi-models.json"

if [ "${FRESH:-0}" = "1" ]; then
  docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
fi

docker compose up -d database
for _ in $(seq 1 60); do
  if docker compose exec -T database pg_isready -U safeagent -d safeagent >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker compose exec -T database pg_isready -U safeagent -d safeagent >/dev/null

cat > "$runtime/config.toml" <<EOF
listen = "0.0.0.0:8080"
database_url = "postgres://safeagent:safeagent@127.0.0.1:55432/safeagent?sslmode=disable"
secure_cookies = false
public_origin = ""
max_request_bytes = 8388608
max_response_bytes = 33554432
max_stream_bytes = 67108864
stream_idle_seconds = 120
max_attempts = 3
shutdown_seconds = 30
operation_log_days = 365
tool_hold_bytes = 1048576
analysis_concurrency = 2
EOF
: > "$runtime/gateway.log"
"$lab/scripts/restart-gateway.sh"

initial=$(sed -n 's/^Initial administrator: admin \/ //p' "$runtime/gateway.log" | head -1)
if [ -z "$initial" ]; then
  echo "the database already has an administrator; rerun with FRESH=1 to rebuild the lab" >&2
  exit 1
fi

jar="$runtime/cookies.txt"
marker=(-H 'X-SafeAgent-Request: 1' -H "Origin: $base" -H 'Content-Type: application/json')
curl -sS --fail-with-body -c "$jar" -X POST "$api/session" "${marker[@]}" \
  -d "{\"username\":\"admin\",\"password\":\"$initial\"}" >/dev/null
curl -sS --fail-with-body -b "$jar" -X PUT "$api/session/password" "${marker[@]}" \
  -d "{\"current\":\"$initial\",\"next\":\"$admin_password\"}" >/dev/null

post() {
  curl -sS --fail-with-body -b "$jar" -X POST "$api$1" "${marker[@]}" -d "$2"
}

project=$(post /projects '{"name":"Protocol Lab","retention_days":30,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0}' | jq -r .id)

channel() {
  local name=$1 format=$2 vendor=$3 endpoint=$4 body
  body=$(jq -nc --arg name "$name" --arg format "$format" --arg vendor "$vendor" --arg endpoint "$endpoint" --arg model "$upstream_model" \
    '{name:$name,format:$format,vendor:$vendor,endpoint:$endpoint,models:[{name:$model}],currency:"USD",enabled:true,allow_private_network:true}')
  post /channels "$body" | jq -r .id
}

chat=$(channel "Chat" openai_chat openai "$chat_url")
responses=$(channel "Responses" openai_responses openai "$responses_url")
messages=$(channel "Messages" anthropic_messages anthropic "$messages_url")

for id in "$chat" "$responses" "$messages"; do
  post "/channels/$id/keys" "$(jq -nc --arg secret "$SAFEAGENT_UPSTREAM_KEY" '{label:"lab",secret:$secret}')" >/dev/null
done

route() {
  local name=$1 channel=$2 body
  body=$(jq -nc --arg name "$name" --arg channel "$channel" --arg model "$upstream_model" \
    '{name:$name,targets:[{channel_id:$channel,upstream_model:$model,priority:0,weight:1}]}')
  post "/projects/$project/routes" "$body" >/dev/null
}

chat_route="${upstream_model}-via-chat"
responses_route="${upstream_model}-via-responses"
messages_route="${upstream_model}-via-messages"
route "$chat_route" "$chat"
route "$responses_route" "$responses"
route "$messages_route" "$messages"

key() {
  local label=$1 body
  body=$(jq -nc --arg label "$label" '{label:$label,requests_per_minute:0,concurrency:0,trust_level:1,role:"agent",enabled:true}')
  post "/projects/$project/keys" "$body" | jq -r .secret
}

claude_key=$(key claude-code)
codex_key=$(key codex)
gemini_key=$(key gemini)
pi_key=$(key pi)
matrix_key=$(key matrix)

jq -n \
  --arg base "$base" \
  --arg project "$project" \
  --arg chat "$chat_route" --arg responses "$responses_route" --arg messages "$messages_route" \
  --arg claude_key "$claude_key" --arg codex_key "$codex_key" --arg gemini_key "$gemini_key" \
  --arg pi_key "$pi_key" --arg matrix_key "$matrix_key" \
  --arg admin_password "$admin_password" --arg upstream_model "$upstream_model" \
  '{base:$base,project:$project,models:{chat:$chat,responses:$responses,messages:$messages},keys:{claude:$claude_key,codex:$codex_key,gemini:$gemini_key,pi:$pi_key,matrix:$matrix_key},admin_password:$admin_password,upstream_model:$upstream_model}' \
  > "$runtime/state.json"

cat > "$runtime/env" <<EOF
SAFEAGENT_BASE_URL=$base
SAFEAGENT_PROJECT=$project
SAFEAGENT_CHAT_MODEL=$chat_route
SAFEAGENT_RESPONSES_MODEL=$responses_route
SAFEAGENT_MESSAGES_MODEL=$messages_route
SAFEAGENT_UPSTREAM_MODEL=$upstream_model
SAFEAGENT_UPSTREAM_CHAT_URL="$chat_url"
SAFEAGENT_MATRIX_KEY=$matrix_key
SAFEAGENT_CLAUDE_KEY=$claude_key
SAFEAGENT_CODEX_KEY=$codex_key
SAFEAGENT_GEMINI_KEY=$gemini_key
SAFEAGENT_PI_KEY=$pi_key
SAFEAGENT_ADMIN_PASSWORD=$admin_password
EOF

echo "lab ready"
jq '{project,models,keys:{claude:(.keys.claude[0:12]+"..."),codex:(.keys.codex[0:12]+"..."),gemini:(.keys.gemini[0:12]+"..."),pi:(.keys.pi[0:12]+"..."),matrix:(.keys.matrix[0:12]+"...")}}' "$runtime/state.json"
