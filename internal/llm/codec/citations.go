package codec

import (
	"encoding/json"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func DecodeCitations(entry map[string]any) []model.Citation {
	var citations []model.Citation
	for _, item := range model.Array(entry["citations"]) {
		if object := model.Object(item); object != nil {
			citations = append(citations, DecodeCitation(model.Messages, object))
		}
	}
	for _, item := range model.Array(entry["annotations"]) {
		if object := model.Object(item); object != nil {
			citations = append(citations, DecodeCitation(model.Responses, object))
		}
	}
	return citations
}

func DecodeCitation(kind model.Format, object map[string]any) model.Citation {
	if kind == model.Chat {
		if nested := model.Object(object["url_citation"]); nested != nil {
			object = nested
		}
	}
	citation := model.Citation{
		Kind: model.String(object["type"]), CitedText: model.String(object["cited_text"]),
		URL: model.String(object["url"]), Title: model.String(object["title"]),
		Raw: &model.Raw{Format: kind, Fields: map[string]json.RawMessage{"native": model.Marshal(object)}},
	}
	start, end := object["start_char_index"], object["end_char_index"]
	if kind == model.Responses || kind == model.Chat {
		start, end = object["start_index"], object["end_index"]
	}
	if value, ok := model.Integer(start); ok {
		citation.Start = &value
	}
	if value, ok := model.Integer(end); ok {
		citation.End = &value
	}
	return citation
}

func encodeCitations(citations []model.Citation, target model.Format) ([]any, []model.Loss) {
	var losses []model.Loss
	encoded := make([]any, 0, len(citations))
	for index, citation := range citations {
		entry, nested := EncodeCitation(citation, target)
		losses = append(losses, PrefixLosses(nested, "citations["+strconv.Itoa(index)+"]")...)
		if entry != nil {
			encoded = append(encoded, entry)
		}
	}
	return encoded, losses
}

// EncodeCitation returns nil, with the reason in the losses, when the target has no equivalent.
func EncodeCitation(citation model.Citation, target model.Format) (map[string]any, []model.Loss) {
	if native := RawObject(citation.Raw, target); native != nil {
		return native, nil
	}
	switch target {
	case model.Messages:
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
	case model.Responses:
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
	case model.Gemini:
		if citation.URL == "" {
			break
		}
		web := map[string]any{"uri": citation.URL}
		if citation.Title != "" {
			web["title"] = citation.Title
		}
		return map[string]any{"web": web}, nil
	case model.Chat:
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
	return nil, []model.Loss{{Path: "", Code: model.LossCitationMapped, Class: model.LossDegraded, Detail: detail, From: citationFormat(citation), To: target}}
}

func citationFormat(citation model.Citation) model.Format {
	if citation.Raw != nil {
		return citation.Raw.Format
	}
	return ""
}
