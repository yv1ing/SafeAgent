package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/stream"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (state *call) completeStream(session *stream.Session, upstream *upstreamCall) (attemptResult, *model.Error) {
	journal := session.Journal()
	response := journal.Response(state.kind)
	state.decorate(response)
	if response.Failure != nil {
		if !session.Committed() {
			session.Close()
			return attemptFailed, transient("upstream_reported_failure", "The upstream reported a failed response.")
		}
		session.StageFailure(response.Failure)
		state.finishStream(session, upstream, "failed", response, response.Failure)
		return attemptEnded, response.Failure
	}
	if len(response.Items) == 0 && response.Finish.Kind != model.FinishContentFilter && !session.Committed() {
		session.Close()
		return attemptFailed, transient("empty_response", "The upstream returned no content.")
	}
	if err := session.StageComplete(); err != nil {
		session.Close()
		return attemptEnded, internal("encode_failed", "The response could not be encoded.")
	}
	ledger := state.ledger(journal)
	state.recordResponse(response, ledger, responseMeta{Committed: true, Identify: true, Stored: state.responseStore()})
	if !state.settle("completed", nil, response.Usage, upstream.endpoint.model) {
		failure := internal("audit_unavailable", "The response could not be recorded.")
		session.Close()
		return attemptEnded, failure
	}
	session.WriteStaged()
	return attemptCompleted, nil
}

func (state *call) failStream(session *stream.Session, upstream *upstreamCall, stateName string, failure *model.Error) {
	response := session.Journal().Response(state.kind)
	state.decorate(response)
	response.Failure = failure
	response.Status = "failed"
	session.StageFailure(failure)
	state.finishStream(session, upstream, stateName, response, failure)
}

func (state *call) finishStream(session *stream.Session, upstream *upstreamCall, stateName string, response *model.Response, failure *model.Error) {
	state.streamCommitted = session.Committed()
	state.streamTruncated = failure != nil && failure.Code == "upstream_stream_truncated"
	ledger := state.ledger(session.Journal())
	state.recordResponse(response, ledger, responseMeta{
		Committed: state.streamCommitted,
		Partial:   state.streamCommitted,
		Truncated: state.streamTruncated,
	})
	if !state.settle(stateName, failure, response.Usage, upstream.endpoint.model) {
		state.pipeline.logger.Error("failed stream could not be sealed", "call", state.recorder.Call().ID)
	}
	session.WriteStaged()
}

func (state *call) collectResponse(ctx context.Context, upstream *upstreamCall, candidate routing.Candidate) (attemptResult, *model.Error) {
	var response *model.Response
	if upstream.response != nil {
		response = upstream.response
		state.failureLedger.Losses = append(state.failureLedger.Losses, upstream.losses...)
		if failure := state.gateResponse(response); failure != nil {
			return attemptFailed, failure
		}
	} else {
		collector := stream.New(stream.Options{
			Encoder: stream.NopEncoder{}, Writer: stream.NewDiscardWriter(), Journal: model.NewJournal(),
			Restorers: scopeRestorers{scope: state.scope}, Gate: state.toolGate, ToolKinds: state.toolKinds, Format: state.kind,
			IncludeUsage: state.request.StreamOptions != nil && state.request.StreamOptions.IncludeUsage,
			ResponseID:   model.ResponseID(state.kind, state.recorder.Call().ID, ""), ResponseModel: state.request.Model,
			HoldLimit: state.pipeline.settings.ToolHoldBytes,
		})
		if failure := state.consume(ctx, upstream, collector); failure != nil {
			collector.Close()
			return attemptFailed, failure
		}
		collector.Close()
		response = collector.Journal().Response(state.kind)
	}
	state.decorate(response)
	if len(response.Items) == 0 && response.Failure == nil && response.Finish.Kind != model.FinishContentFilter {
		return attemptFailed, transient("empty_response", "The upstream returned no content.")
	}
	if response.Failure != nil {
		return attemptFailed, transient("upstream_reported_failure", "The upstream reported a failed response.")
	}
	if refusal := state.evaluateCollectedResponse(response); refusal != nil {
		ledger := model.Ledger{Losses: mergeLosses(state.failureLedger.Losses, nil), Repairs: mergeRepairs(state.failureLedger.Repairs, nil)}
		state.recordResponse(response, ledger, responseMeta{Blocked: true})
		state.applyLossHeaders(ledger.Losses)
		if !state.settle("blocked", refusal, response.Usage, upstream.endpoint.model) {
			refusal = internal("audit_unavailable", "The response could not be recorded.")
		}
		state.reply(refusal)
		return attemptEnded, refusal
	}
	if upstream.response != nil {
		state.normalizeCustomTools(response)
		state.restoreResponse(response)
	}
	payload, losses, err := state.coder.EncodeResponse(response, state.clientPeer())
	if err != nil {
		return attemptEnded, internal("encode_failed", "The response could not be encoded.")
	}
	state.failureLedger.Losses = append(state.failureLedger.Losses, losses...)
	if limit := state.pipeline.settings.MaxResponseBytes; limit > 0 && int64(len(payload)) > limit {
		return attemptEnded, failure("upstream_failure", model.ClassTransient, "response_too_large", "The upstream response exceeded the size limit.", http.StatusBadGateway)
	}
	state.recordFirstToken()
	ledger := model.Ledger{Losses: mergeLosses(state.failureLedger.Losses, nil), Repairs: mergeRepairs(state.failureLedger.Repairs, nil)}
	state.recordResponse(response, ledger, responseMeta{Identify: true, Stored: state.responseStore()})
	state.applyLossHeaders(ledger.Losses)
	if !state.settle("completed", nil, response.Usage, upstream.endpoint.model) {
		failure := internal("audit_unavailable", "The response could not be recorded.")
		state.reply(failure)
		return attemptEnded, failure
	}
	header := state.writer.Header()
	header.Set("Content-Type", "application/json")
	state.writer.WriteHeader(http.StatusOK)
	_, _ = state.writer.Write(payload)
	return attemptCompleted, nil
}

