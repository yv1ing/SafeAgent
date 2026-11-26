#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
require_lab
# shellcheck source=../lib/console.sh
. "$LAB/lib/console.sh"
# shellcheck source=../lib/protocol.sh
. "$LAB/lib/protocol.sh"

umask 077
mkdir -p "$LAB_RUNTIME/security"
admin_jar="$LAB_RUNTIME/security/cookies-admin.txt"
user_jar="$LAB_RUNTIME/security/cookies-user.txt"
CONSOLE_JAR="$admin_jar"
console_login

cleanup() {
  if [ -n "${user_id:-}" ]; then
    api_delete "/projects/$SAFEAGENT_PROJECT/members/$user_id" "$admin_jar"
    api_put "/users/$user_id" '{"disabled":true,"administrator":false}' "$admin_jar" || true
  fi
  if [ -n "${rate_key_id:-}" ]; then
    api_delete "/projects/$SAFEAGENT_PROJECT/keys/$rate_key_id" "$admin_jar"
  fi
  for id in "${project_b:-}" "${project_c:-}"; do
    if [ -n "$id" ]; then api_delete "/projects/$id" "$admin_jar"; fi
  done
  rm -f "$admin_jar" "$user_jar"
}
cleanup_register cleanup

send() {
  local client=$1 model=$2 key=$3 file=$4 body=$5
  gateway_send "$client" "$model" "$key" "$file" "$body" false 60
}

