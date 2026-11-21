package gemini

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// candidateCitations lifts Gemini's two citation shapes into the canonical model: explicit citation
// sources and grounding chunks from Google Search.
func candidateCitations(candidate map[string]any) []model.Citation {
	citations := []model.Citation{}
	metadata := model.Object(candidate["citationMetadata"])
	for _, item := range model.Array(metadata["citationSources"]) {
		source := model.Object(item)
		if source == nil {
			continue
		}
		citation := model.Citation{
			Kind: "citation_source", URL: model.String(source["uri"]), Title: model.String(source["title"]),
			Raw: &model.Raw{Format: model.Gemini, Fields: map[string]json.RawMessage{"native": model.Marshal(source)}},
		}
		if start, ok := model.Integer(source["startIndex"]); ok {
			citation.Start = &start
		}
		if end, ok := model.Integer(source["endIndex"]); ok {
			citation.End = &end
		}
		citations = append(citations, citation)
	}
	grounding := model.Object(candidate["groundingMetadata"])
	for _, item := range model.Array(grounding["groundingChunks"]) {
		chunk := model.Object(item)
		web := model.Object(chunk["web"])
		if web == nil {
			continue
		}
		citations = append(citations, model.Citation{
			Kind: "grounding", URL: model.String(web["uri"]), Title: model.String(web["title"]),
			Raw: &model.Raw{Format: model.Gemini, Fields: map[string]json.RawMessage{"native": model.Marshal(web)}},
		})
	}
	return citations
}

func attachCitations(items []model.Item, citations []model.Citation) []model.Item {
	if len(citations) == 0 {
		return items
	}
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item := &items[itemIndex]
		if item.Kind != model.ItemMessage {
			continue
		}
		for partIndex := len(item.Parts) - 1; partIndex >= 0; partIndex-- {
			if item.Parts[partIndex].Kind == model.PartText {
				item.Parts[partIndex].Citations = append(item.Parts[partIndex].Citations, citations...)
				return items
			}
		}
	}
	return items
}

func encodeCitations(citations []model.Citation) (map[string]any, []model.Loss) {
	var sources, chunks []any
	var losses []model.Loss
	for _, citation := range citations {
		entry, nested := geminiCitation(citation)
		losses = append(losses, nested...)
		if entry == nil {
			continue
		}
		if citation.Kind == "citation_source" {
			sources = append(sources, entry)
		} else {
			chunks = append(chunks, entry)
		}
	}
	encoded := map[string]any{}
	if len(sources) > 0 {
		encoded["citationMetadata"] = map[string]any{"citationSources": sources}
	}
	if len(chunks) > 0 {
		encoded["groundingMetadata"] = map[string]any{"groundingChunks": chunks}
	}
	if len(encoded) == 0 {
		return nil, losses
	}
	return encoded, losses
}

func geminiCitation(citation model.Citation) (map[string]any, []model.Loss) {
	if native := model.Object(citation.Raw.For(model.Gemini)["native"]); native != nil {
		if citation.Kind == "citation_source" {
			return native, nil
		}
		if native["uri"] != nil {
			return map[string]any{"web": native}, nil
		}
		return native, nil
	}
	if citation.URL == "" {
		return nil, []model.Loss{{Path: "citations", Code: model.LossCitationMapped, Class: model.LossDegraded, Detail: "Gemini carries only URL citations", To: model.Gemini}}
	}
	entry := map[string]any{"uri": citation.URL}
	if citation.Title != "" {
		entry["title"] = citation.Title
	}
	if citation.Kind == "citation_source" {
		if citation.Start != nil {
			entry["startIndex"] = *citation.Start
		}
		if citation.End != nil {
			entry["endIndex"] = *citation.End
		}
		return entry, nil
	}
	return map[string]any{"web": entry}, nil
}
