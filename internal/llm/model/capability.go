package model

import (
	"strconv"
	"strings"
)

type MediaCapability struct {
	ImageURL        bool
	ImageData       bool
	ImageFileID     bool
	DocumentURL     bool
	DocumentData    bool
	DocumentFileID  bool
	DocumentMIME    []string
	AudioInput      bool
	VideoInput      bool
	ToolResultMedia bool
}

type ToolCapability struct {
	Custom             bool
	CustomGrammar      bool
	ServerCapabilities []string
	Strict             bool
	Parallel           bool
}

type ReasoningCapability struct {
	Full       bool
	Summary    bool
	Redacted   bool
	Signatures bool
}

type Capability struct {
	Format        Format
	Media         MediaCapability
	Tools         ToolCapability
	Reasoning     ReasoningCapability
	Citations     bool
	CacheControl  bool
	TopK          bool
	StopSequences bool
	StreamUsage   bool
	Logprobs      bool
	Refusal       bool
	MultiChoice   bool
	NativeCount   bool
	ToolArgStream bool
}

// Compat records behavioral quirks of a concrete upstream that its wire format does not imply.
// Codecs consult it instead of branching on a vendor name.
type Compat struct {
	RequireMaxTokens    bool
	IgnoreTopK          bool
	IgnoreBudgetTokens  bool
	IgnoreServiceTier   bool
	NoCustomToolType    bool
	NoReasoning         bool
	NoTools             bool
	HTTP200FailedStatus bool
	DefaultMaxOutput    int64
}

// Profile is the capability and compatibility of one endpoint; codecs are given the profile of the
// party they talk to and adapt their wire output to it.
type Profile struct {
	Capability Capability
	Compat     Compat
}

func (cap Capability) SupportsServer(capability string) bool {
	for _, supported := range cap.Tools.ServerCapabilities {
		if supported == capability {
			return true
		}
	}
	return false
}

// ValidateRequestCapability reports conversions that change meaning before any upstream call. A
// fatal loss means the request cannot be represented by the target; degraded losses are recorded
// and the request proceeds.
func ValidateRequestCapability(request *Request, cap Capability) []Loss {
	var losses []Loss
	source := request.Source
	loss := func(path string, code LossCode, class LossClass, detail string) {
		losses = append(losses, Loss{Path: path, Code: code, Class: class, Detail: detail, From: source, To: cap.Format})
	}
	for index := range request.Items {
		item := &request.Items[index]
		path := "items[" + strconv.Itoa(index) + "]"
		switch item.Kind {
		case ItemToolResult, ItemServerToolResult:
			for partIndex := range item.Parts {
				part := &item.Parts[partIndex]
				if part.Kind != PartMedia {
					continue
				}
				if !cap.Media.ToolResultMedia {
					loss(path+".parts["+strconv.Itoa(partIndex)+"].media", LossMediaMapped, LossFatal,
						"the target cannot carry media inside a tool result")
				} else if supported, reason := mediaSupported(part.Media, cap); !supported {
					loss(path+".parts["+strconv.Itoa(partIndex)+"].media", LossMediaMapped, LossFatal, reason)
				}
			}
			if item.Kind == ItemServerToolResult && source != cap.Format {
				loss(path, LossServerToolMapped, LossDegraded, "provider-private server tool result is not replayable across protocols")
			}
		case ItemServerToolCall:
			if source != cap.Format {
				loss(path, LossServerToolMapped, LossDegraded, "provider-executed tool call is mapped by capability")
			}
		default:
			for partIndex := range item.Parts {
				part := &item.Parts[partIndex]
				switch part.Kind {
				case PartMedia:
					if supported, reason := mediaSupported(part.Media, cap); !supported {
						loss(path+".parts["+strconv.Itoa(partIndex)+"].media", LossMediaMapped, LossFatal, reason)
					}
				case PartText:
					if len(part.Citations) > 0 && !cap.Citations {
						loss(path+".parts["+strconv.Itoa(partIndex)+"].citations", LossCitationMapped, LossDegraded,
							"the target carries no inline citations")
					}
				}
				if part.CacheControl != nil && !cap.CacheControl {
					loss(path+".parts["+strconv.Itoa(partIndex)+"].cache_control", LossFieldDropped, LossDegraded,
						"the target has no prompt cache markers")
				}
			}
		}
		if item.CacheControl != nil && !cap.CacheControl {
			loss(path+".cache_control", LossFieldDropped, LossDegraded, "the target has no prompt cache markers")
		}
		if item.Kind == ItemReasoning && item.Reasoning != nil && item.Reasoning.Signature != nil && source != cap.Format {
			loss(path+".reasoning.signature", LossSignatureStripped, LossDegraded,
				"a reasoning signature is meaningful only to the provider that issued it; the text is retained")
		}
	}
	for index, tool := range request.Tools {
		path := "tools[" + strconv.Itoa(index) + "]"
		switch tool.Kind {
		case ToolCustom:
			if !cap.Tools.Custom {
				loss(path, LossToolKindMapped, LossDegraded, "the custom tool is exposed as a function taking one string input")
			}
			if len(tool.Format) > 0 && !cap.Tools.CustomGrammar {
				loss(path+".format", LossFieldDropped, LossDegraded, "the target has no tool grammar")
			}
		case ToolServer:
			if source == cap.Format {
				continue
			}
			if tool.Server == nil || !cap.SupportsServer(ServerCapability(tool.Server)) {
				if request.ToolChoice != nil && request.ToolChoice.Mode == "tool" && len(request.ToolChoice.Names) == 1 && request.ToolChoice.Names[0] == tool.Name {
					loss(path, LossServerToolMapped, LossFatal, "the required provider-executed tool has no equivalent in the target")
					continue
				}
				loss(path, LossServerToolMapped, LossDegraded, "the provider-executed tool has no equivalent in the target and is omitted")
			}
		}
		if tool.CacheControl != nil && !cap.CacheControl {
			loss(path+".cache_control", LossFieldDropped, LossDegraded, "the target has no prompt cache markers")
		}
	}
	if request.ParallelToolCalls != nil && !cap.Tools.Parallel {
		loss("parallel_tool_calls", LossFieldDropped, LossDegraded, "the target cannot enforce a parallel tool policy")
	}
	if len(request.Stop) > 0 && !cap.StopSequences {
		loss("stop", LossFieldDropped, LossDegraded, "the target has no stop sequence field")
	}
	if request.TopK != nil && !cap.TopK {
		loss("top_k", LossFieldDropped, LossDegraded, "the target has no top-k sampling")
	}
	return losses
}