body() {
  local client=$1 model=$2
  gateway_body "$client" "$model" hello false 16
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
  http=$(send "$client" "$probe_model" "invalid-key" "$LAB_RUNTIME/security/invalid-$client.json" "$(body "$client" "$probe_model")")
  check "invalid $client key is rejected" "$http" "401"
done

project_b=$(api_post /projects '{"name":"Isolation Lab","retention_days":7,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0}' | jq -r .id)
key_b=$(api_post "/projects/$project_b/keys" '{"label":"isolation","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)
http=$(send chat "$probe_model" "$key_b" "$LAB_RUNTIME/security/isolation.json" "$(body chat "$probe_model")")
if [ "$http" = "200" ]; then
  fail "a key cannot use another project route"
else
  pass "a key cannot use another project route"
fi

rate_json=$(api_post "/projects/$SAFEAGENT_PROJECT/keys" '{"label":"rate-limit","requests_per_minute":1,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}')
rate_key=$(jq -r .secret <<<"$rate_json")
rate_key_id=$(jq -r .id <<<"$rate_json")
http=$(send chat "$probe_model" "$rate_key" "$LAB_RUNTIME/security/rate-1.json" "$(body chat "$probe_model")")
check "the first request under the rate limit passes" "$http" "200"
http=$(send chat "$probe_model" "$rate_key" "$LAB_RUNTIME/security/rate-2.json" "$(body chat "$probe_model")")
check "the second request is rate limited" "$http" "429"

channel_id=$(jq -r '.channels.chat.id // (.channels | to_entries[0].value.id)' "$LAB_RUNTIME/state.json")
channel_model_id=$(jq -r '.channels.chat.model_id // (.channels | to_entries[0].value.model_id)' "$LAB_RUNTIME/state.json")
project_c=$(api_post /projects '{"name":"Quota Lab","retention_days":1,"requests_per_minute":0,"concurrency":0,"daily_token_quota":1}' | jq -r .id)
api_post "/projects/$project_c/routes" \
  "$(jq -nc --arg channel "$channel_id" --arg model_id "$channel_model_id" --arg name "$probe_model" '{name:$name,targets:[{channel_id:$channel,model_id:$model_id,priority:0,weight:1}]}')" >/dev/null
key_c=$(api_post "/projects/$project_c/keys" '{"label":"quota","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)
http=$(send chat "$probe_model" "$key_c" "$LAB_RUNTIME/security/quota.json" "$(body chat "$probe_model")")
if [ "$http" = "429" ] && jq -e '.error.code == "daily_quota_exceeded"' "$LAB_RUNTIME/security/quota.json" >/dev/null; then
  pass "the daily token quota is enforced before the upstream"
else
  fail "the daily token quota was not enforced: HTTP $http $(head -c 300 "$LAB_RUNTIME/security/quota.json")"
fi

username="member$(date +%s)-$$"
created=$(api_post /users "{\"username\":\"$username\",\"administrator\":false}")
user_id=$(jq -r .id <<<"$created")
initial=$(jq -r .initial_password <<<"$created")
CONSOLE_JAR="$user_jar"
console_login "$username" "$initial"
api_put /session/password "{\"current\":\"$initial\",\"next\":\"SecurePass123456\"}" "$user_jar" >/dev/null
CONSOLE_JAR="$admin_jar"

check "a member cannot list channels" \
  "$(api_status "$user_jar" GET /channels)" "403"
check "a member cannot list users" \
  "$(api_status "$user_jar" GET /users)" "403"
non_member=$(api_status "$user_jar" GET "/projects/$SAFEAGENT_PROJECT/calls?limit=1")
if [ "$non_member" = "403" ] || [ "$non_member" = "404" ]; then
  pass "a non-member cannot read project calls"
else
  fail "a non-member read project calls: HTTP $non_member"
fi

api_post "/projects/$SAFEAGENT_PROJECT/members" "{\"user_id\":\"$user_id\"}" >/dev/null
check "a project member can read project calls" \
  "$(api_status "$user_jar" GET "/projects/$SAFEAGENT_PROJECT/calls?limit=1")" "200"
project_json=$(api_get "/projects/$SAFEAGENT_PROJECT")
project_update=$(jq -c '{name,retention_days,requests_per_minute,concurrency,daily_token_quota,default_max_output_tokens,stream_review_bytes:1,stream_review_timeout_ms:300,version}' <<<"$project_json")
check "a project member cannot change stream review" \
  "$(api_status "$user_jar" PUT "/projects/$SAFEAGENT_PROJECT" "$project_update")" "403"
check "a project member cannot create a channel" \
  "$(api_status "$user_jar" POST /channels '{}')" "403"
check "a project member cannot read the global operation log" \
  "$(api_status "$user_jar" GET /operation-logs)" "403"

send chat "$probe_model" "$SAFEAGENT_MATRIX_KEY" "$LAB_RUNTIME/security/redaction.json" \
  '{"model":"'"$probe_model"'","stream":false,"max_tokens":40,"messages":[{"role":"user","content":"Please repeat alice@example.com"}]}' >/dev/null
redacted_call=""
for _ in $(seq 1 20); do
  redacted_call=$(api_get "/projects/$SAFEAGENT_PROJECT/calls?limit=50" | jq -r '[.items[] | select(.redaction_values > 0)] | first | .id // empty')
  [ -n "$redacted_call" ] && break
  sleep 1
done
if [ -z "$redacted_call" ]; then
  fail "no call with a redaction hit was available for the raw-access check"
else
  admin_detail=$(api_get "/projects/$SAFEAGENT_PROJECT/calls/$redacted_call")
  member_detail=$(api_get "/projects/$SAFEAGENT_PROJECT/calls/$redacted_call" "$user_jar")
  admin_text=$(jq -r '[.transcript.turns[] | select(.role == "user") | .items[].text] | join(" ")' <<<"$admin_detail")
  member_text=$(jq -r '[.transcript.turns[] | select(.role == "user") | .items[].text] | join(" ")' <<<"$member_detail")
  if [[ "$admin_text" == *"alice@example.com"* && "$member_text" != *"alice@example.com"* && "$member_text" == *"[EMAIL_"* ]]; then
    pass "administrators see raw values and members see the redacted view"
  else
    fail "raw/redacted views are not separated (admin=$admin_text member=$member_text)"
  fi
  if api_get "/projects/$SAFEAGENT_PROJECT/operations?limit=50" | jq -e '[.items[].action] | index("readCallRaw") != null' >/dev/null; then
    pass "raw access is written to the operation log"
  else
    fail "raw access was not written to the operation log"
  fi
fi

summary
