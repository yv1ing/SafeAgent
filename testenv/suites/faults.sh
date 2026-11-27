#!/usr/bin/env bash
set -euo pipefail
umask 077

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
require_lab
# shellcheck source=../lib/console.sh
. "$LAB/lib/console.sh"
# shellcheck source=../lib/protocol.sh
. "$LAB/lib/protocol.sh"

mkdir -p "$LAB_RUNTIME/faults"
CONSOLE_JAR="$LAB_RUNTIME/faults/cookies.txt"
console_login

run="$(date +%s)-$$"

cd "$LAB_ROOT"
go build -trimpath -o "$LAB_RUNTIME/faults/fault-server" ./testenv/faults
"$LAB_RUNTIME/faults/fault-server" -listen 127.0.0.1:18080 > "$LAB_RUNTIME/faults/fault-server.log" 2>&1 &
fault_pid=$!
echo "$fault_pid" > "$LAB_RUNTIME/faults/fault-server.pid"
providers_created=()
cleanup() {
  kill "$fault_pid" 2>/dev/null || true
  rm -f "$LAB_RUNTIME/faults/fault-server.pid"
  if [ -n "${project:-}" ]; then
    api_delete "/projects/$project"
  fi
  if [ -n "${review_project:-}" ]; then
    api_delete "/projects/$review_project"
  fi
  local channels models id
  channels=$(api_get /channels 2>/dev/null || true)
  for id in $(jq -r --arg suffix " $run" '.[] | select(.name | endswith($suffix)) | .id' <<<"${channels:-[]}"); do
    api_delete "/channels/$id?acknowledge=true&detach_targets=true"
  done
  models=$(api_get /models 2>/dev/null || true)
  for id in $(jq -r --arg suffix "-$run" '.[] | select(.upstream_name | endswith($suffix)) | .id' <<<"${models:-[]}"); do
    api_delete "/models/$id?acknowledge=true&detach_targets=true"
  done
  for id in "${providers_created[@]}"; do
    api_delete "/providers/$id"
  done
  rm -f "$CONSOLE_JAR"
}
cleanup_register cleanup
for _ in $(seq 1 50); do
  curl -fsS http://127.0.0.1:18080/chat/text >/dev/null 2>&1 && break
  sleep 0.1
done
if ! kill -0 "$fault_pid" 2>/dev/null; then
  echo "the fault upstream failed to start on 127.0.0.1:18080" >&2
  cat "$LAB_RUNTIME/faults/fault-server.log" >&2
  exit 1
fi

project=$(api_post /projects '{"name":"Fault Lab","retention_days":1,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0}' | jq -r .id)

