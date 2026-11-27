#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
umask 077
lab_env
# shellcheck source=../lib/console.sh
. "$LAB/lib/console.sh"
cd "$LAB"
base=${SAFEAGENT_BASE_URL:-http://127.0.0.1:8080}
runtime="$LAB_RUNTIME"

formats=(chat responses messages gemini)
declare -A channel_format=([chat]=openai_chat [responses]=openai_responses [messages]=anthropic_messages [gemini]=google_gemini)
declare -A default_provider=([chat]=openai [responses]=openai [messages]=anthropic [gemini]=google)
available=()
declare -A channel_model channel_provider channel_url channel_key

for format in "${formats[@]}"; do
  url=$(upstream "$format" URL)
  [ -n "$url" ] || continue
  key=$(upstream "$format" KEY)
  model=$(upstream "$format" MODEL)
  if [ -z "$key" ] || [ -z "$model" ]; then
    echo "SAFEAGENT_UPSTREAM_${format^^}_URL is set but its key or model is missing" >&2
    exit 1
  fi
  available+=("$format")
  channel_url[$format]=$url
  channel_key[$format]=$key
  channel_model[$format]=$model
  channel_provider[$format]=$(upstream "$format" PROVIDER)
  channel_provider[$format]=${channel_provider[$format]:-${default_provider[$format]}}
done

if [ ${#available[@]} -eq 0 ]; then
  echo "set at least one SAFEAGENT_UPSTREAM_<FORMAT>_URL, KEY and MODEL" >&2
  exit 1
fi

admin_password=${SAFEAGENT_ADMIN_PASSWORD:-"Ag1$(openssl rand -hex 12)"}
lab_uid=${SAFEAGENT_LAB_UID:-$(id -u)}
lab_gid=${SAFEAGENT_LAB_GID:-$(id -g)}
if [ "$lab_uid" = "0" ]; then
  lab_uid=1000
  lab_gid=1000
fi
export SAFEAGENT_LAB_UID=$lab_uid SAFEAGENT_LAB_GID=$lab_gid
mkdir -p "$runtime/workspace"/{claude,codex,gemini,pi} "$runtime/screenshots" "$runtime/logs"
for agent in claude codex gemini pi; do
  printf 'bluebird\n' > "$runtime/workspace/$agent/hello.txt"
done

models_json=$runtime/pi-models.json
jq -n --arg base "http://host.docker.internal:8080/v1" '{
  "$schema": "https://pi.dev/schemas/models.schema.json",
  providers: {safeagent: {baseUrl: $base, api: "openai-completions", apiKey: "${SAFEAGENT_KEY}", models: []}}
}' > "$models_json"
for format in "${available[@]}"; do
  route="lab-$format"
  jq --arg id "$route" --arg name "$route" \
    '.providers.safeagent.models += [{id:$id, name:$name, reasoning:true, input:["text"], contextWindow:131072, maxTokens:8192}]' \
    "$models_json" > "$models_json.tmp"
  mv "$models_json.tmp" "$models_json"
done

if [ "${FRESH:-0}" = "1" ]; then
  lab_compose --profile agents --profile browser down --volumes --remove-orphans >/dev/null 2>&1 || true
fi

lab_compose up -d database
for _ in $(seq 1 60); do
  if lab_compose exec -T database pg_isready -U safeagent -d safeagent >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
lab_compose exec -T database pg_isready -U safeagent -d safeagent >/dev/null

tables=$(lab_compose exec -T database psql -U safeagent -d safeagent -tAc \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'" | tr -d '[:space:]')
administrators=0
users_table=$(lab_compose exec -T database psql -U safeagent -d safeagent -tAc \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'users'" | tr -d '[:space:]')
if [ "$users_table" = "1" ]; then
  administrators=$(lab_compose exec -T database psql -U safeagent -d safeagent -tAc \
    "SELECT count(*) FROM users WHERE administrator AND NOT disabled" | tr -d '[:space:]')
fi
if [ "$administrators" != "0" ]; then
  echo "the database already has an administrator; rerun with FRESH=1 to rebuild the lab" >&2
  exit 1
fi

instance_key_file="$runtime/instance.key"
if [ ! -f "$instance_key_file" ] && [ "$tables" != "0" ]; then
  echo "the database is not empty but $instance_key_file is missing; rerun with FRESH=1" >&2
  exit 1
fi
if [ ! -f "$instance_key_file" ]; then
  openssl rand -base64 32 > "$instance_key_file"
fi
if [ "$(base64 -d "$instance_key_file" 2>/dev/null | wc -c)" -ne 32 ]; then
  echo "$instance_key_file must contain 32 bytes encoded as standard base64" >&2
  exit 1
fi
instance_key=$(cat "$instance_key_file")
cat > "$runtime/config.toml" <<EOF
listen = "0.0.0.0:8080"
database_url = "postgres://safeagent:safeagent@127.0.0.1:55432/safeagent?sslmode=disable"
instance_key = "$instance_key"
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
EOF
: > "$runtime/gateway.log"
"$LAB/scripts/restart-gateway.sh"

initial_line=$(sed -n 's/^Initial administrator: //p' "$runtime/gateway.log" | head -1)
initial_user=${initial_line%% /*}
initial=${initial_line#* / }
if [ -z "$initial_line" ] || [ "$initial_user" = "$initial" ]; then
  if [ -f "$runtime/gateway.pid" ]; then
    kill "$(cat "$runtime/gateway.pid")" 2>/dev/null || true
    rm -f "$runtime/gateway.pid"
  fi
  echo "the gateway did not print an initial administrator; rerun with FRESH=1 to rebuild the lab" >&2
  exit 1
fi

CONSOLE_JAR="$runtime/cookies.txt"
console_login "$initial_user" "$initial"
api_put /session/password "$(jq -nc --arg current "$initial" --arg next "$admin_password" '{current:$current,next:$next}')" >/dev/null

project=$(api_post /projects '{"name":"Protocol Lab","retention_days":30,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0}' | jq -r .id)

# The lab reuses the seeded provider rows. A configured format records the supplied upstream URL on
# its channel, which carries the keys, headers and limit; the provider endpoint rows stay untouched.
providers=$(api_get /providers)
for format in "${available[@]}"; do
  provider="${channel_provider[$format]}"
  provider_row=$(jq -c --arg id "$provider" '.[] | select(.id == $id)' <<<"$providers")
  if [ -z "$provider_row" ]; then
    echo "provider $provider is not seeded; known providers: $(jq -r '[.[].id] | join(", ")' <<<"$providers")" >&2
    exit 1
  fi
  if ! jq -e --arg format "${channel_format[$format]}" '.formats | index($format)' <<<"$provider_row" >/dev/null; then
    echo "provider $provider does not expose ${channel_format[$format]}, required by the $format format" >&2
    exit 1
  fi
done

channel_body() {
  local format=$1
  local headers limit
  headers=$(upstream "$format" HEADERS_JSON)
  if [ -z "$headers" ]; then
    headers='{}'
  fi
  jq -e 'type == "object"' <<<"$headers" >/dev/null || {
    echo "SAFEAGENT_UPSTREAM_${format^^}_HEADERS_JSON must be a JSON object" >&2
    exit 1
  }
  limit=$(upstream "$format" DEFAULT_MAX_OUTPUT_TOKENS)
  jq -nc \
    --arg name "Lab $format" \
    --arg provider "${channel_provider[$format]}" \
    --arg endpoint "${channel_url[$format]}" \
    --argjson headers "$headers" \
    --argjson limit "${limit:-0}" \
    '{name:$name,provider_id:$provider,base_url:$endpoint,headers:$headers,default_max_output_tokens:$limit,enabled:true,allow_private_network:true}'
}

declare -A channel_id route_name route_alias route_id model_id
for format in "${available[@]}"; do
  channel_id[$format]=$(api_post /channels "$(channel_body "$format")" | jq -r .id)
  api_post "/channels/${channel_id[$format]}/keys" \
    "$(jq -nc --arg secret "${channel_key[$format]}" '{label:"lab",secret:$secret}')" >/dev/null
done

# One batch create per provider. Formats that share a provider and upstream name share one model.
declare -A batch_models created_models
for format in "${available[@]}"; do
  provider="${channel_provider[$format]}"
  batch_models[$provider]=$(jq -c --arg provider "$provider" --arg model "${channel_model[$format]}" \
    'if any(.[]; .upstream_name == $model) then . else . + [{provider_id:$provider,upstream_name:$model,kind:"generation",currency:"USD",enabled:true}] end' \
    <<<"${batch_models[$provider]:-[]}")
done
for provider in "${!batch_models[@]}"; do
  created_models[$provider]=$(api_post /models/batch "$(jq -nc --argjson models "${batch_models[$provider]}" '{models:$models}')")
done
for format in "${available[@]}"; do
  provider="${channel_provider[$format]}"
  model_id[$format]=$(jq -r --arg model "${channel_model[$format]}" \
    '.[] | select(.upstream_name == $model) | .id' <<<"${created_models[$provider]}")
done

for format in "${available[@]}"; do
  route_name[$format]="lab-$format"
  route_alias[$format]="lab-$format-v1"
  route_id[$format]=$(api_post "/projects/$project/routes" "$(jq -nc \
    --arg name "${route_name[$format]}" --arg alias "${route_alias[$format]}" \
    --arg channel "${channel_id[$format]}" --arg model_id "${model_id[$format]}" \
    --arg format "${channel_format[$format]}" \
    '{name:$name,aliases:[$alias],targets:[{channel_id:$channel,model_id:$model_id,format:$format,priority:0,weight:1}]}')" | jq -r .id)
done

# The default route serves side and auxiliary calls whose model has no route of its own.
default_route=${route_id[chat]:-}
if [ -z "$default_route" ]; then
  default_route=${route_id[${available[0]}]}
fi
api_put "/projects/$project" "$(jq -c --arg route "$default_route" \
  '{name,retention_days,auxiliary_retention_days,default_route_id:$route,requests_per_minute,concurrency,daily_token_quota,default_max_output_tokens,stream_review_bytes,stream_review_timeout_ms,version}' \
  <<<"$(api_get "/projects/$project")")" >/dev/null

key() {
  local label=$1 body
  body=$(jq -nc --arg label "$label" '{label:$label,requests_per_minute:0,concurrency:0,trust_level:1,role:"agent",enabled:true}')
  api_post "/projects/$project/keys" "$body" | jq -r .secret
}

claude_key=$(key claude-code)
codex_key=$(key codex)
gemini_key=$(key gemini)
pi_key=$(key pi)
matrix_key=$(key matrix)

channels_json=$(jq -nc '{}')
for format in "${available[@]}"; do
  channels_json=$(jq -c --arg format "$format" --arg id "${channel_id[$format]}" \
    --arg provider_id "${channel_provider[$format]}" --arg model "${channel_model[$format]}" \
    --arg model_id "${model_id[$format]}" --arg route "${route_name[$format]}" \
    '. + {($format): {id:$id, provider_id:$provider_id, model:$model, model_id:$model_id, route:$route}}' <<<"$channels_json")
done
routes_json=$(jq -nc '{}')
for format in "${available[@]}"; do
  routes_json=$(jq -c --arg format "$format" --arg name "${route_name[$format]}" --arg id "${route_id[$format]}" --arg alias "${route_alias[$format]}" \
    '. + {($format): {name:$name, id:$id, alias:$alias}}' <<<"$routes_json")
done

jq -n \
  --arg base "$base" --arg project "$project" \
  --arg default_route "$default_route" \
  --argjson channels "$channels_json" --argjson routes "$routes_json" \
  --arg claude_key "$claude_key" --arg codex_key "$codex_key" --arg gemini_key "$gemini_key" \
  --arg pi_key "$pi_key" --arg matrix_key "$matrix_key" \
  --arg admin_password "$admin_password" \
  '{base:$base,project:$project,default_route_id:$default_route,channels:$channels,routes:$routes,keys:{claude:$claude_key,codex:$codex_key,gemini:$gemini_key,pi:$pi_key,matrix:$matrix_key},admin_password:$admin_password}' \
  > "$runtime/state.json"

available_list=${available[*]}
cat > "$runtime/env" <<EOF
SAFEAGENT_BASE_URL=$base
SAFEAGENT_PROJECT=$project
SAFEAGENT_AVAILABLE_FORMATS="$available_list"
SAFEAGENT_CLAUDE_KEY=$claude_key
SAFEAGENT_CODEX_KEY=$codex_key
SAFEAGENT_GEMINI_KEY=$gemini_key
SAFEAGENT_PI_KEY=$pi_key
SAFEAGENT_MATRIX_KEY=$matrix_key
SAFEAGENT_ADMIN_PASSWORD=$admin_password
SAFEAGENT_LAB_UID=$lab_uid
SAFEAGENT_LAB_GID=$lab_gid
EOF
for format in "${available[@]}"; do
  printf 'SAFEAGENT_%s_ROUTE=%s\n' "${format^^}" "${route_name[$format]}" >> "$runtime/env"
done

if [ "$(id -u)" = "0" ]; then
  chown -R "$lab_uid:$lab_gid" "$runtime/workspace" "$models_json"
fi

rm -f "$CONSOLE_JAR"
echo "lab ready"
jq '{project, channels, routes, keys:{claude:(.keys.claude[0:12]+"..."),codex:(.keys.codex[0:12]+"..."),gemini:(.keys.gemini[0:12]+"..."),pi:(.keys.pi[0:12]+"..."),matrix:(.keys.matrix[0:12]+"...")}}' "$runtime/state.json"
