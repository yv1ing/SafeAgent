package main

import (
	"strings"
	"testing"
)

func TestFindingGateRejectsInvalidReportsAndDeduplicatesReachability(t *testing.T) {
	for _, report := range []string{"", `{`, `{ "progress": {} }`} {
		if _, err := findings(strings.NewReader(report)); err == nil {
			t.Fatal("accepted invalid report", report)
		}
	}
	report := `{"config":{}} {"finding":{"osv":"GO-TEST","trace":[{"function":"Call"}]}} {"finding":{"osv":"GO-TEST","trace":[{"function":"Call"}]}} {"finding":{"osv":"GO-MODULE","trace":[{}]}}`
	identifiers, err := findings(strings.NewReader(report))
	if err != nil || len(identifiers) != 1 || identifiers[0] != "GO-TEST" {
		t.Fatal(identifiers, err)
	}
}
