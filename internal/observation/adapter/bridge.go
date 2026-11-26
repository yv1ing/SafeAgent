package adapter

import (
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/redact"
)

// ScopeRedactor is the single bridge between the redaction engine and the observation model. The
// online admission path and the replay path both build their Values through it, so a placeholder,
// its raw range and its shown range can never be derived differently in the two paths.
func ScopeRedactor(scope *redact.Scope) Redactor {
	if scope == nil {
		return nil
	}
	return func(text, path string) observation.Value {
		shown, result := scope.Replace(text, path)
		if result == nil {
			return observation.NewValue(text, shown, nil, nil)
		}
		if result.Blocked {
			placeholder := "[blocked]"
			return observation.NewValue(text, placeholder,
				[]observation.Span{{Start: 0, End: len(text), Label: "blocked"}},
				[]observation.ShownSpan{{Start: 0, End: len(placeholder), Label: "blocked", Action: string(redact.ActionBlock)}},
			)
		}
		spans := make([]observation.Span, 0, len(result.Hits))
		for _, hit := range result.Hits {
			spans = append(spans, observation.Span{Start: hit.Start, End: hit.End, Label: hit.Label, ValueID: hit.ValueID, Secret: hit.Secret})
		}
		shownSpans := make([]observation.ShownSpan, 0, len(result.Marks))
		for _, mark := range result.Marks {
			shownSpans = append(shownSpans, observation.ShownSpan{
				Start: mark.ShownStart, End: mark.ShownEnd, Label: mark.Label,
				Action: string(mark.Action), ValueID: mark.ValueID,
			})
		}
		return observation.NewValue(text, shown, spans, shownSpans)
	}
}
