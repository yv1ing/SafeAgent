package codec

import "github.com/yv1ing/SafeAgent/internal/llm/model"

// Unseal opens the SafeAgent envelope a client echoed back so an upstream sees the provider blob. The
// digest recorded when the envelope was issued is kept and compared with the text now carried by the
// item; a signature that fails either step is removed and reported, the text stays. An item without
// text (Gemini sends the signature of a thought on an empty part) has nothing to compare.
func (peer Peer) Unseal(item *model.Item) []model.Loss {
	if peer.Side != Client || item == nil {
		return nil
	}
	signature := signatureOf(item)
	if signature == nil || signature.Blob == "" {
		return nil
	}
	if peer.Envelope == nil {
		clearSignature(item)
		return []model.Loss{unverified("signature is not a SafeAgent envelope")}
	}
	opened, ok := peer.Envelope.Open(signature.Blob)
	if !ok {
		clearSignature(item)
		return []model.Loss{unverified("signature is not a SafeAgent envelope")}
	}
	if item.Kind == model.ItemReasoning && item.Text() != "" && opened.TextDigest != "" && opened.TextDigest != model.TextDigest(item.Text()) {
		clearSignature(item)
		return []model.Loss{unverified("text no longer matches the text the signature was issued for")}
	}
	if item.Kind == model.ItemToolCall && item.ToolCall != nil && opened.TextDigest != "" &&
		opened.TextDigest != model.TextDigest(toolSignatureBinding(item.ToolCall.Name, item.ToolCall.Arguments)) {
		clearSignature(item)
		return []model.Loss{unverified("tool arguments no longer match the call the signature was issued for")}
	}
	*signature = opened
	if item.Kind == model.ItemReasoning && item.Text() == "" && opened.Text != "" {
		item.Parts = append(item.Parts, model.Part{Kind: model.PartReasoningText, Text: opened.Text})
	}
	return nil
}

func unverified(detail string) model.Loss {
	return model.Loss{Path: "signature", Code: model.LossSignatureUnverified, Class: model.LossDegraded, Detail: detail}
}

func signatureOf(item *model.Item) *model.Signature {
	if item.Reasoning != nil && item.Reasoning.Signature != nil {
		return item.Reasoning.Signature
	}
	if item.ToolCall != nil && item.ToolCall.Signature != nil {
		return item.ToolCall.Signature
	}
	return nil
}

func clearSignature(item *model.Item) {
	if item.Reasoning != nil {
		item.Reasoning.Signature = nil
	}
	if item.ToolCall != nil {
		item.ToolCall.Signature = nil
	}
}