# Each fault path is a custom provider cloned from the seeded endpoint definition, so the channel
# base URL and the endpoint path point at the local fault upstream.
chat_endpoints=$(api_get /providers/openai | jq -c .endpoints)
messages_endpoints=$(api_get /providers/anthropic | jq -c .endpoints)
channel() {
  local label=$1 format=$2 endpoints=$3 path=$4 static_header=${5:-}
  local headers='{}' slug model model_id channel_id provider_id
  [ -n "$static_header" ] && headers=$(jq -nc --arg value "$static_header" '{"X-Lab-Static":$value}')
  slug=$(printf '%s' "$label" | tr '[:upper:] ' '[:lower:]-')
  model="fault-model-$slug-$run"
  provider_id=$(api_post /providers "$(jq -nc --arg label "$label $run" --arg format "$format" \
    --arg base "http://127.0.0.1:18080" --arg path "$path" --argjson endpoints "$endpoints" \
    '{label:$label,endpoints:{($format): ($endpoints[$format] | .BaseURL=$base | .Path=$path)},enabled:true}')" | jq -r .id)
  providers_created+=("$provider_id")
  model_id=$(api_post /models "$(jq -nc --arg provider "$provider_id" --arg model "$model" \
    '{provider_id:$provider,upstream_name:$model,kind:"generation",currency:"USD",enabled:true}')" | jq -r .id)
  channel_id=$(api_post /channels "$(jq -nc --arg name "$label $run" --arg provider "$provider_id" --argjson headers "$headers" \
    '{name:$name,provider_id:$provider,base_url:"http://127.0.0.1:18080",headers:$headers,enabled:true,allow_private_network:true}')" | jq -r .id)
  printf '%s %s\n' "$channel_id" "$model_id"
}

read -r text_channel text_model <<<"$(channel "Fault Text" openai_chat "$chat_endpoints" /chat/text)"
read -r fail_channel fail_model <<<"$(channel "Fault Fail" openai_chat "$chat_endpoints" /chat/fail)"
read -r partial_channel partial_model <<<"$(channel "Fault Partial" openai_chat "$chat_endpoints" /chat/partial)"
read -r tool_channel tool_model <<<"$(channel "Fault Tool" openai_chat "$chat_endpoints" /chat/tool)"
read -r sibling_channel sibling_model <<<"$(channel "Fault Sibling" openai_chat "$chat_endpoints" /chat/tool-sibling)"
read -r review_channel review_model <<<"$(channel "Fault Review Source" openai_chat "$chat_endpoints" /chat/review)"
read -r multi_channel multi_model <<<"$(channel "Fault Multi" openai_chat "$chat_endpoints" /chat/multi)"
read -r echo_channel echo_model <<<"$(channel "Fault Echo" openai_chat "$chat_endpoints" /chat/echo yes)"
read -r messages_channel messages_model <<<"$(channel "Fault Messages" anthropic_messages "$messages_endpoints" /messages/echo)"
embedding_model="fault-embedding-$run"
embedding_provider=$(api_post /providers "$(jq -nc --arg label "Fault Embedding $run" \
  --arg base "http://127.0.0.1:18080" --argjson endpoints "$chat_endpoints" \
  '{label:$label,endpoints:{openai_chat:($endpoints.openai_chat | .BaseURL=$base)},enabled:true}')" | jq -r .id)
providers_created+=("$embedding_provider")
embedding_model_id=$(api_post /models "$(jq -nc --arg provider "$embedding_provider" --arg model "$embedding_model" \
  '{provider_id:$provider,upstream_name:$model,kind:"embedding",currency:"USD",enabled:true}')" | jq -r .id)
embedding_channel=$(api_post /channels "$(jq -nc --arg name "Fault Embedding $run" --arg provider "$embedding_provider" \
  '{name:$name,provider_id:$provider,base_url:"http://127.0.0.1:18080",enabled:true,allow_private_network:true}')" | jq -r .id)

for id in "$text_channel" "$fail_channel" "$partial_channel" "$tool_channel" "$sibling_channel" "$review_channel" "$multi_channel" "$echo_channel" "$messages_channel" "$embedding_channel"; do
  api_post "/channels/$id/keys" '{"label":"fault","secret":"fault-key"}' >/dev/null
done

route() {
  local name=$1 targets=$2
  api_post "/projects/$project/routes" "$(jq -nc --arg name "$name" --argjson targets "$targets" '{name:$name,targets:$targets}')" >/dev/null
}
target() {
  jq -nc --arg channel "$1" --arg model_id "$2" --argjson priority "${3:-0}" --argjson limit "${4:-0}" \
    '{channel_id:$channel,model_id:$model_id,priority:$priority,weight:1,max_output_tokens:$limit}'
}
route fault-failover "$(jq -nc --argjson first "$(target "$fail_channel" "$fail_model" 0)" --argjson second "$(target "$text_channel" "$text_model" 1)" '[$first,$second]')"
route fault-midstream "$(jq -nc --argjson target "$(target "$partial_channel" "$partial_model" 0)" '[$target]')"
route fault-tool "$(jq -nc --argjson target "$(target "$tool_channel" "$tool_model" 0)" '[$target]')"
route fault-sibling "$(jq -nc --argjson target "$(target "$sibling_channel" "$sibling_model" 0)" '[$target]')"
route fault-embed "$(jq -nc --argjson target "$(target "$embedding_channel" "$embedding_model_id" 0)" '[$target]')"
route fault-multi "$(jq -nc --argjson target "$(target "$multi_channel" "$multi_model" 0)" '[$target]')"
route fault-echo "$(jq -nc --argjson target "$(target "$echo_channel" "$echo_model" 0)" '[$target]')"
route fault-default "$(jq -nc --argjson target "$(target "$messages_channel" "$messages_model" 0 111)" '[$target]')"
fault_key=$(api_post "/projects/$project/keys" '{"label":"fault","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)

review_project=$(api_post /projects '{"name":"Fault Review","retention_days":1,"requests_per_minute":0,"concurrency":0,"daily_token_quota":0,"stream_review_bytes":10,"stream_review_timeout_ms":1000}' | jq -r .id)
api_post "/projects/$review_project/routes" \
  "$(jq -nc --arg channel "$review_channel" --arg model_id "$review_model" '{name:"fault-review",targets:[{channel_id:$channel,model_id:$model_id,priority:0,weight:1}]}')" >/dev/null
review_key=$(api_post "/projects/$review_project/keys" '{"label":"review","requests_per_minute":0,"concurrency":0,"trust_level":1,"role":"agent","enabled":true}' | jq -r .secret)
api_post "/projects/$review_project/policies" "$(jq -nc --arg when "response.text.contains('REVIEW_BAD')" \
  '{document:{rules:[{id:"fault-review",name:"Fault review",enabled:true,priority:100,phase:"response",when:$when,action:"block",severity:"high",reason:"fault review"}]},note:"fault"}')" >/dev/null

http=$(gateway_send chat fault-failover "$fault_key" "$LAB_RUNTIME/faults/fault-failover.json" '{"model":"fault-failover","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}' true 60)
if [ "$http" = "200" ] && grep -q "FAULT_OK" "$LAB_RUNTIME/faults/fault-failover.json"; then pass "a failed attempt fails over before content"; else fail "failover: HTTP $http $(head -c 200 "$LAB_RUNTIME/faults/fault-failover.json")"; fi
attempts=$(api_get "/projects/$project/calls?model=fault-failover&limit=10" | jq -r '.items[0].attempts // 0')
[ "$attempts" -ge 2 ] && pass "the call records both attempts" || fail "attempt count is $attempts"

http=$(gateway_send chat fault-midstream "$fault_key" "$LAB_RUNTIME/faults/fault-midstream.json" '{"model":"fault-midstream","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}' true 60)
if [ "$http" = "200" ] && grep -q "PARTIAL" "$LAB_RUNTIME/faults/fault-midstream.json" && grep -qiE "error|failed|truncat" "$LAB_RUNTIME/faults/fault-midstream.json"; then
  pass "a truncated stream is reported instead of retried"
else
  fail "midstream: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-midstream.json")"
fi
state=$(api_get "/projects/$project/calls?model=fault-midstream&limit=10" | jq -r '.items[0].state // ""')
attempts=$(api_get "/projects/$project/calls?model=fault-midstream&limit=10" | jq -r '.items[0].attempts // 0')
if [ "$state" = "failed" ] && [ "$attempts" = "1" ]; then
  pass "the truncated call is recorded as failed after one attempt"
else
  fail "midstream call state is $state after $attempts attempts"
fi

api_post "/projects/$project/policies" "$(jq -nc '{document:{rules:[{id:"fault-tool",name:"Fault tool",enabled:true,priority:100,phase:"tool_call",when:"tool.name == '\''get_weather'\''",action:"block",severity:"high",reason:"fault test"}]},note:"fault"}')" >/dev/null
http=$(gateway_send chat fault-tool "$fault_key" "$LAB_RUNTIME/faults/fault-tool.json" '{"model":"fault-tool","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"call get_weather"}]}' true 60)
if [ "$http" = "403" ] && ! grep -q "Paris" "$LAB_RUNTIME/faults/fault-tool.json" && grep -q "tool_call_blocked" "$LAB_RUNTIME/faults/fault-tool.json"; then
  pass "a held tool call is blocked before its arguments are released"
else
  fail "tool hold: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-tool.json")"
fi
api_post "/projects/$project/policies/1/rollback" '{}' >/dev/null 2>&1 || true

http=$(curl -sS -o "$LAB_RUNTIME/faults/fault-embeddings.json" -w '%{http_code}' -m 60 "$GATEWAY_BASE/v1/embeddings" \
  -H "Authorization: Bearer $fault_key" -H 'Content-Type: application/json' \
  -d '{"model":"fault-embed","input":"hello"}')
if [ "$http" = "200" ] && jq -e --arg model "$embedding_model" '.model == $model and (.data[0].embedding | length) == 2' "$LAB_RUNTIME/faults/fault-embeddings.json" >/dev/null; then
  pass "embeddings passthrough applies the upstream model name"
else
  fail "embeddings passthrough: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-embeddings.json")"
