package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/analysis/policy"
)

func TestMembersMustPreserveBlockingRules(t *testing.T) {
	block := policy.Rule{
		ID: "block", Name: "block", Enabled: true, Priority: 100, Phase: policy.PhaseToolCall,
		When: "true", Action: policy.ActionBlock, Severity: "high",
	}
	active := policy.Document{Rules: []policy.Rule{block}}
	if err := memberDocument(active, policy.Document{Rules: []policy.Rule{block}}); err != nil {
		t.Fatalf("an unchanged block rule must be allowed: %v", err)
	}
	if err := memberDocument(active, policy.Document{}); err == nil {
		t.Fatal("a member removed an administrator's block rule")
	}
	changed := block
	changed.When = "false"
	if err := memberDocument(active, policy.Document{Rules: []policy.Rule{changed}}); err == nil {
		t.Fatal("a member changed an administrator's block rule")
	}
	added := policy.Rule{ID: "new", Enabled: true, Phase: policy.PhaseRequest, When: "true", Action: policy.ActionBlock}
	if err := memberDocument(active, policy.Document{Rules: []policy.Rule{block, added}}); err == nil {
		t.Fatal("a member added a block rule")
	}
	record := policy.Rule{ID: "record", Enabled: true, Phase: policy.PhaseRequest, When: "true", Action: policy.ActionRecord}
	if err := memberDocument(active, policy.Document{Rules: []policy.Rule{block, record}}); err != nil {
		t.Fatalf("a member may add a record rule next to the block rule: %v", err)
	}
}
