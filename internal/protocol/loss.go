package protocol

type LossCode string

const (
	LossFieldDropped         LossCode = "field_dropped"
	LossExtrasDropped        LossCode = "extras_dropped"
	LossSignatureStripped    LossCode = "signature_stripped"
	LossSignatureUnverified  LossCode = "signature_unverified"
	LossRoleMapped           LossCode = "role_mapped"
	LossCitationDropped      LossCode = "citation_dropped"
	LossGeminiDummySignature LossCode = "gemini_dummy_signature"
	LossToolKindMapped       LossCode = "tool_kind_mapped"
	LossDefaultTokenLimit    LossCode = "default_token_limit"
	LossFinishMapped         LossCode = "finish_mapped"
	LossCleanEOF             LossCode = "clean_eof_synthesized"
)

type Loss struct {
	Path   string   `json:"path"`
	Code   LossCode `json:"code"`
	Detail string   `json:"detail,omitempty"`
	From   Format   `json:"from,omitempty"`
	To     Format   `json:"to,omitempty"`
}

type FailureClass string

const (
	ClassCancelled FailureClass = "cancelled"
	ClassPolicy    FailureClass = "policy"
	ClassInvalid   FailureClass = "invalid_request"
	ClassTransient FailureClass = "upstream_transient"
	ClassRejected  FailureClass = "upstream_rejected"
	ClassAuth      FailureClass = "upstream_auth"
	ClassRateLimit FailureClass = "rate_limited"
	ClassNoChannel FailureClass = "no_channel"
	ClassInternal  FailureClass = "internal"
)

type Error struct {
	Kind    string       `json:"kind" required:"true" enum:"invalid_request,unsupported,unauthorized,rate_limited,upstream_failure,timeout,blocked,internal"`
	Class   FailureClass `json:"class,omitempty"`
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Status  int          `json:"status,omitempty"`
}

func (failure *Error) Error() string {
	if failure.Message == "" {
		return failure.Code
	}
	return failure.Message
}