func (state *call) evaluateCollectedResponse(response *model.Response) *model.Error {
	redactor := state.redactor()
	text := ""
	toolCalls := 0
	for index := range response.Items {
		item := &response.Items[index]
		if item.Kind == model.ItemToolCall || item.Kind == model.ItemServerToolCall {
			toolCalls++
		}
		for _, part := range item.Parts {
			if part.Text == "" {
				continue
			}
			shown, _ := redactor(part.Text, "response.items["+strconv.Itoa(index)+"].text")
			text += shown
		}
	}
	if len(text) > maxPolicyResultBytes {
		text = text[:maxPolicyResultBytes]
	}
	trace := state.baseTrace()
	observe.AppendResponse(trace, response, state.observeOptions())
	return state.evaluateResponse(trace, policy.Response{
		Status: response.Status, Finish: string(response.Finish.Kind), Text: text,
		ItemCount: len(response.Items), ToolCalls: toolCalls, Truncated: response.Finish.Kind == model.FinishLength,
	})
}

func (state *call) gateResponse(response *model.Response) *model.Error {
	for _, item := range response.Items {
		if item.Kind != model.ItemToolCall && item.Kind != model.ItemServerToolCall {
			continue
		}
		if item.ToolCall == nil {
			continue
		}
		if failure := state.toolGate(item); failure != nil {
			return failure
		}
	}
	return nil
}

func (state *call) decorate(response *model.Response) {
	response.Meta.ID = model.ResponseID(state.kind, state.recorder.Call().ID, response.Meta.ID)
	response.Meta.Model = state.request.Model
	response.Source = state.kind
	if response.Meta.Created == 0 {
		response.Meta.Created = time.Now().Unix()
	}
	response.Status = model.ResponseStatus(response)
}

func (state *call) ledger(journal *model.Journal) model.Ledger {
	ledger := model.Ledger{
		Losses:  mergeLosses(state.failureLedger.Losses, journal.Losses()),
		Repairs: mergeRepairs(state.failureLedger.Repairs, nil),
	}
	return ledger
}

type responseMeta struct {
	Committed bool
	Partial   bool
	Truncated bool
	Blocked   bool
	Identify  bool
	Stored    bool
}

func (state *call) recordResponse(response *model.Response, ledger model.Ledger, meta responseMeta) {
	record := state.recorder.Call()
	record.Losses, record.Repairs = convertLosses(ledger.Losses), convertRepairs(ledger.Repairs)
	if meta.Identify && !meta.Blocked {
		record.ResponseID = response.Meta.ID
		record.ResponseStore = meta.Stored
		record.ConversationHash = model.ConversationFingerprint(state.pipeline.secret, state.client, response)
	}
	if !meta.Blocked {
		state.recordTools(response)
	}
	state.recorder.Event(auditevent.KindResponse, auditevent.Response{
		Response: *response, Ledger: ledger, Committed: meta.Committed,
		Partial: meta.Partial, Truncated: meta.Truncated, Blocked: meta.Blocked,
	})
}

func (state *call) responseStore() bool {
	if state.kind != model.Responses {
		return false
	}
	return state.request.Store == nil || *state.request.Store
}

