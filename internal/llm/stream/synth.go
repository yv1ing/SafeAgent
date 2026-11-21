package stream

import "github.com/yv1ing/SafeAgent/internal/llm/model"

// Events turns a complete response into the canonical event sequence a streaming client needs. It
// is used when the upstream answered with JSON but the client asked for a stream.
func Events(response *model.Response) []model.Event {
	var events []model.Event
	meta := response.Meta
	events = append(events, model.Event{Kind: model.EventMessageStart, Meta: &meta})
	for itemIndex, item := range response.Items {
		snapshot := item.Clone()
		for index := range snapshot.Parts {
			snapshot.Parts[index] = model.WithStreamedContent(snapshot.Parts[index], "")
		}
		if snapshot.ToolCall != nil {
			snapshot.ToolCall.Arguments = ""
		}
		events = append(events, model.Event{Kind: model.EventItemStart, ItemIndex: itemIndex, ItemID: item.ID, Item: &snapshot})
		for partIndex, part := range item.Parts {
			content := model.StreamedContent(part)
			static := part.Clone()
			static = model.WithStreamedContent(static, "")
			events = append(events, model.Event{Kind: model.EventPartStart, ItemIndex: itemIndex, ItemID: item.ID, PartIndex: partIndex, PartID: part.ID, Part: &static})
			if content != "" {
				events = append(events, model.Event{Kind: model.EventPartDelta, ItemIndex: itemIndex, ItemID: item.ID, PartIndex: partIndex, PartID: part.ID, Delta: content})
			}
			final := part.Clone()
			events = append(events, model.Event{Kind: model.EventPartStop, ItemIndex: itemIndex, ItemID: item.ID, PartIndex: partIndex, PartID: part.ID, Part: &final})
		}
		final := item.Clone()
		events = append(events, model.Event{Kind: model.EventItemStop, ItemIndex: itemIndex, ItemID: item.ID, Item: &final})
	}
	finish := response.Finish
	events = append(events, model.Event{Kind: model.EventMessageDelta, Usage: response.Usage, Finish: &finish, Failure: response.Failure})
	events = append(events, model.Event{Kind: model.EventMessageStop})
	return events
}
