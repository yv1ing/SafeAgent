package console

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/observe"
	"github.com/yv1ing/SafeAgent/internal/redact"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	MaximumTranscript      = 200
	maximumTranscriptBytes = 32 << 20
)

type Transcript struct {
	Trace     *observation.Trace
	Hits      []redact.Hit
	Truncated bool
}

// ConversationTrace rebuilds the observation of a conversation up to and including the call. Every
// text carries both views; the transport decides which one the caller may see.
func (service *CallService) ConversationTrace(ctx context.Context, projectID, callID string) (Transcript, error) {
	chain, err := service.store.CallChain(ctx, callID)
	if err != nil {
		return Transcript{}, err
	}
	truncated := len(chain) > MaximumTranscript
	if truncated {
		chain = chain[len(chain)-MaximumTranscript:]
	}
	project, err := service.store.Project(ctx, projectID)
	if err != nil {
		return Transcript{}, err
	}

	combined := &observation.Trace{ID: callID, Principal: observation.Principal{ProjectID: projectID}}
	collected := []redact.Hit{}
	traces := []*observation.Trace{}
	totalBytes := 0
	for index := len(chain) - 1; index >= 0; index-- {
		call := chain[index]
		events, err := service.store.EventsOfKinds(ctx, []string{call.ID}, auditevent.KindRequest, auditevent.KindResponse)
		if err != nil {
			return Transcript{}, err
		}
		var request auditevent.Request
		var response *auditevent.Response
		requestFound := false
		for _, event := range events {
			totalBytes += len(event.Payload)
			switch event.Kind {
			case auditevent.KindRequest:
				if err := json.Unmarshal(event.Payload, &request); err != nil {
					return Transcript{}, fmt.Errorf("request event of %s: %w", call.ID, err)
				}
				requestFound = true
			case auditevent.KindResponse:
				decoded := new(auditevent.Response)
				if err := json.Unmarshal(event.Payload, decoded); err != nil {
					return Transcript{}, fmt.Errorf("response event of %s: %w", call.ID, err)
				}
				response = decoded
			}
		}
		if !requestFound {
			return Transcript{}, repository.ErrNotFound
		}
		if totalBytes > maximumTranscriptBytes {
			truncated = true
			break
		}
		redactor, callHits, err := service.analyzer.RedactorForCall(ctx, project, call.ID)
		if err != nil {
			return Transcript{}, err
		}
		trace := observe.Stored(request, response, observe.Options{
			TraceID:   call.ID,
			Principal: observation.Principal{ProjectID: projectID, AgentKeyID: call.AgentKeyID},
			Redactor:  redactor,
		})
		collected = append(collected, callHits()...)
		traces = append(traces, trace)
		if len(combined.Tools) == 0 && len(trace.Tools) > 0 {
			combined.Tools = trace.Tools
		}
	}
	for index := len(traces) - 1; index >= 0; index-- {
		trace := traces[index]
		for _, turn := range trace.Turns {
			turnIndex := len(combined.Turns)
			turn.Index = turnIndex
			for position := range turn.Items {
				if turn.Items[position].Locator.CallID == "" {
					turn.Items[position].Locator.CallID = trace.ID
				}
			}
			combined.Turns = append(combined.Turns, turn)
		}
	}
	combined.RebuildSteps()
	return Transcript{Trace: combined, Hits: collected, Truncated: truncated}, nil
}
