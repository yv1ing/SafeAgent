#!/usr/bin/env bash
set -euo pipefail

lab=$(cd "$(dirname "$0")/.." && pwd)
runtime="$lab/runtime"
root=$(cd "$lab/.." && pwd)
base="http://127.0.0.1:8080"
api="$base/console/api"

if [ ! -f "$runtime/env" ]; then
  echo "the lab is not set up; run make lab-setup first" >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
. "$runtime/env"
set +a

jar="$runtime/cookies-faults.txt"
marker=(-H 'Content-Type: application/json' -H 'X-SafeAgent-Request: 1' -H "Origin: $base")
curl -fsS -c "$jar" -X POST "$api/session" "${marker[@]}" \
  -d "{\"username\":\"admin\",\"password\":\"$SAFEAGENT_ADMIN_PASSWORD\"}" >/dev/null

post() { curl -fsS -b "$jar" "${marker[@]}" -X POST "$api$1" -d "$2"; }
get() { curl -fsS -b "$jar" "$api$1"; }

cd "$root"
go build -trimpath -o "$runtime/fault-server" ./testenv/faults
"$runtime/fault-server" -listen 127.0.0.1:18080 > "$runtime/faults.log" 2>&1 &
fault_pid=$!
cleanup() {
  kill "$fault_pid" 2>/dev/null || true
  if [ -n "${project:-}" ]; then
    curl -sS -b "$jar" "${marker[@]}" -X DELETE "$api/projects/$project" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
for _ in $(seq 1 50); do
  curl -fsS http://127.0.0.1:18080/chat/text >/dev/null 2>&1 && break
  sleep 0.1
done
if ! kill -0 "$fault_pid" 2>/dev/null; then
  echo "the fault upstream failed to start on 127.0.0.1:18080" >&2
  cat "$runtime/faults.log" >&2
  exit 1
fi

project=$(post /projects '{"name":"Fault Lab","retention_days":1,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0}' | jq -r .id)
channel() {
  local name=$1 format=$2 vendor=$3 endpoint=$4 static_header=${5:-}
  local headers='{}'
  [ -n "$static_header" ] && headers=$(jq -nc --arg value "$static_header" '{"X-Lab-Static":$value}')
  post /channels "$(jq -nc --arg name "$name" --arg format "$format" --arg vendor "$vendor" --arg endpoint "$endpoint" --argjson headers "$headers" \
    '{name:$name,format:$format,vendor:$vendor,endpoint:$endpoint,headers:$headers,models:[{name:"fault-model"}],currency:"USD",enabled:true,allow_private_network:true}')" | jq -r .id
}

text_channel=$(channel "Fault Text" openai_chat openai http://127.0.0.1:18080/chat/text)
fail_channel=$(channel "Fault Fail" openai_chat openai http://127.0.0.1:18080/chat/fail)
partial_channel=$(channel "Fault Partial" openai_chat openai http://127.0.0.1:18080/chat/partial)
tool_channel=$(channel "Fault Tool" openai_chat openai http://127.0.0.1:18080/chat/tool)
echo_channel=$(channel "Fault Echo" openai_chat openai http://127.0.0.1:18080/chat/echo yes)
messages_channel=$(channel "Fault Messages" anthropic_messages anthropic http://127.0.0.1:18080/messages/echo)

for id in "$text_channel" "$fail_channel" "$partial_channel" "$tool_channel" "$echo_channel" "$messages_channel"; do
  post "/channels/$id/keys" '{"label":"fault","secret":"fault-key"}' >/dev/null
done

route() {
  local name=$1 targets=$2
  post "/projects/$project/routes" "$(jq -nc --arg name "$name" --argjson targets "$targets" '{name:$name,targets:$targets}')" >/dev/null
}
target() {
  jq -nc --arg channel "$1" --arg model "fault-model" --argjson priority "${2:-0}" --argjson limit "${3:-0}" \
    '{channel_id:$channel,upstream_model:$model,priority:$priority,weight:1,max_output_tokens:$limit}'
}
route fault-failover "$(jq -nc --argjson first "$(target "$fail_channel" 0)" --argjson second "$(target "$text_channel" 1)" '[$first,$second]')"
route fault-midstream "$(jq -nc --argjson target "$(target "$partial_channel" 0)" '[$target]')"
route fault-tool "$(jq -nc --argjson target "$(target "$tool_channel" 0)" '[$target]')"
route fault-echo "$(jq -nc --argjson target "$(target "$echo_channel" 0)" '[$target]')"
route fault-default "$(jq -nc --argjson target "$(target "$messages_channel" 0 111)" '[$target]')"
fault_key=$(post "/projects/$project/keys" '{"label":"fault","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)

send_chat() {
  local model=$1 file=$2 body=$3
  curl -sS -o "$file" -w '%{http_code}' -N -m 60 "$base/v1/chat/completions" \
    -H "Authorization: Bearer $fault_key" -H 'Content-Type: application/json' -d "$body"
}

status=0
passed=0
failed=0
pass() { printf 'PASS %s\n' "$1"; passed=$((passed + 1)); }
fail() { printf 'FAIL %s\n' "$1"; failed=$((failed + 1)); status=1; }

http=$(send_chat fault-failover "$runtime/fault-failover.json" '{"model":"fault-failover","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}')
if [ "$http" = "200" ] && grep -q "FAULT_OK" "$runtime/fault-failover.json"; then pass "a failed attempt fails over before content"; else fail "failover: HTTP $http $(head -c 200 "$runtime/fault-failover.json")"; fi
attempts=$(get "/projects/$project/calls?model=fault-failover&limit=10" | jq -r '.items[0].attempts // 0')
[ "$attempts" -ge 2 ] && pass "the call records both attempts" || fail "attempt count is $attempts"

http=$(send_chat fault-midstream "$runtime/fault-midstream.json" '{"model":"fault-midstream","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}')
if [ "$http" = "200" ] && grep -q "PARTIAL" "$runtime/fault-midstream.json" && grep -qiE "error|failed|truncat" "$runtime/fault-midstream.json"; then
  pass "a truncated stream is reported instead of retried"
else
  fail "midstream: HTTP $http $(head -c 300 "$runtime/fault-midstream.json")"
fi
state=$(get "/projects/$project/calls?model=fault-midstream&limit=10" | jq -r '.items[0].state // ""')
attempts=$(get "/projects/$project/calls?model=fault-midstream&limit=10" | jq -r '.items[0].attempts // 0')
if [ "$state" = "failed" ] && [ "$attempts" = "1" ]; then
  pass "the truncated call is recorded as failed after one attempt"
else
  fail "midstream call state is $state after $attempts attempts"
fi

jq -nc '{document:{rules:[{id:"fault-tool",name:"Fault tool",enabled:true,priority:100,phase:"tool_call",when:"tool.name == '\''get_weather'\''",action:"block",severity:"high",reason:"fault test"}]},note:"fault"}' |
  curl -fsS -b "$jar" "${marker[@]}" -X POST "$api/projects/$project/policies" -d @- >/dev/null
http=$(send_chat fault-tool "$runtime/fault-tool.json" '{"model":"fault-tool","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"call get_weather"}]}')
if [ "$http" = "200" ] && ! grep -q "Paris" "$runtime/fault-tool.json" && grep -q "tool_call_blocked" "$runtime/fault-tool.json"; then
  pass "a held tool call is blocked before its arguments are released"
else
  fail "tool hold: HTTP $http $(head -c 300 "$runtime/fault-tool.json")"
fi
curl -fsS -b "$jar" "${marker[@]}" -X POST "$api/projects/$project/policies/1/rollback" >/dev/null || true

http=$(curl -sS -o "$runtime/fault-echo.json" -w '%{http_code}' -N -m 60 "$base/v1/chat/completions" \
  -H "Authorization: Bearer $fault_key" -H 'Content-Type: application/json' \
  -H 'OpenAI-Organization: lab-org' -H 'Cookie: secret-cookie' \
  -d '{"model":"fault-echo","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}')
if [ "$http" = "200" ] && grep -q "organization=lab-org" "$runtime/fault-echo.json" && grep -q "static=yes" "$runtime/fault-echo.json" && ! grep -q "secret-cookie" "$runtime/fault-echo.json"; then
  pass "allowlisted and channel headers are forwarded while credentials are not"
else
  fail "header forwarding: HTTP $http $(head -c 400 "$runtime/fault-echo.json")"
fi

http=$(send_chat fault-default "$runtime/fault-default.json" '{"model":"fault-default","stream":true,"messages":[{"role":"user","content":"hello"}]}')
if [ "$http" = "200" ] && grep -q "max_tokens=111" "$runtime/fault-default.json"; then
  pass "the route output limit reaches a Messages upstream"
else
  fail "default output limit: HTTP $http $(head -c 300 "$runtime/fault-default.json")"
fi

http=$(send_chat fault-failover "$runtime/fault-reject.json" '{"model":"fault-failover","stream":false,"messages":[{"role":"user","content":"hello"}],"n":2}')
if [ "$http" = "422" ] && grep -q "unsupported_multi_choice" "$runtime/fault-reject.json"; then
  pass "multiple choices are rejected before the upstream"
else
  fail "multi-choice rejection: HTTP $http $(head -c 300 "$runtime/fault-reject.json")"
fi
http=$(curl -sS -o "$runtime/fault-state.json" -w '%{http_code}' -m 60 "$base/v1/responses" \
  -H "Authorization: Bearer $fault_key" -H 'Content-Type: application/json' \
  -d '{"model":"fault-failover","store":true,"input":"hello"}')
if [ "$http" = "422" ] && grep -qiE "store|state|unsupported" "$runtime/fault-state.json"; then
  pass "provider state is rejected before the upstream"
else
  fail "state rejection: HTTP $http $(head -c 300 "$runtime/fault-state.json")"
fi

printf '\n%d passed, %d failed\n' "$passed" "$failed"
exit "$status"
