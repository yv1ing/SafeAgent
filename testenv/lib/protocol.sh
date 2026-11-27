#!/usr/bin/env bash
# Gateway protocol helpers shared by the acceptance suites. Source it after lab_env and console.sh.

GATEWAY_BASE=${SAFEAGENT_BASE_URL:-http://127.0.0.1:8080}

gateway_body() {
  local client=$1 model=$2 prompt=$3 stream=${4:-false} max_tokens=${5:-40}
  case "$client" in
    chat|messages)
      jq -nc --arg model "$model" --arg prompt "$prompt" \
        --argjson stream "$stream" --argjson max_tokens "$max_tokens" \
        '{model:$model,stream:$stream,max_tokens:$max_tokens,messages:[{role:"user",content:$prompt}]}'
      ;;
    responses)
      jq -nc --arg model "$model" --arg prompt "$prompt" \
        --argjson stream "$stream" --argjson max_tokens "$max_tokens" \
        '{model:$model,stream:$stream,max_output_tokens:$max_tokens,input:$prompt}'
      ;;
    gemini)
      jq -nc --arg prompt "$prompt" --argjson max_tokens "$max_tokens" \
        '{contents:[{role:"user",parts:[{text:$prompt}]}],generationConfig:{maxOutputTokens:$max_tokens}}'
      ;;
  esac
}

gateway_send() {
  local client=$1 route=$2 key=$3 file=$4 body=$5 stream=${6:-false} timeout=${7:-120}
  shift 7
  local args=(-sS -o "$file" -w '%{http_code}' -m "$timeout")
  if [ "$stream" = "true" ]; then
    args+=(-N)
  fi
  case "$client" in
    chat)
      args+=("$GATEWAY_BASE/v1/chat/completions" -H "Authorization: Bearer $key")
      ;;
    responses)
      args+=("$GATEWAY_BASE/v1/responses" -H "Authorization: Bearer $key")
      ;;
    messages)
      args+=("$GATEWAY_BASE/v1/messages" -H "x-api-key: $key" -H 'anthropic-version: 2023-06-01')
      ;;
    gemini)
      if [ "$stream" = "true" ]; then
        args+=("$GATEWAY_BASE/v1beta/models/$route:streamGenerateContent?alt=sse")
      else
        args+=("$GATEWAY_BASE/v1beta/models/$route:generateContent")
      fi
      args+=(-H "x-goog-api-key: $key")
      ;;
  esac
  args+=(-H 'Content-Type: application/json' -d "$body")
  curl "${args[@]}" "$@"
}

response_text() {
  local client=$1 file=$2
  case "$client" in
    chat)
      jq -r 'if .error then (.error.message // "") else (.choices[0].message.content // "") end' "$file"
      ;;
    responses)
      jq -r 'if .error then (.error.message // "") else ([.output[]?.content[]? | select(.type == "output_text") | .text] | join("")) end' "$file"
      ;;
    messages)
      jq -r 'if .type == "error" then (.error.message // "") else ([.content[]? | select(.type == "text") | .text] | join("")) end' "$file"
      ;;
    gemini)
      jq -r 'if .error then (.error.message // "") else ([.candidates[0].content.parts[]? | select(.thought != true) | .text // ""] | join("")) end' "$file"
      ;;
  esac
}

call_ids() {
  api_get "/projects/$SAFEAGENT_PROJECT/calls?limit=100" | jq -c '[.items[].id]'
}

call_detail() {
  api_get "/projects/$SAFEAGENT_PROJECT/calls/$1"
}

new_call() {
  api_get "/projects/$SAFEAGENT_PROJECT/calls?limit=100" |
    jq -r --argjson before "$1" --arg lane "$2" \
      '[.items[] | select(.lane == $lane) | select((.id as $id | $before | index($id)) == null)] | first | .id // empty'
}

wait_for_call() {
  local before=$1 lane=$2 id=""
  for _ in $(seq 1 "${3:-30}"); do
    id=$(new_call "$before" "$lane")
    if [ -n "$id" ]; then
      printf '%s' "$id"
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_call_settled() {
  local id=$1 state=""
  for _ in $(seq 1 "${2:-60}"); do
    state=$(call_detail "$id" | jq -r '.call.state')
    case "$state" in
      completed|failed|cancelled|interrupted) return 0 ;;
    esac
    sleep 1
  done
  return 1
}

trajectory_id_of() {
  call_detail "$1" | jq -r '.call.trajectory_id // empty'
}

trajectory_detail() {
  api_get "/projects/$SAFEAGENT_PROJECT/trajectories/$1"
}

wait_trajectory_settled() {
  local id=$1 state=""
  for _ in $(seq 1 "${2:-60}"); do
    state=$(trajectory_detail "$id" | jq -r '.summary.state')
    case "$state" in
      ended|interrupted) printf '%s' "$state"; return 0 ;;
    esac
    sleep 1
  done
  printf '%s' "$state"
  return 1
}
