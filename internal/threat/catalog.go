package threat

import "github.com/yv1ing/SafeAgent/internal/observation"

// CatalogueVersion is part of every threat. It changes when a code is added, removed or changes
// meaning.
const CatalogueVersion = "2026.11.2"

const (
	CodeInvalidToolArguments        = "invalid_tool_arguments"
	CodeMissingRequiredArgument     = "missing_required_argument"
	CodeArgumentTypeMismatch        = "argument_type_mismatch"
	CodeArgumentConstraintViolation = "argument_constraint_violation"
	CodeUndeclaredTool              = "undeclared_tool"
	CodeToolChoiceViolation         = "tool_choice_violation"
	CodeUnmatchedToolResult         = "unmatched_tool_result"
	CodePromptInjection             = "prompt_injection"
	CodeHiddenInstructions          = "hidden_instructions"
	CodeDangerousCommand            = "dangerous_command"
	CodeSensitivePath               = "sensitive_path"
	CodePrivateNetworkTarget        = "private_network_target"
	CodeCredentialExposure          = "credential_exposure"
	CodeExfiltrationLink            = "exfiltration_link"
	CodeUntrustedValueReuse         = "untrusted_value_reuse"
	CodeExternalInstructionFollowed = "external_instruction_followed"
	CodeSecretFlow                  = "secret_flow"
	CodeAnalysisIncomplete          = "analysis_incomplete"
	CodeUntrustedToExternalSend     = "untrusted_to_external_send"
	CodePrivateToPublicSink         = "private_to_public_sink"
	CodeCapabilityEscalation        = "capability_escalation"
	CodeConstraintLost              = "constraint_lost_in_compaction"
	CodeMemoryAuthorityCollapse     = "memory_authority_collapse"
	CodeRollbackMismatch            = "rollback_cache_mismatch"
	CodeApprovalReplay              = "approval_replay"
	CodeCaptureGap                  = "capture_gap"
	CodeUnattributedAction          = "unattributed_action"
	CodeProvenanceAuthMismatch      = "provenance_authorization_mismatch"
)

