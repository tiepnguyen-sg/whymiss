package main

import (
	"strings"
	"testing"
)

func TestCheckReleasePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		results []result
		wantErr bool
	}{
		{name: "empty", wantErr: true},
		{name: "fewer than fifty", results: policyResults(44, 5, false), wantErr: true},
		{name: "ninety percent", results: policyResults(45, 5, false), wantErr: false},
		{name: "below ninety percent", results: policyResults(44, 6, false), wantErr: true},
		{name: "false high", results: policyResults(45, 5, true), wantErr: true},
		{name: "no ambiguous case", results: withoutAmbiguous(policyResults(45, 5, false)), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := checkReleasePolicy(tc.results); (err != nil) != tc.wantErr {
				t.Fatalf("checkReleasePolicy() = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func policyResults(correct, wrong int, falseHigh bool) []result {
	results := make([]result, correct+wrong)
	for i := range correct {
		results[i] = result{want: "local.cl_slow", got: "local.cl_slow", correct: true}
	}
	results[0].want = "unknown.insufficient_data"
	results[0].got = "unknown.insufficient_data"
	if wrong > 0 {
		results[correct].falseHigh = falseHigh
	}
	return results
}

func withoutAmbiguous(results []result) []result {
	for i := range results {
		results[i].want = "local.cl_slow"
	}
	return results
}

// TestBuildReportStatesItsOwnLimits pins the two facts that stop a high
// accuracy figure from reading as "the engine is finished": how far the corpus
// is from the release minimum, and that dropping an unreproducible scenario
// raises the percentage without improving anything. Both were added after a
// corpus of 15 reported 86.7% with two mislabelled scenarios, and removing
// those two turned the same engine into 100%.
func TestBuildReportStatesItsOwnLimits(t *testing.T) {
	t.Parallel()
	report := buildReport(policyResults(13, 0, false))

	for _, want := range []string{
		"**Corpus size:** 13 of the 50 scenarios the release gate requires",
		"**Causes exercised:** 2",
		"because evidence was withdrawn, not because the engine improved",
		// A corpus can reach a high top-1 figure while mostly asserting that
		// the engine declines to answer. The split has to be on the page.
		"**Expecting `unknown.*`:**",
		"assert that attribution is correctly refused",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q\n---\n%s", want, report)
		}
	}
}
