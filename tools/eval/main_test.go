package main

import "testing"

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
