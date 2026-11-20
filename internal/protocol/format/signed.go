package format

import "github.com/yv1ing/SafeAgent/internal/protocol"

// Unseal opens the SafeAgent envelope a client echoed back so an upstream sees the provider blob. The
// digest recorded when the envelope was issued is kept and compared with the text now carried by the
// item; a signature that fails either step is removed and reported, the text stays. An item without
// text (Gemini sends the signature of a thought on an empty part) has nothing to compare.
func (peer Peer) Unseal(item *protocol.Item) []protocol.Loss {
	if peer.Side != Client || item == nil {
		return nil
	}
	signature := signatureOf(item)
	if signature == nil || signature.Blob == "" {
		return nil
	}
	if peer.Envelope == nil {
		clearSignature(item)
		return []protocol.Loss{unverified("signature is not a SafeAgent envelope")}
	}
	opened, ok := peer.Envelope.Open(signature.Blob)
	if !ok {
		clearSignature(item)
		return []protocol.Loss{unverified("signature is not a SafeAgent envelope")}
	}
	if item.Kind == protocol.ItemReasoning && item.Text() != "" && opened.TextDigest != "" && opened.TextDigest != protocol.TextDigest(item.Text()) {
		clearSignature(item)
		return []protocol.Loss{unverified("text no longer matches the text the signature was issued for")}
	}
	*signature = opened
	if item.Kind == protocol.ItemReasoning && item.Text() == "" && opened.Text != "" {
		item.Parts = append(item.Parts, protocol.Part{Kind: protocol.PartReasoningText, Text: opened.Text})
	}
	return nil
}

func unverified(detail string) protocol.Loss {
	return protocol.Loss{Path: "signature", Code: protocol.LossSignatureUnverified, Class: protocol.LossDegraded, Detail: detail}
}

func signatureOf(item *protocol.Item) *protocol.Signature {
	if item.Reasoning != nil && item.Reasoning.Signature != nil {
		return item.Reasoning.Signature
	}
	if item.ToolCall != nil && item.ToolCall.Signature != nil {
		return item.ToolCall.Signature
	}
	return nil
}

func clearSignature(item *protocol.Item) {
	if item.Reasoning != nil {
		item.Reasoning.Signature = nil
	}
	if item.ToolCall != nil {
		item.ToolCall.Signature = nil
	}
}
