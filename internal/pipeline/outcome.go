package pipeline

import (
	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// settlement is the terminal state and outcome pair written for one call. The pipeline has one
// mapping from a protocol failure to it, so the call row, the metric queries and the console agree
// on what "failed" means for every lane.
type settlement struct {
	state   string
	outcome classify.Outcome
}

// settlementFor maps a failure to the terminal row state and outcome. A policy refusal completed the
// protocol exchange it was asked to perform, and an auxiliary endpoint that does not serve the
// request is a normal negative answer: neither is a gateway failure. Upstream errors, client errors
// and requests the upstream rejected are failures, as is a cancelled call that never completed.
func settlementFor(failure *model.Error, lane classify.Lane) settlement {
	if failure == nil {
		return settlement{state: "completed", outcome: classify.OutcomeOK}
	}
	switch failure.Class {
	case model.ClassPolicy:
		return settlement{state: "completed", outcome: classify.OutcomeBlocked}
	case model.ClassCancelled:
		return settlement{state: "cancelled", outcome: classify.OutcomeCancelled}
	case model.ClassInvalid:
		if lane == classify.LaneAuxiliary {
			if outcome, ok := auxiliaryRefusalOutcome(failure.Code); ok {
				return settlement{state: "completed", outcome: outcome}
			}
		}
		return settlement{state: "failed", outcome: classify.OutcomeClientError}
	case model.ClassRejected:
		return settlement{state: "failed", outcome: classify.OutcomeRejected}
	case model.ClassRateLimit:
		return settlement{state: "failed", outcome: classify.OutcomeRateLimited}
	default:
		return settlement{state: "failed", outcome: classify.OutcomeUpstreamError}
	}
}

// auxiliaryRefusalOutcome recognizes the refusals an auxiliary endpoint returns because it does not
// serve that request at all. The HTTP response is still the protocol error; only the accounting
// differs from a gateway failure.
func auxiliaryRefusalOutcome(code string) (classify.Outcome, bool) {
	switch code {
	case "auxiliary_unsupported", "model_kind_mismatch", "unsupported_feature":
		return classify.OutcomeUnsupported, true
	case "model_not_found", "response_not_found", "previous_response_not_found":
		return classify.OutcomeNotFound, true
	}
	return "", false
}
