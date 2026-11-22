#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
runtime="$lab/runtime"
umask 077
base="http://127.0.0.1:8080"
api="$base/console/api"

if [ ! -f "$runtime/env" ]; then
  echo "the lab is not set up; run make lab-setup first" >&2
  exit 1
fi
# shellcheck source=lib.sh
. "$lab/scripts/lib.sh"
lab_env "$lab"

admin_jar="$runtime/cookies-security-admin.txt"
user_jar="$runtime/cookies-security-user.txt"
marker=(-H 'Content-Type: application/json' -H 'X-SafeAgent-Request: 1' -H "Origin: $base")
curl -fsS -c "$admin_jar" -X POST "$api/session" "${marker[@]}" \
  -d "{\"username\":\"admin\",\"password\":\"$SAFEAGENT_ADMIN_PASSWORD\"}" >/dev/null

cleanup() {
  if [ -n "${user_id:-}" ]; then
    curl -sS -b "$admin_jar" "${marker[@]}" -X DELETE "$api/projects/$SAFEAGENT_PROJECT/members/$user_id" >/dev/null 2>&1 || true
    curl -sS -b "$admin_jar" "${marker[@]}" -X PUT "$api/users/$user_id" -d '{"disabled":true,"administrator":false}' >/dev/null 2>&1 || true
  fi
  if [ -n "${rate_key_id:-}" ]; then
    curl -sS -b "$admin_jar" "${marker[@]}" -X DELETE "$api/projects/$SAFEAGENT_PROJECT/keys/$rate_key_id" >/dev/null 2>&1 || true
  fi
  for id in "${project_b:-}" "${project_c:-}"; do
    [ -n "$id" ] && curl -sS -b "$admin_jar" "${marker[@]}" -X DELETE "$api/projects/$id" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

admin_post() { curl -fsS -b "$admin_jar" "${marker[@]}" -X POST "$api$1" -d "$2"; }
admin_get() { curl -fsS -b "$admin_jar" "$api$1"; }
status=0
passed=0
failed=0

check() {
  local label=$1 actual=$2 expected=$3
  if [ "$actual" = "$expected" ]; then
    printf 'PASS %s\n' "$label"
    passed=$((passed + 1))
  else
    printf 'FAIL %s: got %s, want %s\n' "$label" "$actual" "$expected"
    failed=$((failed + 1)); status=1
  fi
}

send() {
  local client=$1 model=$2 key=$3 file=$4 body=$5
  case "$client" in
    chat)
      curl -sS -o "$file" -w '%{http_code}' -m 60 "$base/v1/chat/completions" \
        -H "Authorization: Bearer $key" -H 'Content-Type: application/json' -d "$body"
      ;;
    responses)
      curl -sS -o "$file" -w '%{http_code}' -m 60 "$base/v1/responses" \
        -H "Authorization: Bearer $key" -H 'Content-Type: application/json' -d "$body"
      ;;
    messages)
      curl -sS -o "$file" -w '%{http_code}' -m 60 "$base/v1/messages" \
        -H "x-api-key: $key" -H 'anthropic-version: 2023-06-01' \
        -H 'Content-Type: application/json' -d "$body"
      ;;
    gemini)
      curl -sS -o "$file" -w '%{http_code}' -m 60 "$base/v1beta/models/$model:generateContent" \
        -H "x-goog-api-key: $key" -H 'Content-Type: application/json' -d "$body"
      ;;
  esac
}

body() {
  local client=$1 model=$2
  case "$client" in
    chat) jq -nc --arg model "$model" '{model:$model,stream:false,max_tokens:16,messages:[{role:"user",content:"hello"}]}' ;;
    responses) jq -nc --arg model "$model" '{model:$model,stream:false,max_output_tokens:16,input:"hello"}' ;;
    messages) jq -nc --arg model "$model" '{model:$model,stream:false,max_tokens:16,messages:[{role:"user",content:"hello"}]}' ;;
    gemini) jq -nc '{contents:[{role:"user",parts:[{text:"hello"}]}],generationConfig:{maxOutputTokens:16}}' ;;
  esac
}

formats=($SAFEAGENT_AVAILABLE_FORMATS)
declare -A route
load_routes
if [ -n "${route[chat]:-}" ]; then
  probe_model=${route[chat]}
else
  probe_model=${route[${formats[0]}]}
fi

for client in chat responses messages gemini; do
  http=$(send "$client" "$probe_model" "invalid-key" "$runtime/security-invalid-$client.json" "$(body "$client" "$probe_model")")
  check "invalid $client key is rejected" "$http" "401"
done

