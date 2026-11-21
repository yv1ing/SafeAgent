package model

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
	Text       string `json:"-"`
}

type signaturePayload struct {
	Version    string `json:"v"`
	Vendor     string `json:"w"`
	Model      string `json:"m,omitempty"`
	Blob       string `json:"b"`
	TextDigest string `json:"d,omitempty"`
	Text       string `json:"t,omitempty"`
}

func TextDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func SealSignature(secret []byte, signature Signature) (string, error) {
	payload, err := json.Marshal(signaturePayload{Version: signaturePrefix, Vendor: string(signature.Vendor), Model: signature.Model, Blob: signature.Blob, TextDigest: signature.TextDigest, Text: signature.Text})
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
	return Signature{Vendor: Vendor(decoded.Vendor), Model: decoded.Model, Blob: decoded.Blob, TextDigest: decoded.TextDigest, Text: decoded.Text}, true
}

func signatureMAC(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("safeagent/signature/v1\x00"))
	mac.Write(payload)
	return mac.Sum(nil)[:signatureMACBytes]
}

// AdaptReasoning removes reasoning that only its issuing vendor can interpret: foreign signatures
// are stripped (the text stays) and redacted reasoning from another protocol is dropped.
func AdaptReasoning(request *Request, vendor Vendor, target Format) []Loss {
	var losses []Loss
	for index := range request.Items {
		item := &request.Items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		if item.ToolCall != nil && item.ToolCall.Signature != nil && item.ToolCall.Signature.Vendor != vendor {
			item.ToolCall.Signature = nil
			losses = append(losses, Loss{Path: path + ".tool_call.signature", Code: LossSignatureStripped, Class: LossDegraded, Detail: "the tool call signature was issued by a different provider; the call is retained without a signature", From: request.Source, To: target})
		}
		if item.Kind != ItemReasoning {
			continue
		}
		if item.Reasoning != nil && item.Reasoning.Signature != nil && item.Reasoning.Signature.Vendor != vendor {
			item.Reasoning.Signature = nil
			losses = append(losses, Loss{Path: path + ".reasoning.signature", Code: LossSignatureStripped, Class: LossDegraded, Detail: "reasoning signature was issued by a different provider; the text is retained without a signature", From: request.Source, To: target})
		}
		parts := item.Parts[:0]
		for partIndex, part := range item.Parts {
			if part.Kind == PartRedactedReasoning && target != request.Source {
				losses = append(losses, Loss{Path: path + ".parts[" + strconv.Itoa(partIndex) + "]", Code: LossFieldDropped, Class: LossDegraded, Detail: "redacted reasoning is only meaningful to its originating provider", From: request.Source, To: target})
				continue
			}
			parts = append(parts, part)
		}
		item.Parts = parts
	}
	return losses
}
