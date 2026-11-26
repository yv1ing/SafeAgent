#!/usr/bin/env bash
set -euo pipefail

# shellcheck source=../lib/lab.sh
. "$(dirname "$0")/../lib/lab.sh"
require_lab
# shellcheck source=../lib/console.sh
. "$LAB/lib/console.sh"

umask 077
mkdir -p "$LAB_RUNTIME/control"
CONSOLE_JAR="$LAB_RUNTIME/control/cookies.txt"
console_login
delete_json="$LAB_RUNTIME/control/delete.json"
stamp="$(date +%s)-$$"

project=""
channel_id=""
model_id=""
provider_id=""
route_id=""
dangling_channel=""
dangling_model=""
dangling_provider=""

cleanup() {
  if [ -n "$project" ]; then
    api_delete "/projects/$project"
  fi
  for entry in "$channel_id" "$dangling_channel"; do
    if [ -n "$entry" ]; then api_delete "/channels/$entry?acknowledge=true&detach_targets=true"; fi
  done
  for entry in "$model_id" "$dangling_model"; do
    if [ -n "$entry" ]; then api_delete "/models/$entry?acknowledge=true&detach_targets=true"; fi
  done
  for entry in "$provider_id" "$dangling_provider"; do
    if [ -n "$entry" ]; then api_delete "/providers/$entry"; fi
  done
  rm -f "$CONSOLE_JAR"
}
cleanup_register cleanup

endpoints=$(api_get /providers/openai | jq -c '.endpoints | {openai_chat: (.openai_chat | .BaseURL = "https://wizard.example.invalid")}')

project=$(api_post /projects "$(jq -nc --arg name "Control Plane $stamp" '{name:$name,retention_days:1}')" | jq -r .id)

connect() {
  local label=$1
  api_post /configuration/connect "$(jq -nc \
    --arg label "$label $stamp" \
    --arg channel "$label channel $stamp" \
    --arg model "$label model $stamp" \
    --arg secret "control-secret-$stamp" \
    --argjson endpoints "$endpoints" \
    '{provider:{label:$label,endpoints:$endpoints,enabled:true},
      channel:{name:$channel,provider_id:"",base_url:"",enabled:true,headers:{}},
      keys:[$secret],
      models:[{provider_id:"",upstream_name:$model,kind:"generation",currency:"USD",enabled:true}]}')"
}

connected=$(connect "Wizard")
provider_id=$(jq -r '.provider.id // empty' <<<"$connected")
channel_id=$(jq -r '.channel.id // empty' <<<"$connected")
model_id=$(jq -r '.models[0].id // empty' <<<"$connected")
if [ -n "$provider_id" ] && [ -n "$channel_id" ] && [ -n "$model_id" ] &&
  [ "$(jq -r '.channel.provider_id' <<<"$connected")" = "$provider_id" ] &&
  [ "$(jq -r '.models[0].provider_id' <<<"$connected")" = "$provider_id" ]; then
  pass "the wizard connect creates a provider, a channel and its catalog models in one call"
else
  fail "wizard connect: $connected"
fi
if [ "$(api_get "/providers/$provider_id" | jq -r .source)" = "custom" ] &&
  [ "$(api_get "/channels/$channel_id" | jq -r '.route_count')" = "0" ] &&
  [ "$(api_get "/models/$model_id" | jq -r '.formats | index("openai_chat") != null')" = "true" ]; then
  pass "the connect result is readable through the provider, channel and model endpoints"
else
  fail "the connect result is not consistent across the control-plane endpoints"
fi

route_name="control-route-$stamp"
manual_alias="control-alias-$stamp"
route=$(api_post "/projects/$project/routes" "$(jq -nc \
  --arg name "$route_name" --arg alias "$manual_alias" \
  --arg channel "$channel_id" --arg model_id "$model_id" \
  '{name:$name,aliases:[$alias],targets:[{channel_id:$channel,model_id:$model_id,priority:0,weight:1,max_output_tokens:64}]}')")
route_id=$(jq -r .id <<<"$route")
if [ -n "$route_id" ] && jq -e --arg alias "$manual_alias" '
  .aliases | length == 1 and .[0].alias == $alias and .[0].source == "manual"
