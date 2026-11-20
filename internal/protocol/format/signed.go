package format

import "github.com/yv1ing/SafeAgent/internal/protocol"

// Unseal opens the SafeAgent envelope a client echoed back so an upstream sees the provider blob. The
// digest recorded when the envelope was issued is kept and compared with the text now carried by
// the block; a signature that fails either step is removed and reported, the text stays. A block
// without text (Gemini sends the signature of a thought on an empty part) has nothing to compare.
func (peer Peer) Unseal(block *protocol.Block) []protocol.Loss {
	if peer.Side != Client || block == nil {
		return nil
	}
	signature := signatureOf(block)
	if signature == nil || signature.Blob == "" {
		return nil
	}
	if peer.Envelope == nil {
		clearSignature(block)
		return []protocol.Loss{unverified("signature is not a SafeAgent envelope")}
	}
	opened, ok := peer.Envelope.Open(signature.Blob)
	if !ok {
		clearSignature(block)
		return []protocol.Loss{unverified("signature is not a SafeAgent envelope")}
	}
	if block.Reasoning != nil && block.Text != "" && opened.TextDigest != "" && opened.TextDigest != protocol.TextDigest(block.Text) {
		clearSignature(block)
		return []protocol.Loss{unverified("text no longer matches the text the signature was issued for")}
	}
	*signature = opened
	return nil
}

func unverified(detail string) protocol.Loss {
	return protocol.Loss{Path: "signature", Code: protocol.LossSignatureUnverified, Detail: detail}
}

func signatureOf(block *protocol.Block) *protocol.Signature {
	if block.Reasoning != nil && block.Reasoning.Signature != nil {
		return block.Reasoning.Signature
	}
	if block.ToolCall != nil && block.ToolCall.Signature != nil {
		return block.ToolCall.Signature
	}
	return nil
}

func clearSignature(block *protocol.Block) {
	if block.Reasoning != nil {
		block.Reasoning.Signature = nil
	}
	if block.ToolCall != nil {
		block.ToolCall.Signature = nil
	}
}
