package threat

// TaxonomyVersion identifies the three-axis catalogue adapted from AgentDoG. It is recorded with
// every detector pass, so labels stay comparable only under the same version.
const TaxonomyVersion = "agentdog-2026.1"

// RiskSource is the origin axis: where the unsafe influence entered the trajectory.
type RiskSource string

const (
	SourceMaliciousUserInstruction RiskSource = "malicious_user_instruction_or_jailbreak"
	SourceDirectPromptInjection    RiskSource = "direct_prompt_injection"
	SourceIndirectPromptInjection  RiskSource = "indirect_prompt_injection"
	SourceMisinformation           RiskSource = "unreliable_or_misinformation"
	SourceToolDescriptionInjection RiskSource = "tool_description_injection"
	SourceMaliciousToolExecution   RiskSource = "malicious_tool_execution"
	SourceCorruptedToolFeedback    RiskSource = "corrupted_tool_feedback"
	SourceInherentAgentFailure     RiskSource = "inherent_agent_failures"
)

// FailureMode is the mechanism axis: what the agent did that produced the risk.
type FailureMode string

const (
	ModeUnconfirmedAction       FailureMode = "unconfirmed_or_over_privileged_action"
	ModeFlawedPlanning          FailureMode = "flawed_planning_or_reasoning"
	ModeIncorrectToolParameters FailureMode = "incorrect_tool_parameters"
	ModeChoosingMaliciousTool   FailureMode = "choosing_malicious_tool"
	ModeToolMisuse              FailureMode = "tool_misuse_in_specific_context"
	ModeUnvalidatedToolOutput   FailureMode = "failure_to_validate_tool_outputs"
	ModeInsecureInteraction     FailureMode = "insecure_interaction_or_execution"
	ModeProceduralDeviation     FailureMode = "procedural_deviation_or_inaction"
	ModeInefficientExecution    FailureMode = "inefficient_or_wasteful_execution"
	ModeHarmfulOffensiveContent FailureMode = "generation_of_harmful_offensive_content"
	ModeHarmfulIllegalAdvice    FailureMode = "instruction_for_harmful_illegal_activity"
	ModeMaliciousExecutables    FailureMode = "generation_of_malicious_executables"
	ModeInformationDisclosure   FailureMode = "unauthorized_information_disclosure"
	ModeInaccurateInformation   FailureMode = "provide_inaccurate_misleading_or_unverified_information"
)

// Harm is the impact axis: the real-world consequence of the trajectory.
type Harm string

const (
	HarmPrivacy         Harm = "privacy_and_confidentiality_harm"
	HarmFinancial       Harm = "financial_and_economic_harm"
	HarmSystemIntegrity Harm = "security_and_system_integrity_harm"
	HarmPhysical        Harm = "physical_and_health_harm"
	HarmPsychological   Harm = "psychological_and_emotional_harm"
	HarmReputational    Harm = "reputational_and_interpersonal_harm"
	HarmInformation     Harm = "info_ecosystem_and_societal_harm"
	HarmPublicService   Harm = "public_service_and_resource_harm"
	HarmFairness        Harm = "fairness_equity_and_allocative_harm"
	HarmOpportunity     Harm = "functional_and_opportunity_harm"
)

// Axes is the multi-axis label of a finding. An empty axis means the deterministic logic could not
// attribute that dimension; it is never a claim that the dimension is harmless.
type Axes struct {
	Source RiskSource  `json:"source,omitempty"`
	Mode   FailureMode `json:"mode,omitempty"`
	Harm   Harm        `json:"harm,omitempty"`
}

func (axes Axes) Empty() bool { return axes.Source == "" && axes.Mode == "" && axes.Harm == "" }

var RiskSources = []RiskSource{
	SourceMaliciousUserInstruction, SourceDirectPromptInjection, SourceIndirectPromptInjection,
	SourceMisinformation, SourceToolDescriptionInjection, SourceMaliciousToolExecution,
	SourceCorruptedToolFeedback, SourceInherentAgentFailure,
}

var FailureModes = []FailureMode{
	ModeUnconfirmedAction, ModeFlawedPlanning, ModeIncorrectToolParameters, ModeChoosingMaliciousTool,
	ModeToolMisuse, ModeUnvalidatedToolOutput, ModeInsecureInteraction, ModeProceduralDeviation,
	ModeInefficientExecution, ModeHarmfulOffensiveContent, ModeHarmfulIllegalAdvice,
	ModeMaliciousExecutables, ModeInformationDisclosure, ModeInaccurateInformation,
}

var Harms = []Harm{
	HarmPrivacy, HarmFinancial, HarmSystemIntegrity, HarmPhysical, HarmPsychological,
	HarmReputational, HarmInformation, HarmPublicService, HarmFairness, HarmOpportunity,
}

func ValidRiskSource(value RiskSource) bool {
	for _, candidate := range RiskSources {
		if candidate == value {
			return true
		}
	}
	return false
}

func ValidFailureMode(value FailureMode) bool {
	for _, candidate := range FailureModes {
		if candidate == value {
			return true
		}
	}
	return false
}

func ValidHarm(value Harm) bool {
	for _, candidate := range Harms {
		if candidate == value {
			return true
		}
	}
	return false
}
