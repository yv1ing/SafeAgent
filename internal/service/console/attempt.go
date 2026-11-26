package console

import (
	"time"

	"github.com/yv1ing/SafeAgent/internal/classify"
	"github.com/yv1ing/SafeAgent/internal/storage/auditevent"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

const (
	attemptRunning   = "running"
	attemptSucceeded = "succeeded"
	attemptFailed    = "failed"
	attemptCancelled = "cancelled"
)

// CallAttempt is one upstream try of one call. The call row only keeps the final outcome; a retry
// waterfall needs every attempt and whether a later one recovered the call.
type CallAttempt struct {
	Index                int
	ChannelID            string
	ChannelName          string
	ModelID              string
	UpstreamModel        string
	Format               string
	KeyID                string
	StartOffset          int64
	EndOffset            int64
	FirstTokenOffset     int64
	DurationMilliseconds int64
	Result               string
	Code                 string
	Class                string
	Recovered            bool
}

// BuildAttempts rebuilds the attempt waterfall from a call's timeline. The returned map ties each
// attempt event's sequence to the attempt it belongs to, so a replay step can name its node.
func BuildAttempts(call *entity.Call, entries []auditevent.Entry) ([]CallAttempt, map[int64]int) {
	attempts := make([]CallAttempt, 0, 2)
	bySequence := map[int64]int{}
	open := -1
	offset := func(at time.Time) int64 { return max(at.Sub(call.CreatedAt).Milliseconds(), 0) }
	start := func(at time.Time) int {
		attempts = append(attempts, CallAttempt{Index: len(attempts) + 1, StartOffset: offset(at), Result: attemptRunning})
		return len(attempts) - 1
	}
	for _, entry := range entries {
		switch entry.Kind {
		case auditevent.KindAttempt:
			payload := entry.Decoded.Attempt
			if payload == nil {
				continue
			}
			open = start(entry.At)
			attempt := &attempts[open]
			attempt.ChannelID, attempt.ChannelName = payload.ChannelID, payload.ChannelName
			attempt.ModelID, attempt.UpstreamModel = payload.ModelID, payload.UpstreamModel
			attempt.Format, attempt.KeyID = payload.Format, payload.KeyID
			bySequence[entry.Sequence] = attempt.Index
		case auditevent.KindFirstToken:
			if open >= 0 && entry.Decoded.FirstToken != nil {
				attempts[open].FirstTokenOffset = entry.Decoded.FirstToken.Milliseconds
				bySequence[entry.Sequence] = attempts[open].Index
			}
		case auditevent.KindAttemptFailed:
			payload := entry.Decoded.AttemptFailure
			if payload == nil {
				continue
			}
			if open < 0 {
				open = start(entry.At)
			}
			attempt := &attempts[open]
			if attempt.ChannelID == "" {
				attempt.ChannelID = payload.ChannelID
			}
			if attempt.KeyID == "" {
				attempt.KeyID = payload.KeyID
			}
			attempt.Result, attempt.Code, attempt.Class, attempt.EndOffset = attemptFailed, payload.Code, payload.Class, offset(entry.At)
			bySequence[entry.Sequence] = attempt.Index
			open = -1
		}
	}
	if open >= 0 {
		attempt := &attempts[open]
		attempt.Result = terminalAttemptResult(call)
		attempt.EndOffset = int64(call.DurationMilliseconds)
		if call.TerminatedAt != nil {
			attempt.EndOffset = offset(*call.TerminatedAt)
		}
	}
	for index := range attempts {
		if attempts[index].EndOffset > attempts[index].StartOffset {
			attempts[index].DurationMilliseconds = attempts[index].EndOffset - attempts[index].StartOffset
		}
		if attempts[index].Result != attemptFailed {
			continue
		}
		for later := index + 1; later < len(attempts); later++ {
			if attempts[later].Result == attemptSucceeded {
				attempts[index].Recovered = true
				break
			}
		}
	}
	return attempts, bySequence
}

func terminalAttemptResult(call *entity.Call) string {
	switch {
	case call.State == "completed":
		return attemptSucceeded
	case call.Outcome == classify.OutcomeCancelled:
		return attemptCancelled
	default:
		return attemptFailed
	}
}