// RejectUpstreamState refuses features that would make the gateway proxy provider-side state and
// bypass its own history, redaction and audit. store is accepted because the gateway keeps the
// response itself; previous_response_id is expanded from that history.
func RejectUpstreamState(request *Request) *Error {
	if request.Background != nil && *request.Background {
		return UnsupportedFeature("background_not_supported", "Background responses are not supported.")
	}
	if request.Conversation != "" {
		return UnsupportedFeature("conversation_not_supported", "Server-side conversations are not supported.")
	}
	return nil
}

func UnsupportedFeature(code, message string) *Error {
	return &Error{Kind: "unsupported", Class: ClassInvalid, Code: code, Message: message, Status: 422}
}

func ServerCapability(server *ServerTool) string {
	switch server.Format {
	case Responses:
		switch {
		case strings.HasPrefix(server.Type, "web_search"):
			return "web_search"
		case server.Type == "code_interpreter":
			return "code_execution"
		}
	case Messages:
		switch {
		case strings.HasPrefix(server.Type, "web_search_"):
			return "web_search"
		case strings.HasPrefix(server.Type, "code_execution_"):
			return "code_execution"
		}
	case Gemini:
		switch server.Type {
		case "googleSearch":
			return "web_search"
		case "codeExecution":
			return "code_execution"
		}
	}
	return server.Type
}

func mediaSupported(media *Media, cap Capability) (bool, string) {
	if media == nil {
		return false, "the media part carries no media"
	}
	switch media.Kind {
	case "image":
		if cap.Format == Chat && media.FileID != "" {
			return false, "Chat image input cannot resolve provider file identifiers"
		}
		if cap.Format == Gemini && media.MIMEType == "" {
			return false, "Gemini image input requires a known MIME type"
		}
		return carrierSupported(media, cap.Media.ImageURL, cap.Media.ImageData, cap.Media.ImageFileID)
	case "document":
		if media.MIMEType == "" {
			return false, "the document has no MIME type"
		}
		if len(cap.Media.DocumentMIME) > 0 && !containsString(cap.Media.DocumentMIME, media.MIMEType) {
			return false, "the target cannot represent " + media.MIMEType + " documents"
		}
		if cap.Format == Messages && media.FileID == "" && media.MIMEType != "application/pdf" && media.MIMEType != "text/plain" {
			return false, "Messages document input accepts PDF or plain text"
		}
		return carrierSupported(media, cap.Media.DocumentURL, cap.Media.DocumentData, cap.Media.DocumentFileID)
	case "audio":
		if !cap.Media.AudioInput {
			return false, "the target accepts no audio input"
		}
		if media.MIMEType != "" && !strings.HasPrefix(media.MIMEType, "audio/") {
			return false, "the audio MIME type is unsupported"
		}
		if cap.Format == Chat && (media.Data == "" || !chatAudioMIME(media.MIMEType)) {
			return false, "Chat audio input requires inline WAV or MP3 data"
		}
		if cap.Format == Gemini && media.MIMEType == "" {
			return false, "Gemini audio input requires a known MIME type"
		}
		return carrierSupported(media, true, true, false)
	case "video":
		if !cap.Media.VideoInput {
			return false, "the target accepts no video input"
		}
		if cap.Format == Gemini && media.MIMEType == "" {
			return false, "Gemini video input requires a known MIME type"
		}
		return carrierSupported(media, true, true, false)
	}
	return false, "unknown media kind " + media.Kind
}

func chatAudioMIME(mime string) bool {
	return mime == "audio/wav" || mime == "audio/mpeg" || mime == "audio/mp3"
}

func carrierSupported(media *Media, url, data, fileID bool) (bool, string) {
	switch {
	case media.FileID != "" && !fileID:
		return false, "the target cannot resolve provider file identifiers"
	case media.Data != "" && !data:
		return false, "the target accepts no inline media data"
	case media.URL != "" && !url:
		return false, "the target accepts no media URLs"
	}
	return true, ""
}

func containsString(values []string, value string) bool {
	for _, entry := range values {
		if entry == value {
			return true
		}
	}
	return false
}
