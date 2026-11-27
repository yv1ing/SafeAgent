package console

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/observation"
	"github.com/yv1ing/SafeAgent/internal/policy"
	access "github.com/yv1ing/SafeAgent/internal/service/access"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/threat"
)

type PolicyVersionView struct {
	ID        string          `json:"id" required:"true"`
	Version   int64           `json:"version" required:"true"`
	Document  json.RawMessage `json:"document" required:"true"`
	Hash      string          `json:"hash" required:"true"`
	Note      string          `json:"note" required:"true"`
	CreatedBy string          `json:"created_by" required:"true"`
	CreatedAt string          `json:"created_at" required:"true"`
}

type PolicyPage struct {
	Items []PolicyVersionView `json:"items" required:"true"`
}

type PolicySaveInput struct {
	Document json.RawMessage `json:"document" required:"true"`
	Note     string          `json:"note"`
}

type PolicyValidateInput struct {
	Document json.RawMessage `json:"document" required:"true"`
}

type PolicyValidateView struct {
	Valid bool `json:"valid" required:"true"`
}

type PolicyTestInput struct {
	Document json.RawMessage `json:"document" required:"true"`
	Trace    json.RawMessage `json:"trace" required:"true" description:"An observation.Trace object"`
}

type PolicyTestView struct {
	Findings    []FindingView        `json:"findings" required:"true"`
	Decisions   []PolicyDecisionView `json:"decisions" required:"true"`
	Verdict     string               `json:"verdict" required:"true"`
	MaxSeverity string               `json:"max_severity" required:"true"`
}

type PolicyDecisionView struct {
	RuleID   string `json:"rule_id" required:"true"`
	RuleName string `json:"rule_name" required:"true"`
	Action   string `json:"action" required:"true" enum:"block,record"`
	Severity string `json:"severity" required:"true"`
	Reason   string `json:"reason" required:"true"`
}

type policyVersionPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	Version   int64  `path:"version" json:"-" required:"true"`
}

type savePolicyInput struct {
	projectPath
	PolicySaveInput
}

type validatePolicyInput struct {
	projectPath
	PolicyValidateInput
}

type testPolicyInput struct {
	projectPath
	PolicyTestInput
}

func policyVersionView(value *service.PolicyVersion) PolicyVersionView {
	encoded, _ := json.Marshal(value.Document)
	return PolicyVersionView{
		ID: value.ID, Version: value.Version, Document: encoded, Hash: value.Hash,
		Note: value.Note, CreatedBy: value.CreatedBy, CreatedAt: stamp(value.CreatedAt),
	}
}

func (console *Console) registerPolicies() {
	console.add("GET", "/projects/{project_id}/policies", "listPolicies", projectPath{}, PolicyPage{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		versions, err := console.Policy.List(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		items := make([]PolicyVersionView, 0, len(versions))
		for index := range versions {
			items = append(items, policyVersionView(&versions[index]))
		}
		return PolicyPage{Items: items}, nil
	})
	console.add("GET", "/projects/{project_id}/policies/active", "getActivePolicy", projectPath{}, PolicyVersionView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		version, err := console.Policy.Active(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return policyVersionView(&version), nil
	})
	console.add("POST", "/projects/{project_id}/policies", "savePolicy", savePolicyInput{}, PolicyVersionView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input PolicySaveInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		var document policy.Document
		if err := json.Unmarshal(input.Document, &document); err != nil {
			return nil, errInvalidRequest
		}
		version, err := console.Policy.Save(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), document, input.Note)
		if err != nil {
			return nil, err
		}
		return policyVersionView(&version), nil
	})
	console.add("POST", "/projects/{project_id}/policies/{version}/rollback", "rollbackPolicy", policyVersionPath{}, PolicyVersionView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		version, err := pathInt(ctx.Param("version"))
		if err != nil {
			return nil, errInvalidRequest
		}
		saved, err := console.Policy.Rollback(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), version)
		if err != nil {
			return nil, err
		}
		return policyVersionView(&saved), nil
	})
	console.add("POST", "/projects/{project_id}/policies/validate", "validatePolicy", validatePolicyInput{}, PolicyValidateView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input PolicyValidateInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		var document policy.Document
		if err := json.Unmarshal(input.Document, &document); err != nil {
			return nil, errInvalidRequest
		}
		if err := console.Policy.Validate(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), document); err != nil {
			var failure *access.Failure
			if errors.As(err, &failure) && failure.Kind == access.KindInvalid {
				return PolicyValidateView{Valid: false}, nil
			}
			return nil, err
		}
		return PolicyValidateView{Valid: true}, nil
	})
	console.add("POST", "/projects/{project_id}/policies/test", "testPolicy", testPolicyInput{}, PolicyTestView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		if !console.ruleLimit.Allow("policy:" + identity.User.ID) {
			return nil, errRateLimited
		}
		var input PolicyTestInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		var document policy.Document
		if err := json.Unmarshal(input.Document, &document); err != nil {
			return nil, errInvalidRequest
		}
		var trace observation.Trace
		if err := json.Unmarshal(input.Trace, &trace); err != nil {
			return nil, errInvalidRequest
		}
		result, err := console.Policy.Test(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), document, &trace)
		if err != nil {
			return nil, err
		}
		findings := make([]FindingView, 0, len(result.Findings))
		for index := range result.Findings {
			findings = append(findings, threatFindingView(result.Findings[index]))
		}
		decisions := make([]PolicyDecisionView, 0, len(result.Decisions))
		for _, decision := range result.Decisions {
			decisions = append(decisions, PolicyDecisionView{RuleID: decision.RuleID, RuleName: decision.Name, Action: decision.Action, Severity: decision.Severity, Reason: decision.Reason})
		}
		summary := threat.Summarize(result.Findings)
		return PolicyTestView{Findings: findings, Decisions: decisions, Verdict: result.Verdict.Value, MaxSeverity: summary.MaxSeverity}, nil
	})
}
