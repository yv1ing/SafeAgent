package model

type LossCode string

const (
	LossFieldDropped           LossCode = "field_dropped"
	LossExtrasDropped          LossCode = "extras_dropped"
	LossSignatureStripped      LossCode = "signature_stripped"
	LossSignatureUnverified    LossCode = "signature_unverified"
	LossRoleMapped             LossCode = "role_mapped"
	LossCitationMapped         LossCode = "citation_mapped"
	LossGeminiDummySignature   LossCode = "gemini_dummy_signature"
	LossToolKindMapped         LossCode = "tool_kind_mapped"
	LossDefaultTokenLimit      LossCode = "default_token_limit"
	LossFinishMapped           LossCode = "finish_mapped"
	LossCleanEOF               LossCode = "clean_eof_synthesized"
	LossMediaMapped            LossCode = "media_mapped"
	LossReasoningNotReplayable LossCode = "reasoning_not_replayable"
	LossServerToolMapped       LossCode = "server_tool_mapped"
	LossItemIDMapped           LossCode = "item_id_synthesized"
	LossCapabilityRemoved      LossCode = "capability_field_removed"
	LossMultiChoicePassthrough LossCode = "multi_choice_requires_passthrough"
)

// LossClass separates a conversion that cannot preserve core semantics (fatal, rejected before
// the upstream is called) from one that only drops private or secondary data (degraded, recorded
// and continued).
type LossClass string

const (
	LossFatal    LossClass = "fatal"
	LossDegraded LossClass = "degraded"
)

type Loss struct {
	Path    string    `json:"path"`
	Code    LossCode  `json:"code"`
	Class   LossClass `json:"class,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	From    Format    `json:"from,omitempty"`
	To      Format    `json:"to,omitempty"`
	ItemKey string    `json:"item_key,omitempty"`
}

// Fatal reports whether the loss forbids forwarding the request.
func (loss Loss) Fatal() bool { return loss.Class == LossFatal }

type Repair struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Detail  string `json:"detail,omitempty"`
	ItemKey string `json:"item_key,omitempty"`
}

type Ledger struct {
	Losses  []Loss   `json:"losses,omitempty"`
	Repairs []Repair `json:"repairs,omitempty"`
}