// CodeDefinition is the registry entry a detector must use. It declares what the code means, where
// it can originate and how serious it is; the three axes are derived from the code, the origin and
// the sink instead of being written by hand for every code.
type CodeDefinition struct {
	Code        string               `json:"code"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Severity    string               `json:"severity"`
	Origins     []observation.Origin `json:"origins"`
}

var CodeCatalogue = map[string]CodeDefinition{
	CodeUntrustedToExternalSend: {
		Code: CodeUntrustedToExternalSend, Name: "Untrusted content reached an external send",
		Description: "An external-send action consumed untrusted content without approval or declassification.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginToolResult, observation.OriginToolDescription},
	},
	CodePrivateToPublicSink: {
		Code: CodePrivateToPublicSink, Name: "Private content reached a public sink",
		Description: "A private or user-identity segment entered a public sink.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginUser, observation.OriginToolResult, observation.OriginAssistant},
	},
	CodeCapabilityEscalation: {
		Code: CodeCapabilityEscalation, Name: "Capability escalation",
		Description: "A delegated capability was not a subset of the delegating capability.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginEnvironment},
	},
	CodeConstraintLost: {
		Code: CodeConstraintLost, Name: "Constraint lost in compaction",
		Description: "A pinned constraint did not survive context compaction.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginEnvironment},
	},
	CodeMemoryAuthorityCollapse: {
		Code: CodeMemoryAuthorityCollapse, Name: "Memory authority collapse",
		Description: "A memory entry has lower authority than the segment it was derived from.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginEnvironment},
	},
	CodeRollbackMismatch: {
		Code: CodeRollbackMismatch, Name: "Rollback state mismatch",
		Description: "A rollback left inference cache state opaque or bound to another session.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginEnvironment},
	},
	CodeApprovalReplay: {
		Code: CodeApprovalReplay, Name: "Approval replay",
		Description: "One approval or invocation was executed more than once.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginEnvironment},
	},
	CodeCaptureGap: {
		Code: CodeCaptureGap, Name: "Capture gap",
		Description: "Observed boundaries are below the configured coverage threshold.",
		Severity:    SeverityMedium, Origins: []observation.Origin{observation.OriginEnvironment},
	},
	CodeUnattributedAction: {
		Code: CodeUnattributedAction, Name: "Unattributed action",
		Description: "An action has no path to input segments or to an authorization decision.",
		Severity:    SeverityMedium, Origins: []observation.Origin{observation.OriginAssistant},
	},
	CodeProvenanceAuthMismatch: {
		Code: CodeProvenanceAuthMismatch, Name: "Provenance authorization mismatch",
		Description: "Provenance exists for a value but no authorization path permits its use.",
		Severity:    SeverityHigh, Origins: []observation.Origin{observation.OriginAssistant},
	},
	CodeInvalidToolArguments: {
		Code: CodeInvalidToolArguments, Name: "Malformed arguments",
		Description: "Tool arguments are missing, are not JSON, are not an object, or carry trailing data.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeMissingRequiredArgument: {
		Code: CodeMissingRequiredArgument, Name: "Missing required argument",
		Description: "A required property of the declared tool schema is absent.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeArgumentTypeMismatch: {
		Code: CodeArgumentTypeMismatch, Name: "Wrong argument type",
		Description: "A property does not match its declared JSON type or no alternative matched.",
		Severity:    SeverityMedium,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeArgumentConstraintViolation: {
		Code: CodeArgumentConstraintViolation, Name: "Constraint violation",
		Description: "A value breaks an enum, range, pattern, length or bounds constraint.",
		Severity:    SeverityMedium,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeUndeclaredTool: {
		Code: CodeUndeclaredTool, Name: "Undeclared tool",
		Description: "The model called a tool that the request did not declare.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeToolChoiceViolation: {
		Code: CodeToolChoiceViolation, Name: "Tool choice ignored",
		Description: "The call contradicts tool_choice or the allowed tool list.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeUnmatchedToolResult: {
		Code: CodeUnmatchedToolResult, Name: "Unmatched tool result",
		Description: "A tool result refers to a call this trajectory never made.",
		Severity:    SeverityMedium,
		Origins:     []observation.Origin{observation.OriginToolResult},
	},
	CodePromptInjection: {
		Code: CodePromptInjection, Name: "Prompt injection",
		Description: "Text contains instructions aimed at the agent rather than at the task.",
		Severity:    SeverityMedium,
		Origins: []observation.Origin{
			observation.OriginSystem, observation.OriginDeveloper, observation.OriginUser,
			observation.OriginToolResult, observation.OriginToolDescription,
		},
	},
	CodeHiddenInstructions: {
		Code: CodeHiddenInstructions, Name: "Hidden instructions",
		Description: "Invisible or bidirectional Unicode can conceal instructions.",
		Severity:    SeverityHigh,
		Origins: []observation.Origin{
			observation.OriginSystem, observation.OriginDeveloper, observation.OriginUser,
			observation.OriginAssistant, observation.OriginToolResult, observation.OriginToolDescription,
		},
	},
	CodeDangerousCommand: {
		Code: CodeDangerousCommand, Name: "Dangerous command",
		Description: "A command can destroy data, escalate privileges or fetch and execute code.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant, observation.OriginToolResult},
	},
	CodeSensitivePath: {
		Code: CodeSensitivePath, Name: "Sensitive path",
		Description: "A file operation targets credentials, keys or system configuration.",
		Severity:    SeverityMedium,
		Origins:     []observation.Origin{observation.OriginAssistant, observation.OriginToolResult},
	},
	CodePrivateNetworkTarget: {
		Code: CodePrivateNetworkTarget, Name: "Private network target",
		Description: "A tool call reaches a loopback, private, link-local or metadata address.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant, observation.OriginToolResult},
	},
	CodeCredentialExposure: {
		Code: CodeCredentialExposure, Name: "Credential exposure",
		Description: "A credential-like value appears in tool arguments or tool output.",
		Severity:    SeverityMedium,
		Origins:     []observation.Origin{observation.OriginAssistant, observation.OriginToolResult},
	},
	CodeExfiltrationLink: {
		Code: CodeExfiltrationLink, Name: "Possible exfiltration link",
		Description: "A URL carries data or points at a private, credential-bearing or tracking destination.",
		Severity:    SeverityHigh,
		Origins: []observation.Origin{
			observation.OriginSystem, observation.OriginDeveloper, observation.OriginUser,
			observation.OriginAssistant, observation.OriginToolResult, observation.OriginToolDescription,
		},
	},
	CodeUntrustedValueReuse: {
		Code: CodeUntrustedValueReuse, Name: "Untrusted value reused",
		Description: "A value produced by external content is passed into a tool that can act or send.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginToolResult, observation.OriginToolDescription},
	},
	CodeExternalInstructionFollowed: {
		Code: CodeExternalInstructionFollowed, Name: "External instruction followed",
		Description: "A tool call follows an instruction that came from external content.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginToolResult, observation.OriginToolDescription},
	},
	CodeSecretFlow: {
		Code: CodeSecretFlow, Name: "Secret sent to a sink",
		Description: "A redaction placeholder for a secret reaches a tool that can send or expose it.",
		Severity:    SeverityHigh,
		Origins:     []observation.Origin{observation.OriginAssistant},
	},
	CodeAnalysisIncomplete: {
		Code: CodeAnalysisIncomplete, Name: "Analysis incomplete",
		Description: "A detector failed, timed out or the finding cap was reached.",
		Severity:    SeverityInfo,
		Origins:     []observation.Origin{observation.OriginEnvironment},
	},
}

func CodeDefinitionFor(code string) (CodeDefinition, bool) {
	definition, found := CodeCatalogue[code]
	return definition, found
}

// AxisFor derives the three-axis label of a deterministic finding. Origin decides where the risk
// entered, the code decides the mechanism, and the sink decides the plausible harm. An empty axis
// is left empty: the deterministic layer does not guess at dimensions it cannot see.
func AxisFor(code string, origin observation.Origin, sink observation.Sink) Axes {
	if code == CodeAnalysisIncomplete {
		return Axes{}
	}
	axes := Axes{Source: sourceFor(code, origin), Mode: modeFor(code, sink)}
	switch code {
	case CodeInvalidToolArguments, CodeMissingRequiredArgument, CodeArgumentTypeMismatch, CodeArgumentConstraintViolation:
		axes.Harm = HarmOpportunity
	case CodeUndeclaredTool, CodeToolChoiceViolation, CodeUnmatchedToolResult:
		axes.Harm = HarmSystemIntegrity
	case CodePromptInjection, CodeHiddenInstructions:
		axes.Harm = HarmInformation
	case CodeDangerousCommand:
		axes.Harm = HarmSystemIntegrity
	case CodeSensitivePath, CodeCredentialExposure, CodeExfiltrationLink, CodeSecretFlow:
		axes.Harm = HarmPrivacy
	case CodePrivateNetworkTarget:
		axes.Harm = HarmSystemIntegrity
	case CodeUntrustedValueReuse, CodeExternalInstructionFollowed:
		axes.Harm = harmForSink(sink)
	case CodeUntrustedToExternalSend, CodeProvenanceAuthMismatch:
		axes.Harm = HarmPrivacy
	case CodePrivateToPublicSink:
		axes.Harm = HarmPrivacy
	case CodeCapabilityEscalation:
		axes.Harm = HarmSystemIntegrity
	case CodeConstraintLost:
		axes.Harm = HarmOpportunity
	case CodeMemoryAuthorityCollapse:
		axes.Harm = HarmPrivacy
	case CodeRollbackMismatch, CodeApprovalReplay:
		axes.Harm = HarmSystemIntegrity
	case CodeCaptureGap, CodeUnattributedAction:
		axes.Harm = HarmSystemIntegrity
	}
	return axes
}

func sourceFor(code string, origin observation.Origin) RiskSource {
	switch code {
	case CodePromptInjection, CodeHiddenInstructions:
		switch origin {
		case observation.OriginToolResult:
			return SourceIndirectPromptInjection
		case observation.OriginToolDescription:
			return SourceToolDescriptionInjection
		case observation.OriginUser:
			return SourceDirectPromptInjection
		case observation.OriginSystem, observation.OriginDeveloper:
			return SourceMaliciousUserInstruction
		}
		return SourceInherentAgentFailure
	case CodeUntrustedValueReuse, CodeExternalInstructionFollowed:
		switch origin {
		case observation.OriginToolDescription:
			return SourceToolDescriptionInjection
		case observation.OriginToolResult:
			return SourceIndirectPromptInjection
		}
		return SourceInherentAgentFailure
	}
	return SourceInherentAgentFailure
}

func modeFor(code string, sink observation.Sink) FailureMode {
	switch code {
	case CodeInvalidToolArguments, CodeMissingRequiredArgument, CodeArgumentTypeMismatch, CodeArgumentConstraintViolation:
		return ModeIncorrectToolParameters
	case CodeUndeclaredTool:
		return ModeChoosingMaliciousTool
	case CodeToolChoiceViolation:
		return ModeToolMisuse
	case CodeUnmatchedToolResult:
		return ModeProceduralDeviation
	case CodePromptInjection, CodeHiddenInstructions:
		return ModeFlawedPlanning
	case CodeDangerousCommand:
		return ModeInsecureInteraction
	case CodeSensitivePath, CodePrivateNetworkTarget:
		return ModeUnconfirmedAction
	case CodeCredentialExposure, CodeExfiltrationLink, CodeSecretFlow:
		return ModeInformationDisclosure
	case CodeUntrustedValueReuse, CodeUntrustedToExternalSend, CodeProvenanceAuthMismatch, CodeMemoryAuthorityCollapse:
		return ModeUnvalidatedToolOutput
	case CodePrivateToPublicSink:
		return ModeInformationDisclosure
	case CodeCapabilityEscalation:
		return ModeUnconfirmedAction
	case CodeConstraintLost:
		return ModeFlawedPlanning
	case CodeRollbackMismatch, CodeCaptureGap, CodeUnattributedAction:
		return ModeProceduralDeviation
	case CodeApprovalReplay:
		return ModeToolMisuse
	case CodeExternalInstructionFollowed:
		if sink == observation.SinkShellExec || sink == observation.SinkFileWrite || sink == observation.SinkExternalSend {
			return ModeInsecureInteraction
		}
		return ModeUnvalidatedToolOutput
	}
	return ""
}

func harmForSink(sink observation.Sink) Harm {
	switch sink {
	case observation.SinkExternalSend, observation.SinkFileRead, observation.SinkCredentialAccess:
		return HarmPrivacy
	case observation.SinkShellExec, observation.SinkFileWrite:
		return HarmSystemIntegrity
	case observation.SinkPayment:
		return HarmFinancial
	}
	return ""
}
