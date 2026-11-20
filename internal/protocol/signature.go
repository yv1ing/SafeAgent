package protocol

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

const (
	signaturePrefix   = "SA1"
	signatureMACBytes = 16
)

type Signature struct {
	Vendor     Vendor `json:"v"`
	Model      string `json:"m,omitempty"`
	Blob       string `json:"b"`
	TextDigest string `json:"d,omitempty"`
}

type signaturePayload struct {
	Version    string `json:"v"`
	Vendor     string `json:"w"`
	Model      string `json:"m,omitempty"`
	Blob       string `json:"b"`
	TextDigest string `json:"d,omitempty"`
}

func TextDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func SealSignature(secret []byte, signature Signature) (string, error) {
	payload, err := json.Marshal(signaturePayload{Version: signaturePrefix, Vendor: string(signature.Vendor), Model: signature.Model, Blob: signature.Blob, TextDigest: signature.TextDigest})
	if err != nil {
		return "", err
	}
	raw := make([]byte, 0, len(signaturePrefix)+1+signatureMACBytes+len(payload))
	raw = append(raw, signaturePrefix...)
	raw = append(raw, 0)
	raw = append(raw, signatureMAC(secret, payload)...)
	raw = append(raw, payload...)
	return base64.StdEncoding.EncodeToString(raw), nil
}

func OpenSignature(secret []byte, encoded string) (Signature, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) < len(signaturePrefix)+1+signatureMACBytes {
		return Signature{}, false
	}
	if string(raw[:len(signaturePrefix)]) != signaturePrefix || raw[len(signaturePrefix)] != 0 {
		return Signature{}, false
	}
	mac := raw[len(signaturePrefix)+1 : len(signaturePrefix)+1+signatureMACBytes]
	payload := raw[len(signaturePrefix)+1+signatureMACBytes:]
	if subtle.ConstantTimeCompare(mac, signatureMAC(secret, payload)) != 1 {
		return Signature{}, false
	}
	var decoded signaturePayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return Signature{}, false
	}
	if decoded.Version != signaturePrefix || decoded.Blob == "" {
		return Signature{}, false
	}
	return Signature{Vendor: Vendor(decoded.Vendor), Model: decoded.Model, Blob: decoded.Blob, TextDigest: decoded.TextDigest}, true
}

func signatureMAC(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("safeagent/signature/v1\x00"))
	mac.Write(payload)
	return mac.Sum(nil)[:signatureMACBytes]
}

// AdaptReasoning removes reasoning that only its issuing vendor can interpret: foreign signatures
// are stripped (the text stays) and redacted reasoning is dropped.
func AdaptReasoning(request *Request, vendor Vendor) []Loss {
	var losses []Loss
	for messageIndex := range request.Messages {
		message := &request.Messages[messageIndex]
		for blockIndex := range message.Blocks {
			block := &message.Blocks[blockIndex]
			path := "messages[" + strconv.Itoa(messageIndex) + "].blocks[" + strconv.Itoa(blockIndex) + "]"
			switch block.Kind {
			case KindReasoning:
				if block.Reasoning == nil || block.Reasoning.Signature == nil {
					continue
				}
				if block.Reasoning.Signature.Vendor != vendor {
					block.Reasoning.Signature = nil
					losses = append(losses, Loss{Path: path, Code: LossSignatureStripped, Detail: "reasoning signature was issued by a different provider; the text is retained without a signature"})
				}
			case KindRedacted:
				if block.Extras.Format == "" || block.Extras.Format.DefaultVendor() == vendor {
					continue
				}
				block.Kind = ""
				losses = append(losses, Loss{Path: path, Code: LossFieldDropped, Detail: "redacted reasoning is only meaningful to its originating provider", From: block.Extras.Format})
			}
		}
		message.Blocks = dropEmptyBlocks(message.Blocks)
	}
	return losses
}

func dropEmptyBlocks(blocks []Block) []Block {
	result := blocks[:0]
	for _, block := range blocks {
		if block.Kind == "" || block.Kind == KindText && block.Text == "" {
			continue
		}
		result = append(result, block)
	}
	return result
}