fi
embedding_call=$(api_get "/projects/$project/calls?model=fault-embed&limit=10&include_auxiliary=true" | jq -r '.items[0] // empty')
default_embedding=$(api_get "/projects/$project/calls?model=fault-embed&limit=10" | jq '.items | length')
if [ "$default_embedding" = "0" ] && [ -n "$embedding_call" ] &&
  [ "$(jq -r '.lane' <<<"$embedding_call")" = "auxiliary" ] &&
  [ "$(jq -r '.operation' <<<"$embedding_call")" = "embeddings" ] &&
  [ "$(jq -r '.outcome' <<<"$embedding_call")" = "ok" ] &&
  [ "$(jq -r '.channel_id' <<<"$embedding_call")" = "$embedding_channel" ] &&
  [ "$(jq -r '.model_id' <<<"$embedding_call")" = "$embedding_model_id" ] &&
  [ "$(jq -r '.upstream_model' <<<"$embedding_call")" = "$embedding_model" ]; then
  pass "the auxiliary call is hidden by default and records its channel model"
else
  fail "auxiliary audit: default=$default_embedding $embedding_call"
fi

api_post "/projects/$project/policies" "$(jq -nc '{document:{rules:[{id:"fault-sibling",name:"Fault sibling",enabled:true,priority:100,phase:"tool_call",when:"tool.name == '\''one'\''",action:"block",severity:"high",reason:"fault sibling"}]},note:"fault"}')" >/dev/null
http=$(gateway_send chat fault-sibling "$fault_key" "$LAB_RUNTIME/faults/fault-sibling.json" '{"model":"fault-sibling","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"call both tools"}]}' true 60)
if [ "$http" = "200" ] && grep -q "delivered" "$LAB_RUNTIME/faults/fault-sibling.json" && grep -q "tool_call_blocked" "$LAB_RUNTIME/faults/fault-sibling.json" && ! grep -q "call_2" "$LAB_RUNTIME/faults/fault-sibling.json"; then
  pass "a held sibling is withheld from the client after a block"
