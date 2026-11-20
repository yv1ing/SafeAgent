package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func CompatibleResponseID(format Format, id string) bool {
	switch format {
	case Responses:
		return strings.HasPrefix(id, "resp_")
	case Messages:
		return strings.HasPrefix(id, "msg_")
	case Chat:
		return strings.HasPrefix(id, "chatcmpl-")
	case Gemini:
		return true
	}
	return false
}

func ResponseID(format Format, callID, upstream string) string {
	switch format {
	case Responses:
		if strings.HasPrefix(upstream, "resp_") {
			return upstream
		}
	case Messages:
		if strings.HasPrefix(upstream, "msg_") {
			return upstream
		}
	case Chat:
		if strings.HasPrefix(upstream, "chatcmpl-") {
			return upstream
		}
	case Gemini:
		if upstream != "" {
			return upstream
		}
	}
	switch format {
	case Responses:
		return "resp_" + callID
	case Messages:
		return "msg_" + callID
	case Gemini:
		return callID
	}
	return "chatcmpl-" + callID
}

func itemPrefix(kind ItemKind) string {
	switch kind {
	case ItemMessage:
		return "msg_"
	case ItemReasoning:
		return "rs_"
	case ItemToolCall:
		return "fc_"
	case ItemServerToolCall:
		return "stc_"
	case ItemServerToolResult:
		return "str_"
	case ItemToolResult:
		return "tr_"
	}
	return "item_"
}

func CompatibleItemID(format Format, kind ItemKind, id string) bool {
	if format != Responses {
		return true
	}
	if id == "" {
		return false
	}
	if kind == ItemToolCall {
		return strings.HasPrefix(id, "fc_") || strings.HasPrefix(id, "ctc_")
	}
	return strings.HasPrefix(id, itemPrefix(kind))
}

func ItemID(format Format, kind ItemKind, source, callID string, ordinal int) (string, bool) {
	if format != Responses {
		return "", false
	}
	if CompatibleItemID(format, kind, source) {
		return source, false
	}
	material := callID + "\x00" + strconv.Itoa(ordinal) + "\x00" + string(kind)
	return itemPrefix(kind) + hash8(material), true
}

func CallID(source, salt, name, arguments string) string {
	if source != "" {
		return source
	}
	material := name + "\x00" + arguments
	if salt != "" {
		material = salt + "\x00" + material
	}
	return "call_" + hash8(material)
}

func hash8(material string) string {
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:8])
}
