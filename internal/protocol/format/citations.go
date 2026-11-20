package format

import (
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

func decodeCitations(entry map[string]any) []protocol.Citation {
	var citations []protocol.Citation
	for _, item := range protocol.Array(entry["citations"]) {
		if object := protocol.Object(item); object != nil {
			citations = append(citations, DecodeCitation(protocol.Messages, object))
		}
	}
	for _, item := range protocol.Array(entry["annotations"]) {
		if object := protocol.Object(item); object != nil {
			citations = append(citations, DecodeCitation(protocol.Responses, object))
		}
	}
	return citations
}

func DecodeCitation(kind protocol.Format, object map[string]any) protocol.Citation {
	citation := protocol.Citation{Kind: protocol.String(object["type"]), CitedText: protocol.String(object["cited_text"]), URL: protocol.String(object["url"]), Title: protocol.String(object["title"]), Extras: nativeExtras(kind, object)}
	start, end := object["start_char_index"], object["end_char_index"]
	if kind == protocol.Responses || kind == protocol.Chat {
		start, end = object["start_index"], object["end_index"]
	}
	if value, ok := protocol.Integer(start); ok {
		citation.Start = &value
	}
	if value, ok := protocol.Integer(end); ok {
		citation.End = &value
	}
	return citation
}

func encodeCitations(citations []protocol.Citation, target protocol.Format) ([]any, []protocol.Loss) {
	var losses []protocol.Loss
	encoded := make([]any, 0, len(citations))
	for index, citation := range citations {
		entry, nested := EncodeCitation(citation, target)
		losses = append(losses, prefixLosses(nested, "citations["+strconv.Itoa(index)+"]")...)
		if entry != nil {
			encoded = append(encoded, entry)
		}
	}
	return encoded, losses
}

// EncodeCitation returns nil, with the reason in the losses, when the target has no equivalent.
func EncodeCitation(citation protocol.Citation, target protocol.Format) (map[string]any, []protocol.Loss) {
	if target != protocol.Messages && target != protocol.Responses {
		return nil, []protocol.Loss{{Path: "", Code: protocol.LossCitationDropped, Detail: "target has no citation field", To: target}}
	}
	if native := nativeObject(citation.Extras, target); native != nil {
		return native, nil
	}
	if target == protocol.Responses && citation.URL != "" {
		annotation := map[string]any{"type": "url_citation", "url": citation.URL, "title": citation.Title}
		if citation.Start != nil {
			annotation["start_index"] = *citation.Start
		}
		if citation.End != nil {
			annotation["end_index"] = *citation.End
		}
		return annotation, nil
	}
	return nil, []protocol.Loss{{Path: "", Code: protocol.LossCitationDropped, Detail: "citation has no equivalent in the target format", From: citation.Extras.Format, To: target}}
}