else
  fail "sibling hold: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-sibling.json")"
fi
sibling_call=$(api_get "/projects/$project/calls?model=fault-sibling&limit=10" | jq -r '.items[0].id // empty')
sibling_detail=""
if [ -n "$sibling_call" ]; then
  sibling_detail=$(api_get "/projects/$project/calls/$sibling_call")
fi
if [ -n "$sibling_call" ] &&
  jq -e '[.events[] | select(.kind == "action.proposed") | .payload.tool_call_id | select(. == "call_2")] | length == 0' <<<"$sibling_detail" >/dev/null &&
  jq -e '[.events[] | select(.kind == "action.executed") | select(.payload.response_segment != "")] | length > 0' <<<"$sibling_detail" >/dev/null; then
  pass "the audit response excludes the held sibling"
else
  fail "sibling audit: $sibling_detail"
fi
api_post "/projects/$project/policies/1/rollback" '{}' >/dev/null 2>&1 || true

http=$(gateway_send chat fault-multi "$fault_key" "$LAB_RUNTIME/faults/fault-multi.json" '{"model":"fault-multi","stream":true,"n":2,"messages":[{"role":"user","content":"hello"}]}' true 60)
if [ "$http" = "200" ] && grep -q "MULTI_0" "$LAB_RUNTIME/faults/fault-multi.json" && grep -q "MULTI_1" "$LAB_RUNTIME/faults/fault-multi.json" && grep -q "DONE" "$LAB_RUNTIME/faults/fault-multi.json"; then
  pass "a same-format streaming multi-choice response passes through"
else
  fail "multi-choice pass-through: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-multi.json")"
