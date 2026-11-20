package format

import (
	"encoding/json"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

func DecodeCitations(entry map[string]any) []protocol.Citation {
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
	if kind == protocol.Chat {
		if nested := protocol.Object(object["url_citation"]); nested != nil {
			object = nested
		}
	}
	citation := protocol.Citation{
		Kind: protocol.String(object["type"]), CitedText: protocol.String(object["cited_text"]),
		URL: protocol.String(object["url"]), Title: protocol.String(object["title"]),
		Raw: &protocol.Raw{Format: kind, Fields: map[string]json.RawMessage{"native": protocol.Marshal(object)}},
	}
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
	if native := RawObject(citation.Raw, target); native != nil {
		return native, nil
	}
	switch target {
	case protocol.Messages:
		if citation.URL != "" {
			entry := map[string]any{"type": "web_search_result_location", "url": citation.URL, "title": citation.Title, "cited_text": citation.CitedText}
			return entry, nil
		}
		if citation.Start != nil && citation.End != nil {
			entry := map[string]any{
				"type": "char_location", "document_index": 0, "document_title": citation.Title,
				"start_char_index": *citation.Start, "end_char_index": *citation.End, "cited_text": citation.CitedText,
			}
			return entry, nil
		}
	case protocol.Responses:
		if citation.URL == "" {
			break
		}
		annotation := map[string]any{"type": "url_citation", "url": citation.URL, "title": citation.Title}
		if citation.Start != nil {
			annotation["start_index"] = *citation.Start
		}
		if citation.End != nil {
			annotation["end_index"] = *citation.End
		}
		return annotation, nil
	case protocol.Gemini:
		if citation.URL == "" {
			break
		}
		web := map[string]any{"uri": citation.URL}
		if citation.Title != "" {
			web["title"] = citation.Title
		}
		return map[string]any{"web": web}, nil
	case protocol.Chat:
		if citation.URL == "" {
			break
		}
		annotation := map[string]any{"type": "url_citation", "url_citation": map[string]any{"url": citation.URL, "title": citation.Title}}
		if citation.Start != nil {
			annotation["url_citation"].(map[string]any)["start_index"] = *citation.Start
		}
		if citation.End != nil {
			annotation["url_citation"].(map[string]any)["end_index"] = *citation.End
		}
		return annotation, nil
	}
	detail := "citation has no equivalent in the target format"
	if citation.URL == "" {
		detail = "the target carries only URL citations"
	}
	return nil, []protocol.Loss{{Path: "", Code: protocol.LossCitationMapped, Class: protocol.LossDegraded, Detail: detail, From: citationFormat(citation), To: target}}
}

func citationFormat(citation protocol.Citation) protocol.Format {
	if citation.Raw != nil {
		return citation.Raw.Format
	}
	return ""
}