// recordTools writes one event per tool call the model issued and per tool result this request
// added; calls of earlier turns were recorded by the turns that issued them.
func (state *call) recordTools(response *model.Response) {
	for index := range response.Items {
		item := &response.Items[index]
		if item.Kind == model.ItemToolCall && item.ToolCall != nil {
			state.recorder.ToolCall(auditevent.ToolCall{
				ID: item.ToolCall.ID, Kind: string(item.ToolCall.Kind), Name: item.ToolCall.Name,
				Arguments: item.ToolCall.Arguments,
			})
		}
	}
	for index := min(state.offset, len(state.request.Items)); index < len(state.request.Items); index++ {
		item := &state.request.Items[index]
		if item.Kind == model.ItemToolResult {
			state.recorder.ToolResult(auditevent.ToolResult{CallID: item.CallID, Name: item.Name, IsError: item.IsError})
		}
	}
}

// normalizeCustomTools turns a lowered function call back into the custom tool the client
// declared. Streaming does the same at item stop; this covers a JSON upstream response.
func (state *call) normalizeCustomTools(response *model.Response) {
	for index := range response.Items {
		item := &response.Items[index]
		if item.Kind != model.ItemToolCall || item.ToolCall == nil || item.ToolCall.Kind != model.ToolFunction {
			continue
		}
		if state.toolKinds[item.ToolCall.Name] != model.ToolCustom {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(item.ToolCall.Arguments), &payload); err != nil || len(payload) != 1 {
			continue
		}
		input, ok := payload["input"].(string)
		if !ok {
			continue
		}
		item.ToolCall.Kind, item.ToolCall.Arguments = model.ToolCustom, input
	}
}

func (state *call) restoreResponse(response *model.Response) {
	scope := state.scope
	if scope == nil {
		return
	}
	for index := range response.Items {
		item := &response.Items[index]
		for partIndex := range item.Parts {
			part := &item.Parts[partIndex]
			part.Text = scope.Restore(part.Text)
			for citationIndex := range part.Citations {
				part.Citations[citationIndex].CitedText = scope.Restore(part.Citations[citationIndex].CitedText)
				part.Citations[citationIndex].Title = scope.Restore(part.Citations[citationIndex].Title)
			}
		}
		if item.ToolCall != nil && item.ToolCall.Arguments != "" {
			if item.ToolCall.Kind == model.ToolCustom {
				item.ToolCall.Arguments = scope.Restore(item.ToolCall.Arguments)
			} else {
				restorer := scope.JSONRestorer()
				item.ToolCall.Arguments = restorer.Push(item.ToolCall.Arguments) + restorer.Flush()
			}
		}
	}
}

func (state *call) applyLossHeaders(losses []model.Loss) {
	header := state.writer.Header()
	header.Del("X-SafeAgent-Losses")
	header.Del("X-SafeAgent-Loss-Count")
	losses = mergeLosses(losses, nil)
	codes := map[string]bool{}
	for _, loss := range losses {
		codes[string(loss.Code)] = true
	}
	if len(codes) == 0 {
		return
	}
	names := make([]string, 0, len(codes))
	for name := range codes {
		names = append(names, name)
	}
	sort.Strings(names)
	value := ""
	for index, name := range names {
		if index > 0 {
			value += ","
		}
		value += name
	}
	header.Set("X-SafeAgent-Losses", value)
	header.Set("X-SafeAgent-Loss-Count", strconv.Itoa(len(losses)))
}

func mergeLosses(prior, current []model.Loss) []model.Loss {
	return mergeUnique(prior, current)
}

func mergeRepairs(prior, current []model.Repair) []model.Repair {
	return mergeUnique(prior, current)
}

func mergeUnique[T comparable](groups ...[]T) []T {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	seen := make(map[T]bool, total)
	merged := make([]T, 0, total)
	for _, group := range groups {
		for _, item := range group {
			if seen[item] {
				continue
			}
			seen[item] = true
			merged = append(merged, item)
		}
	}
	return merged
}

func convertRepairs(repairs []model.Repair) []entity.Repair {
	converted := make([]entity.Repair, 0, len(repairs))
	for _, item := range repairs {
		converted = append(converted, entity.Repair{Kind: item.Kind, Path: item.Path, Detail: item.Detail, ItemKey: item.ItemKey})
	}
	return converted
}

func convertLosses(losses []model.Loss) []entity.Loss {
	converted := make([]entity.Loss, 0, len(losses))
	for _, item := range losses {
		converted = append(converted, entity.Loss{Path: item.Path, Code: string(item.Code), Class: string(item.Class), Detail: item.Detail, From: string(item.From), To: string(item.To), ItemKey: item.ItemKey})
	}
	return converted
}
