package model

import "sort"

type journalItem struct {
	item  Item
	parts map[int]Part
	order []int
}

// Journal accumulates canonical stream events into the response that is sent to the client and
// stored in the audit record. It is the single state representation of a response: decoders
// assemble their own input, the session restores and gates events, and the journal records what
// actually happened.
type Journal struct {
	meta    ResponseMeta
	items   map[int]*journalItem
	order   []int
	usage   *Usage
	finish  Finish
	failure *Error
	losses  []Loss
	raw     *Raw
}

func NewJournal() *Journal {
	return &Journal{items: map[int]*journalItem{}}
}

func (journal *Journal) Apply(event Event) {
	if event.Meta != nil {
		journal.meta = *event.Meta
	}
	switch event.Kind {
	case EventItemStart:
		if event.Item == nil {
			return
		}
		state := &journalItem{item: event.Item.Clone(), parts: map[int]Part{}}
		journal.items[event.ItemIndex] = state
		journal.order = append(journal.order, event.ItemIndex)
	case EventPartStart:
		state := journal.items[event.ItemIndex]
		if state == nil || event.Part == nil {
			return
		}
		state.parts[event.PartIndex] = event.Part.Clone()
		state.order = append(state.order, event.PartIndex)
	case EventPartDelta:
		state := journal.items[event.ItemIndex]
		if state == nil || event.Delta == "" {
			return
		}
		part, found := state.parts[event.PartIndex]
		if !found {
			return
		}
		if streamedKind(part.Kind) {
			part.Text += event.Delta
		}
		state.parts[event.PartIndex] = part
	case EventPartStop:
		state := journal.items[event.ItemIndex]
		if state == nil {
			return
		}
		part, found := state.parts[event.PartIndex]
		if !found {
			return
		}
		if event.Part != nil {
			MergePart(&part, *event.Part)
		}
		state.parts[event.PartIndex] = part
	case EventItemStop:
		state := journal.items[event.ItemIndex]
		if state == nil {
			return
		}
		if event.Item != nil {
			MergeItem(&state.item, *event.Item)
		}
		journal.materialize(state)
	case EventMessageDelta:
		if event.Usage != nil {
			journal.usage = event.Usage
		}
		if event.Failure != nil {
			journal.failure = event.Failure
		}
		if event.Finish != nil {
			journal.finish = *event.Finish
		}
		if event.Raw != nil {
			journal.raw = event.Raw.Clone()
		}
	case EventError:
		journal.failure = event.Failure
	}
	journal.losses = append(journal.losses, event.Losses...)
}

// materialize folds the ordered parts of an item into its snapshot and moves streamed tool
// arguments onto the call. Tool argument parts are transport detail, not content.
func (journal *Journal) materialize(state *journalItem) {
	order := append([]int(nil), state.order...)
	sort.Ints(order)
	parts := make([]Part, 0, len(order))
	arguments := ""
	for _, index := range order {
		part, found := state.parts[index]
		if !found {
			continue
		}
		if part.Kind == PartToolArguments {
			arguments = part.Text
			continue
		}
		part.Ordinal = len(parts)
		parts = append(parts, part)
	}
	state.item.Parts = parts
	if state.item.ToolCall != nil && state.item.ToolCall.Arguments == "" {
		state.item.ToolCall.Arguments = arguments
	}
}

func (journal *Journal) Items() []Item {
	order := append([]int(nil), journal.order...)
	sort.Ints(order)
	items := make([]Item, 0, len(order))
	for _, index := range order {
		state := journal.items[index]
		if state == nil {
			continue
		}
		journal.materialize(state)
		item := state.item.Clone()
		item.Ordinal = len(items)
		items = append(items, item)
	}
	return items
}

func (journal *Journal) Item(index int) *Item {
	state := journal.items[index]
	if state == nil {
		return nil
	}
	item := state.item.Clone()
	return &item
}

func (journal *Journal) SetMeta(meta ResponseMeta) { journal.meta = meta }

func (journal *Journal) Meta() ResponseMeta { return journal.meta }

func (journal *Journal) Usage() *Usage { return journal.usage }

func (journal *Journal) Finish() Finish { return journal.finish }

func (journal *Journal) Failure() *Error { return journal.failure }

func (journal *Journal) Losses() []Loss { return journal.losses }

func (journal *Journal) Content() bool {
	for _, state := range journal.items {
		if state.item.HasText() {
			return true
		}
		if state.item.Reasoning != nil && state.item.Reasoning.Signature != nil {
			return true
		}
		if state.item.ToolCall != nil || state.item.Server != nil || len(state.item.Native) > 0 {
			return true
		}
		if len(state.parts) > 0 {
			return true
		}
	}
	return false
}

func (journal *Journal) Response(source Format) *Response {
	response := &Response{Source: source, Meta: journal.meta, Items: journal.Items(), Finish: journal.finish, Usage: journal.usage, Failure: journal.failure, Raw: journal.raw}
	response.Status = ResponseStatus(response)
	return response
}

// ResponseStatus reports the wire status of a response from its failure and finish reason.
func ResponseStatus(response *Response) string {
	if response.Failure != nil {
		return "failed"
	}
	if response.Finish.Kind == FinishLength {
		return "incomplete"
	}
	return "completed"
}

func streamedKind(kind PartKind) bool {
	switch kind {
	case PartText, PartRefusal, PartReasoningText, PartReasoningSummary, PartToolArguments:
		return true
	}
	return false
}