fi

http=$(curl -sS -o "$LAB_RUNTIME/faults/fault-echo.json" -w '%{http_code}' -N -m 60 "$GATEWAY_BASE/v1/chat/completions" \
  -H "Authorization: Bearer $fault_key" -H 'Content-Type: application/json' \
  -H 'OpenAI-Organization: lab-org' -H 'Cookie: secret-cookie' \
  -d '{"model":"fault-echo","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}')
if [ "$http" = "200" ] && grep -q "organization=lab-org" "$LAB_RUNTIME/faults/fault-echo.json" && grep -q "static=yes" "$LAB_RUNTIME/faults/fault-echo.json" && ! grep -q "secret-cookie" "$LAB_RUNTIME/faults/fault-echo.json"; then
  pass "allowlisted and channel headers are forwarded while credentials are not"
else
  fail "header forwarding: HTTP $http $(head -c 400 "$LAB_RUNTIME/faults/fault-echo.json")"
fi

http=$(gateway_send chat fault-default "$fault_key" "$LAB_RUNTIME/faults/fault-default.json" '{"model":"fault-default","stream":true,"messages":[{"role":"user","content":"hello"}]}' true 60)
if [ "$http" = "200" ] && grep -q "max_tokens=111" "$LAB_RUNTIME/faults/fault-default.json"; then
  pass "the route output limit reaches a Messages upstream"
else
  fail "default output limit: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-default.json")"
fi

http=$(gateway_send chat fault-failover "$fault_key" "$LAB_RUNTIME/faults/fault-reject.json" '{"model":"fault-failover","stream":false,"messages":[{"role":"user","content":"hello"}],"n":2}' true 60)
if [ "$http" = "422" ] && grep -q "unsupported_multi_choice" "$LAB_RUNTIME/faults/fault-reject.json"; then
  pass "multiple choices are rejected before the upstream"
else
  fail "multi-choice rejection: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-reject.json")"
fi
http=$(curl -sS -o "$LAB_RUNTIME/faults/fault-state.json" -w '%{http_code}' -m 60 "$GATEWAY_BASE/v1/responses" \
  -H "Authorization: Bearer $fault_key" -H 'Content-Type: application/json' \
  -d '{"model":"fault-failover","background":true,"input":"hello"}')
if [ "$http" = "422" ] && grep -q "background_not_supported" "$LAB_RUNTIME/faults/fault-state.json"; then
  pass "background responses are rejected before the upstream"
else
  fail "background rejection: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-state.json")"
fi

http=$(curl -sS -o "$LAB_RUNTIME/faults/fault-review.json" -w '%{http_code}' -N -m 60 "$GATEWAY_BASE/v1/chat/completions" \
  -H "Authorization: Bearer $review_key" -H 'Content-Type: application/json' \
  -d '{"model":"fault-review","stream":true,"max_tokens":16,"messages":[{"role":"user","content":"hello"}]}')
if [ "$http" = "403" ] && ! grep -q "REVIEW_BAD" "$LAB_RUNTIME/faults/fault-review.json" && grep -q "response_blocked" "$LAB_RUNTIME/faults/fault-review.json"; then
  pass "stream review blocks before the first byte"
else
  fail "stream review: HTTP $http $(head -c 300 "$LAB_RUNTIME/faults/fault-review.json")"
fi
review_detail=$(api_get "/projects/$review_project/calls?model=fault-review&limit=10")
review_call=$(jq -r '.items[0].id // empty' <<<"$review_detail")
if [ -n "$review_call" ] &&
  [ "$(jq -r '.items[0].state' <<<"$review_detail")" = "completed" ] &&
  [ "$(jq -r '.items[0].outcome' <<<"$review_detail")" = "blocked" ] &&
  [ "$(jq -r '.items[0].error_code' <<<"$review_detail")" = "response_blocked" ] &&
  jq -e '[.events[] | select(.kind == "action.executed") | .payload.blocked] | any' <<<"$(api_get "/projects/$review_project/calls/$review_call")" >/dev/null; then
  pass "a stream review block records a completed call with a blocked outcome"
else
  fail "stream review audit: $review_detail"
fi

summary
