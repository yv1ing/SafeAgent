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
		citation := protocol.Citation{Kind: "citation_source", URL: protocol.String(source["uri"]), Title: protocol.String(source["title"]), Extras: protocol.Extras{Format: protocol.Gemini, Fields: map[string]json.RawMessage{}}}
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
		citations = append(citations, protocol.Citation{Kind: "grounding", URL: protocol.String(web["uri"]), Title: protocol.String(web["title"]), Extras: protocol.Extras{Format: protocol.Gemini, Fields: map[string]json.RawMessage{}}})
	}
	return citations
}

func attachCitations(blocks []protocol.Block, citations []protocol.Citation) []protocol.Block {
	if len(citations) == 0 {
		return blocks
	}
	for index := len(blocks) - 1; index >= 0; index-- {
		if blocks[index].Kind == protocol.KindText {
			blocks[index].Citations = append(blocks[index].Citations, citations...)
			return blocks
		}
	}
	return blocks
}