' <<<"$route" >/dev/null; then
  pass "a route keeps its manual alias"
else
  fail "route alias: $route"
fi

renamed="control-renamed-$stamp"
route_update=$(api_put "/projects/$project/routes/$route_id" "$(jq -nc \
  --arg name "$renamed" --arg alias "$manual_alias" \
  --arg channel "$channel_id" --arg model_id "$model_id" --argjson version "$(jq -r .version <<<"$route")" \
  '{name:$name,aliases:[$alias],targets:[{channel_id:$channel,model_id:$model_id,priority:0,weight:1,max_output_tokens:64}],version:$version}')")
if jq -e --arg old "$route_name" --arg manual "$manual_alias" '
  .name != $old and
  ([.aliases[] | select(.alias == $old and .source == "renamed")] | length) == 1 and
  ([.aliases[] | select(.alias == $manual and .source == "manual")] | length) == 1
' <<<"$route_update" >/dev/null; then
  pass "renaming a route records the old name as a renamed alias"
else
  fail "alias on rename: $route_update"
fi

project_json=$(api_get "/projects/$project")
api_put "/projects/$project" "$(jq -c --arg route "$route_id" \
  '{name,retention_days,auxiliary_retention_days,default_route_id:$route,requests_per_minute,concurrency,daily_token_quota,default_max_output_tokens,stream_review_bytes,stream_review_timeout_ms,version}' \
  <<<"$project_json")" >/dev/null
if [ "$(api_get "/projects/$project" | jq -r .default_route_id)" = "$route_id" ]; then
  pass "the project default route is set"
else
  fail "the project default route was not stored"
fi

impact=$(api_get "/configuration/impact?channel_id=$channel_id")
if jq -e --arg project "$project" --arg route "$route_id" '
  ([.routes[] | select(.route_id == $route and .target_count == 1)] | length) == 1 and
  (.projects | index($project) != null)
' <<<"$impact" >/dev/null; then
  pass "channel impact names the referencing route and project"
else
  fail "channel impact: $impact"
fi
if [ "$(api_delete_status "/channels/$channel_id" "$delete_json")" = "409" ] &&
  [ "$(jq -r .code "$delete_json")" = "impact_requires_acknowledge" ]; then
  pass "soft deleting a referenced channel requires acknowledge"
else
  fail "channel delete without acknowledge: $(cat "$delete_json")"
fi

