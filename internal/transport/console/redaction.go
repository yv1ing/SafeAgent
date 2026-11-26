package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

// RedactionIndexView is the deduplicated redaction list of one conversation. Raw values never leave
// the service layer; the index only names values by their stable placeholder digest.
type RedactionIndexView struct {
	SessionID string               `json:"session_id" required:"true"`
	Total     int                  `json:"total" required:"true" description:"Number of occurrences across the conversation"`
	Values    []RedactionValueView `json:"values" required:"true"`
}

type RedactionValueView struct {
	ValueID     string                  `json:"value_id" required:"true"`
	Label       string                  `json:"label" required:"true"`
	Action      string                  `json:"action" required:"true" enum:"replace,block,alert"`
	Detector    string                  `json:"detector" required:"true" description:"Derived hint; empty when the label is not a built-in detector"`
	Secret      bool                    `json:"secret" required:"true"`
	Count       int                     `json:"count" required:"true"`
	FirstCallID string                  `json:"first_call_id" required:"true"`
	LastCallID  string                  `json:"last_call_id" required:"true"`
	Occurrences []RedactionLocationView `json:"occurrences" required:"true"`
}

type RedactionLocationView struct {
	CallID        string `json:"call_id" required:"true"`
	Section       string `json:"section" required:"true" enum:"request,response,instructions,params,tools"`
	Ordinal       int    `json:"ordinal" required:"true"`
	Path          string `json:"path" required:"true"`
	CallCreatedAt string `json:"call_created_at" required:"true"`
	Start         int    `json:"start" required:"true"`
	End           int    `json:"end" required:"true"`
	ShownStart    int    `json:"shown_start" required:"true"`
	ShownEnd      int    `json:"shown_end" required:"true"`
}

func (console *Console) registerRedactions() {
	console.add("GET", "/projects/{project_id}/conversations/{session_id}/redactions", "getConversationRedactions", conversationPath{}, RedactionIndexView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		index, err := console.Redaction.Index(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("session_id"))
		if err != nil {
			return nil, err
		}
		return redactionIndexView(index), nil
	})
}

func redactionIndexView(index *service.RedactionIndex) RedactionIndexView {
	view := RedactionIndexView{SessionID: index.SessionID, Total: index.Total, Values: make([]RedactionValueView, 0, len(index.Values))}
	for position := range index.Values {
		value := &index.Values[position]
		locations := make([]RedactionLocationView, 0, len(value.Occurrences))
		for occurrence := range value.Occurrences {
			location := &value.Occurrences[occurrence]
			locations = append(locations, RedactionLocationView{
				CallID: location.CallID, Section: string(location.Section), Ordinal: location.Ordinal,
				Path: location.Path, CallCreatedAt: stamp(location.CallCreatedAt),
				Start: location.Start, End: location.End, ShownStart: location.ShownStart, ShownEnd: location.ShownEnd,
			})
		}
		view.Values = append(view.Values, RedactionValueView{
			ValueID: value.ValueID, Label: value.Label, Action: string(value.Action), Detector: value.Detector,
			Secret: value.Secret, Count: value.Count, FirstCallID: value.FirstCallID,
			LastCallID: value.LastCallID, Occurrences: locations,
		})
	}
	return view
}
