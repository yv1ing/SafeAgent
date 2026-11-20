package gemini

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// candidateCitations lifts Gemini's two citation shapes into the canonical model: explicit citation
// sources and grounding chunks from Google Search.
func candidateCitations(candidate map[string]any) []protocol.Citation {
	citations := []protocol.Citation{}
	metadata := protocol.Object(candidate["citationMetadata"])
	for _, item := range protocol.Array(metadata["citationSources"]) {
		source := protocol.Object(item)
		if source == nil {
			continue
		}
		citation := protocol.Citation{
			Kind: "citation_source", URL: protocol.String(source["uri"]), Title: protocol.String(source["title"]),
			Raw: &protocol.Raw{Format: protocol.Gemini, Fields: map[string]json.RawMessage{"native": protocol.Marshal(source)}},
		}
		if start, ok := protocol.Integer(source["startIndex"]); ok {
			citation.Start = &start
		}
		if end, ok := protocol.Integer(source["endIndex"]); ok {
			citation.End = &end
		}
		citations = append(citations, citation)
	}
	grounding := protocol.Object(candidate["groundingMetadata"])
	for _, item := range protocol.Array(grounding["groundingChunks"]) {
		chunk := protocol.Object(item)
		web := protocol.Object(chunk["web"])
		if web == nil {
			continue
		}
		citations = append(citations, protocol.Citation{
			Kind: "grounding", URL: protocol.String(web["uri"]), Title: protocol.String(web["title"]),
			Raw: &protocol.Raw{Format: protocol.Gemini, Fields: map[string]json.RawMessage{"native": protocol.Marshal(web)}},
		})
	}
	return citations
}

func attachCitations(items []protocol.Item, citations []protocol.Citation) []protocol.Item {
	if len(citations) == 0 {
		return items
	}
	for itemIndex := len(items) - 1; itemIndex >= 0; itemIndex-- {
		item := &items[itemIndex]
		if item.Kind != protocol.ItemMessage {
			continue
		}
		for partIndex := len(item.Parts) - 1; partIndex >= 0; partIndex-- {
			if item.Parts[partIndex].Kind == protocol.PartText {
				item.Parts[partIndex].Citations = append(item.Parts[partIndex].Citations, citations...)
				return items
			}
		}
	}
	return items
}

func encodeCitations(citations []protocol.Citation) (map[string]any, []protocol.Loss) {
	var sources, chunks []any
	var losses []protocol.Loss
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

func geminiCitation(citation protocol.Citation) (map[string]any, []protocol.Loss) {
	if native := protocol.Object(citation.Raw.For(protocol.Gemini)["native"]); native != nil {
		if citation.Kind == "citation_source" {
			return native, nil
		}
		if native["uri"] != nil {
			return map[string]any{"web": native}, nil
		}
		return native, nil
	}
	if citation.URL == "" {
		return nil, []protocol.Loss{{Path: "citations", Code: protocol.LossCitationMapped, Class: protocol.LossDegraded, Detail: "Gemini carries only URL citations", To: protocol.Gemini}}
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