if [ "$(api_delete_status "/channels/$channel_id?acknowledge=true" "$delete_json")" = "204" ]; then
  degraded=$(api_get "/projects/$project/routes/$route_id")
  if jq -e --arg channel "$channel_id" '
    (.targets | map(select(.channel_id == $channel)) | first) as $target |
    $target.enabled == false and $target.disabled_reason == "channel deleted"
  ' <<<"$degraded" >/dev/null &&
    [ "$(api_get "/projects/$project/routes" | jq '[.[] | select(.id == "'"$route_id"'")] | length')" = "1" ]; then
    pass "a soft-deleted channel degrades the route target instead of removing the route"
  else
    fail "the route did not degrade after the channel delete: $degraded"
  fi
  restored=$(api_post "/channels/$channel_id/restore" '{}')
  restored_route=$(api_get "/projects/$project/routes/$route_id")
  if [ "$(jq -r .enabled <<<"$restored")" = "true" ] && jq -e --arg channel "$channel_id" '
    (.targets | map(select(.channel_id == $channel)) | first) as $target |
    $target.enabled == true and ($target.disabled_reason // "") == ""
  ' <<<"$restored_route" >/dev/null; then
    pass "restoring a channel re-enables the targets it disabled"
  else
    fail "channel restore: $restored_route"
  fi
else
  fail "channel soft delete with acknowledge: $(cat "$delete_json")"
fi

if [ "$(api_delete_status "/models/$model_id" "$delete_json")" = "409" ] &&
  [ "$(jq -r .code "$delete_json")" = "impact_requires_acknowledge" ]; then
  pass "soft deleting a referenced model requires acknowledge"
else
  fail "model delete without acknowledge: $(cat "$delete_json")"
fi
if [ "$(api_delete_status "/models/$model_id?acknowledge=true" "$delete_json")" = "204" ]; then
  degraded=$(api_get "/projects/$project/routes/$route_id")
  if jq -e --arg model "$model_id" '
    (.targets | map(select(.model_id == $model)) | first) as $target |
    $target.enabled == false and $target.disabled_reason == "model deleted"
  ' <<<"$degraded" >/dev/null; then
    pass "a soft-deleted model degrades the route target instead of removing the route"
  else
    fail "the route did not degrade after the model delete: $degraded"
  fi
  restored=$(api_post "/models/$model_id/restore" '{}')
  restored_route=$(api_get "/projects/$project/routes/$route_id")
  if [ "$(jq -r .enabled <<<"$restored")" = "true" ] && jq -e --arg model "$model_id" '
    (.targets | map(select(.model_id == $model)) | first) as $target |
    $target.enabled == true and ($target.disabled_reason // "") == ""
  ' <<<"$restored_route" >/dev/null; then
    pass "restoring a model re-enables the targets it disabled"
  else
    fail "model restore: $restored_route"
  fi
else
  fail "model soft delete with acknowledge: $(cat "$delete_json")"
fi

model_json=$(api_get "/models/$model_id")
api_put "/models/$model_id" "$(jq -c '{provider_id,upstream_name,display_name,kind,input_price,output_price,cache_read_price,cache_write_price,currency,enabled:false,version}' <<<"$model_json")" >/dev/null
if jq -e --arg route "$route_id" '
  [.warnings[] | select(.code == "route_target_degraded" and .subject == $route)] | length == 1
' <<<"$(api_get /configuration/health)" >/dev/null; then
  pass "a disabled model produces a configuration-health warning"
else
  fail "the disabled model did not warn: $(api_get /configuration/health)"
fi
model_json=$(api_get "/models/$model_id")
api_put "/models/$model_id" "$(jq -c '{provider_id,upstream_name,display_name,kind,input_price,output_price,cache_read_price,cache_write_price,currency,enabled:true,version}' <<<"$model_json")" >/dev/null
if ! jq -e --arg route "$route_id" '
  [.warnings[] | select(.code == "route_target_degraded" and .subject == $route)] | length == 0
' <<<"$(api_get /configuration/health)" >/dev/null; then
  fail "the warning survived re-enabling the model"
else
  pass "re-enabling the model clears the warning"
fi

channel_json=$(api_get "/channels/$channel_id")
api_put "/channels/$channel_id" "$(jq -c '{name,provider_id,base_url,headers,forward_headers,default_max_output_tokens,timeout_seconds,first_byte_timeout_seconds,allow_private_network,enabled:false,version}' <<<"$channel_json")" >/dev/null
if jq -e --arg route "$route_id" '
  [.warnings[] | select(.code == "route_target_degraded" and .subject == $route)] | length == 1
' <<<"$(api_get /configuration/health)" >/dev/null; then
  pass "a disabled channel produces a configuration-health warning"
else
  fail "the disabled channel did not warn: $(api_get /configuration/health)"
fi
if [ "$(api_get "/projects/$project/routes" | jq '[.[] | select(.id == "'"$route_id"'")] | length')" = "1" ]; then
  pass "a disabled channel does not remove the route"
else
  fail "the route disappeared after the channel was disabled"
fi
channel_json=$(api_get "/channels/$channel_id")
api_put "/channels/$channel_id" "$(jq -c '{name,provider_id,base_url,headers,forward_headers,default_max_output_tokens,timeout_seconds,first_byte_timeout_seconds,allow_private_network,enabled:true,version}' <<<"$channel_json")" >/dev/null
if ! jq -e --arg route "$route_id" '
  [.warnings[] | select(.code == "route_target_degraded" and .subject == $route)] | length == 0
' <<<"$(api_get /configuration/health)" >/dev/null; then
  fail "the warning survived re-enabling the channel"
else
  pass "re-enabling the channel clears the warning"
fi

dangling=$(connect "Dangling")
dangling_provider=$(jq -r '.provider.id // empty' <<<"$dangling")
dangling_channel=$(jq -r '.channel.id // empty' <<<"$dangling")
dangling_model=$(jq -r '.models[0].id // empty' <<<"$dangling")
if [ "$(api_delete_status "/providers/$dangling_provider" "$delete_json")" = "204" ]; then
  dangling_provider=""
else
  fail "deleting a custom provider: $(cat "$delete_json")"
fi
channel_json=$(api_get "/channels/$channel_id")
api_put "/channels/$channel_id" "$(jq -c '{name,provider_id,base_url,headers,forward_headers,default_max_output_tokens,timeout_seconds,first_byte_timeout_seconds,allow_private_network,enabled:false,version}' <<<"$channel_json")" >/dev/null
health=$(api_get /configuration/health)
if jq -e --arg channel "$dangling_channel" --arg model "$dangling_model" --arg route "$route_id" '
  ([.warnings[] | select(.code == "channel_provider_missing" and .subject == $channel)] | length) == 1 and
  ([.warnings[] | select(.code == "model_provider_missing" and .subject == $model)] | length) == 1 and
  ([.warnings[] | select(.code == "route_target_degraded" and .subject == $route)] | length) == 1
' <<<"$health" >/dev/null; then
  pass "a half-built graph only produces configuration-health warnings"
else
  fail "half-built graph health: $health"
fi

if SKIP_WEB=1 "$LAB/scripts/restart-gateway.sh" >/dev/null 2>&1; then
  console_login
  if curl -fsS "$CONSOLE_BASE/health" >/dev/null; then
    pass "the gateway starts with a degraded configuration"
  else
    fail "the gateway health endpoint is unavailable after the degraded restart"
  fi
  restarted_health=$(api_get /configuration/health)
  if jq -e --arg channel "$dangling_channel" '
    [.warnings[] | select(.code == "channel_provider_missing" and .subject == $channel)] | length == 1
  ' <<<"$restarted_health" >/dev/null &&
    grep -q "configuration is degraded" "$LAB_RUNTIME/gateway.log"; then
    pass "startup logs the warning instead of refusing the configuration"
  else
    fail "the restart did not report the degraded configuration"
  fi
else
  fail "the gateway did not restart with a degraded configuration"
fi

api_delete "/channels/$dangling_channel?acknowledge=true&detach_targets=true"
api_delete "/models/$dangling_model?acknowledge=true&detach_targets=true"
dangling_channel=""
dangling_model=""
channel_json=$(api_get "/channels/$channel_id")
api_put "/channels/$channel_id" "$(jq -c '{name,provider_id,base_url,headers,forward_headers,default_max_output_tokens,timeout_seconds,first_byte_timeout_seconds,allow_private_network,enabled:true,version}' <<<"$channel_json")" >/dev/null
if jq -e --arg route "$route_id" '[.warnings[] | select(.subject == $route)] | length == 0' \
  <<<"$(api_get /configuration/health)" >/dev/null; then
  pass "repairing the graph clears the warnings"
else
  fail "the repaired graph still warns: $(api_get /configuration/health)"
fi

# Reset a seeded provider the lab does not use, so the test never repoints a live channel host.
used_providers=$(api_get /channels | jq -r '.[].provider_id' | sort -u)
reset_provider=""
for candidate in $(api_get /providers | jq -r '.[] | select(.source == "builtin") | .id'); do
  if ! grep -qx "$candidate" <<<"$used_providers"; then
    reset_provider=$candidate
    break
  fi
done
if [ -z "$reset_provider" ]; then
  fail "no unused built-in provider is available for the reset check"
else
  seeded=$(api_get "/providers/$reset_provider")
  original_label=$(jq -r .label <<<"$seeded")
  api_put "/providers/$reset_provider" "$(jq -c --arg label "Reset Probe $stamp" '{label:$label,endpoints,enabled,version}' <<<"$seeded")" >/dev/null
  reset=$(api_post "/providers/$reset_provider/reset" '{}')
  if [ "$(jq -r .label <<<"$reset")" = "$original_label" ] &&
    [ "$(jq -r .source <<<"$reset")" = "builtin" ]; then
    pass "resetting a built-in provider restores the seeded definition"
  else
    fail "provider reset: $reset"
  fi
fi

summary