project_b=$(admin_post /projects '{"name":"Isolation Lab","retention_days":7,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0}' | jq -r .id)
key_b=$(admin_post "/projects/$project_b/keys" '{"label":"isolation","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)
http=$(send chat "$probe_model" "$key_b" "$runtime/security-isolation.json" "$(body chat "$probe_model")")
if [ "$http" = "200" ]; then
  printf 'FAIL a key cannot use another project route\n'; failed=$((failed + 1)); status=1
else
  printf 'PASS a key cannot use another project route\n'; passed=$((passed + 1))
fi

rate_json=$(admin_post "/projects/$SAFEAGENT_PROJECT/keys" '{"label":"rate-limit","requests_per_minute":1,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}')
rate_key=$(jq -r .secret <<<"$rate_json")
rate_key_id=$(jq -r .id <<<"$rate_json")
http=$(send chat "$probe_model" "$rate_key" "$runtime/security-rate-1.json" "$(body chat "$probe_model")")
check "the first request under the rate limit passes" "$http" "200"
http=$(send chat "$probe_model" "$rate_key" "$runtime/security-rate-2.json" "$(body chat "$probe_model")")
check "the second request is rate limited" "$http" "429"

channel_id=$(jq -r '.channels.chat.id // (.channels | to_entries[0].value.id)' "$runtime/state.json")
channel_model_id=$(jq -r '.channels.chat.model_id // (.channels | to_entries[0].value.model_id)' "$runtime/state.json")
project_c=$(admin_post /projects '{"name":"Quota Lab","retention_days":1,"requests_per_minute":0,"concurrency":0,"daily_token_quota":1}' | jq -r .id)
admin_post "/projects/$project_c/routes" \
  "$(jq -nc --arg channel "$channel_id" --arg model_id "$channel_model_id" --arg name "$probe_model" '{name:$name,targets:[{channel_id:$channel,model_id:$model_id,priority:0,weight:1}]}')" >/dev/null
key_c=$(admin_post "/projects/$project_c/keys" '{"label":"quota","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)
http=$(send chat "$probe_model" "$key_c" "$runtime/security-quota.json" "$(body chat "$probe_model")")
if [ "$http" = "429" ] && jq -e '.error.code == "daily_quota_exceeded"' "$runtime/security-quota.json" >/dev/null; then
  printf 'PASS the daily token quota is enforced before the upstream\n'; passed=$((passed + 1))
else
  printf 'FAIL the daily token quota was not enforced: HTTP %s %s\n' "$http" "$(head -c 300 "$runtime/security-quota.json")"
  failed=$((failed + 1)); status=1
fi

username="member$(date +%s)-$$"
created=$(admin_post /users "{\"username\":\"$username\",\"administrator\":false}")
user_id=$(jq -r .id <<<"$created")
initial=$(jq -r .initial_password <<<"$created")
curl -fsS -c "$user_jar" -X POST "$api/session" "${marker[@]}" \
  -d "{\"username\":\"$username\",\"password\":\"$initial\"}" >/dev/null
curl -fsS -b "$user_jar" -X PUT "$api/session/password" "${marker[@]}" \
  -d "{\"current\":\"$initial\",\"next\":\"SecurePass123456\"}" >/dev/null

check "a member cannot list channels" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$user_jar" "$api/channels")" "403"
check "a member cannot list users" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$user_jar" "$api/users")" "403"
non_member=$(curl -sS -o /dev/null -w '%{http_code}' -b "$user_jar" "$api/projects/$SAFEAGENT_PROJECT/calls?limit=1")
if [ "$non_member" = "403" ] || [ "$non_member" = "404" ]; then
  printf 'PASS a non-member cannot read project calls\n'; passed=$((passed + 1))
else
  printf 'FAIL a non-member read project calls: HTTP %s\n' "$non_member"; failed=$((failed + 1)); status=1
fi

admin_post "/projects/$SAFEAGENT_PROJECT/members" "{\"user_id\":\"$user_id\"}" >/dev/null
check "a project member can read project calls" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$user_jar" "$api/projects/$SAFEAGENT_PROJECT/calls?limit=1")" "200"
check "a project member cannot create a channel" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$user_jar" -X POST "$api/channels" "${marker[@]}" -d '{}')" "403"
check "a project member cannot read the global operation log" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$user_jar" "$api/operation-logs")" "403"

send chat "$probe_model" "$SAFEAGENT_MATRIX_KEY" "$runtime/security-redaction.json" \
  '{"model":"'"$probe_model"'","stream":false,"max_tokens":40,"messages":[{"role":"user","content":"Please repeat alice@example.com"}]}' >/dev/null
redacted_call=""
for _ in $(seq 1 20); do
  redacted_call=$(admin_get "/projects/$SAFEAGENT_PROJECT/calls?limit=50" | jq -r '[.items[] | select(.redaction_hits > 0)] | first | .id // empty')
  [ -n "$redacted_call" ] && break
  sleep 1
done
if [ -z "$redacted_call" ]; then
  printf 'FAIL no call with a redaction hit was available for the raw-access check\n'
  failed=$((failed + 1)); status=1
else
  admin_detail=$(admin_get "/projects/$SAFEAGENT_PROJECT/calls/$redacted_call")
  member_detail=$(curl -fsS -b "$user_jar" "$api/projects/$SAFEAGENT_PROJECT/calls/$redacted_call")
  admin_text=$(jq -r '[.transcript.turns[] | select(.role == "user") | .items[].text] | join(" ")' <<<"$admin_detail")
  member_text=$(jq -r '[.transcript.turns[] | select(.role == "user") | .items[].text] | join(" ")' <<<"$member_detail")
  if [[ "$admin_text" == *"alice@example.com"* && "$member_text" != *"alice@example.com"* && "$member_text" == *"[EMAIL_"* ]]; then
    printf 'PASS administrators see raw values and members see the redacted view\n'; passed=$((passed + 1))
  else
    printf 'FAIL raw/redacted views are not separated (admin=%s member=%s)\n' "$admin_text" "$member_text"
    failed=$((failed + 1)); status=1
  fi
  admin_get "/projects/$SAFEAGENT_PROJECT/operations?limit=50" | jq -e '[.items[].action] | index("readCallRaw") != null' >/dev/null &&
    { printf 'PASS raw access is written to the operation log\n'; passed=$((passed + 1)); } ||
    { printf 'FAIL raw access was not written to the operation log\n'; failed=$((failed + 1)); status=1; }
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$status"
