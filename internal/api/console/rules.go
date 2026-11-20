package console

import (
	"cmp"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/yv1ing/SafeAgent/internal/analysis"
	"github.com/yv1ing/SafeAgent/internal/redact"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type RuleView struct {
	ID       string `json:"id" required:"true"`
	Kind     string `json:"kind" required:"true" enum:"builtin,regex"`
	Detector string `json:"detector" required:"true"`
	Pattern  string `json:"pattern" required:"true"`
	Capture  string `json:"capture" required:"true"`
	Label    string `json:"label" required:"true"`
	Action   string `json:"action" required:"true" enum:"replace,block,alert"`
	Enabled  bool   `json:"enabled" required:"true"`
	Version  int64  `json:"version" required:"true"`
}

type RuleInput struct {
	Kind     string `json:"kind" required:"true" enum:"builtin,regex"`
	Detector string `json:"detector" description:"Detector id; builtin rules only"`
	Pattern  string `json:"pattern" description:"RE2 expression; regex rules only"`
	Capture  string `json:"capture" description:"Named group to replace instead of the whole match"`
	Label    string `json:"label"`
	Action   string `json:"action" required:"true" enum:"replace,block,alert"`
	Enabled  bool   `json:"enabled"`
	Version  int64  `json:"version" description:"Required on update, ignored on create"`
}

type RuleTestInput struct {
	Rule RuleInput `json:"rule" required:"true"`
	Text string    `json:"text" required:"true" maxLength:"65536"`
}

type RuleTestView struct {
	Replaced string    `json:"replaced" required:"true"`
	Blocked  bool      `json:"blocked" required:"true"`
	Hits     []HitView `json:"hits" required:"true"`
}

type HitView struct {
	Label  string `json:"label" required:"true"`
	Action string `json:"action" required:"true"`
	Path   string `json:"path" required:"true"`
	Start  int    `json:"start" required:"true"`
	End    int    `json:"end" required:"true"`
}

type DetectorView struct {
	ID      string `json:"id" required:"true"`
	Label   string `json:"label" required:"true"`
	Default bool   `json:"default" required:"true" description:"Enabled in new projects"`
	Secret  bool   `json:"secret" required:"true" description:"Matches credentials rather than personal data"`
}

type FindingCodeView struct {
	Code        string   `json:"code" required:"true"`
	Name        string   `json:"name" required:"true"`
	Description string   `json:"description" required:"true"`
	Severity    string   `json:"severity" required:"true" enum:"info,medium,high"`
	Origins     []string `json:"origins" required:"true"`
}

type rulePath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
	RuleID    string `path:"rule_id" json:"-" required:"true"`
}

type createRuleInput struct {
	projectPath
	RuleInput
}

type updateRuleInput struct {
	rulePath
	RuleInput
}

type testRuleInput struct {
	projectPath
	RuleTestInput
}

const maximumSampleBytes = 64 << 10

func ruleView(value *entity.Rule) RuleView {
	return RuleView{ID: value.ID, Kind: value.Kind, Detector: value.Detector, Pattern: value.Pattern, Capture: value.Capture, Label: value.Label, Action: value.Action, Enabled: value.Enabled, Version: value.Version}
}

func (input RuleInput) service() service.RuleInput {
	return service.RuleInput{Kind: input.Kind, Detector: input.Detector, Pattern: input.Pattern, Capture: input.Capture, Label: input.Label, Action: input.Action, Enabled: input.Enabled, Version: input.Version}
}

func hitView(value *redact.Hit) HitView {
	return HitView{Label: value.Label, Action: string(value.Action), Path: value.Path, Start: value.Start, End: value.End}
}

func detectorView(value *redact.Detector) DetectorView {
	return DetectorView{ID: value.ID, Label: value.Label, Default: value.Default, Secret: value.Secret}
}

func findingCodes() []FindingCodeView {
	codes := make([]FindingCodeView, 0, len(analysis.CodeCatalogue))
	for _, definition := range analysis.CodeCatalogue {
		origins := make([]string, 0, len(definition.Origins))
		for _, origin := range definition.Origins {
			origins = append(origins, string(origin))
		}
		codes = append(codes, FindingCodeView{
			Code: definition.Code, Name: definition.Name,
			Description: definition.Description, Severity: definition.Severity, Origins: origins,
		})
	}
	slices.SortFunc(codes, func(left, right FindingCodeView) int {
		if rank := analysis.SeverityRank(right.Severity) - analysis.SeverityRank(left.Severity); rank != 0 {
			return rank
		}
		return cmp.Compare(left.Code, right.Code)
	})
	return codes
}

func (console *Console) registerRules() {
	console.add("GET", "/detectors", "listDetectors", nil, []DetectorView{}, func(*gin.Context, *Identity) (any, error) {
		return viewAll(console.Rule.Detectors(), detectorView), nil
	})
	console.add("GET", "/finding-codes", "listFindingCodes", nil, []FindingCodeView{}, func(*gin.Context, *Identity) (any, error) {
		return findingCodes(), nil
	})
	console.add("GET", "/projects/{project_id}/rules", "listRules", projectPath{}, []RuleView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		rules, err := console.Rule.List(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(rules, ruleView), nil
	})
	console.add("POST", "/projects/{project_id}/rules", "createRule", createRuleInput{}, RuleView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input RuleInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Rule.Create(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), input.service())
		if err != nil {
			return nil, err
		}
		return ruleView(created), nil
	})
	console.add("PUT", "/projects/{project_id}/rules/{rule_id}", "updateRule", updateRuleInput{}, RuleView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input RuleInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Rule.Update(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("rule_id"), input.service())
		if err != nil {
			return nil, err
		}
		return ruleView(updated), nil
	})
	console.add("DELETE", "/projects/{project_id}/rules/{rule_id}", "deleteRule", rulePath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Rule.Delete(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), ctx.Param("rule_id"))
		return nil, err
	})
	console.add("POST", "/projects/{project_id}/rules/test", "testRule", testRuleInput{}, RuleTestView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input RuleTestInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		if len(input.Text) > maximumSampleBytes {
			return nil, errInvalidRequest
		}
		if !console.ruleLimit.Allow(identity.User.ID) {
			return nil, errRateLimited
		}
		result, err := console.Rule.Test(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"), input.Rule.service(), input.Text)
		if err != nil {
			return nil, err
		}
		return RuleTestView{Replaced: result.Replaced, Blocked: result.Blocked, Hits: viewAll(result.Hits, hitView)}, nil
	})
}
