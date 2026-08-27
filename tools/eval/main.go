// Command eval measures internal/rca's accuracy against every scenario in
// test/corpus: for each, it replays the recorded observations, runs
// rca.Analyze, and compares the result against manifest.yaml's expect:
// block. It prints a Markdown report to stdout — `make eval` redirects
// that into docs/evaluation.md.
//
// Normal mode is a measurement and reports whatever the corpus actually shows.
// --check additionally enforces the release policy from BUILD_PROMPT.md §11.3.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/timeline"
)

// manifest mirrors tools/corpusctl's own type — see its doc comment for why
// this is duplicated rather than imported (two independent package main
// binaries).
type manifest struct {
	ID     string `yaml:"id"`
	Expect struct {
		Cause      string `yaml:"cause"`
		SubCause   string `yaml:"sub_cause,omitempty"`
		Confidence string `yaml:"confidence"`
	} `yaml:"expect"`
}

// result is one scenario's measured outcome.
type result struct {
	id, want, got                 string
	wantConfidence, gotConfidence string
	correct                       bool
	falseHigh                     bool
}

const (
	releaseAccuracyPercent = 90
	releaseMinScenarios    = 50
)

func main() {
	flags := flag.NewFlagSet("eval", flag.ContinueOnError)
	check := flags.Bool("check", false, "enforce release accuracy and false-high gates")
	if err := flags.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: eval [--check] <corpus-dir>")
		os.Exit(2)
	}
	if err := run(flags.Arg(0), os.Stdout, *check); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(corpusDir string, out io.Writer, check bool) error {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		return fmt.Errorf("read corpus dir: %w", err)
	}

	var results []result
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := evalScenario(filepath.Join(corpusDir, e.Name()))
		if err != nil {
			return fmt.Errorf("scenario %s: %w", e.Name(), err)
		}
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].id < results[j].id })

	if _, err := io.WriteString(out, buildReport(results)); err != nil {
		return err
	}
	if check {
		return checkReleasePolicy(results)
	}
	return nil
}

func checkReleasePolicy(results []result) error {
	if len(results) < releaseMinScenarios {
		return fmt.Errorf("release gate failed: corpus has %d scenarios, want at least %d", len(results), releaseMinScenarios)
	}
	correct, falseHigh, ambiguous := 0, 0, 0
	for _, r := range results {
		if r.correct {
			correct++
		}
		if r.falseHigh {
			falseHigh++
		}
		if strings.HasPrefix(r.want, "unknown.") {
			ambiguous++
		}
	}
	if ambiguous == 0 {
		return fmt.Errorf("release gate failed: corpus has no ambiguous scenario expected to yield unknown.*")
	}
	if falseHigh > 0 {
		return fmt.Errorf("release gate failed: %d false-high verdict(s), want 0", falseHigh)
	}
	if correct*100 < len(results)*releaseAccuracyPercent {
		return fmt.Errorf("release gate failed: top-1 accuracy %d/%d is below %d%%", correct, len(results), releaseAccuracyPercent)
	}
	return nil
}

func evalScenario(dir string) (result, error) {
	id := filepath.Base(dir)

	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		return result{}, fmt.Errorf("read manifest.yaml: %w", err)
	}
	var m manifest
	if err := yaml.Unmarshal(manifestBytes, &m); err != nil {
		return result{}, fmt.Errorf("parse manifest.yaml: %w", err)
	}

	obs, err := timeline.LoadObservations(filepath.Join(dir, "observations.jsonl"))
	if err != nil {
		return result{}, fmt.Errorf("load observations: %w", err)
	}
	samples, err := timeline.LoadSamples(filepath.Join(dir, "samples.jsonl"))
	if err != nil {
		return result{}, fmt.Errorf("load samples: %w", err)
	}
	tl, err := timeline.ReplayWithSamples(obs, samples, domain.MainnetPreEPBS())
	if err != nil {
		return result{}, fmt.Errorf("replay: %w", err)
	}

	v := rca.Analyze(tl, rca.DefaultConfig())

	want := m.Expect.Cause
	if m.Expect.SubCause != "" {
		want = m.Expect.SubCause
	}
	got := string(v.ReportedCause())

	return result{
		id:             id,
		want:           want,
		got:            got,
		wantConfidence: m.Expect.Confidence,
		gotConfidence:  string(v.Confidence),
		correct:        want == got,
		falseHigh:      want != got && v.Confidence == domain.ConfidenceHigh,
	}, nil
}

// percentOf returns part as a percentage of whole, and 0 for an empty corpus
// rather than NaN — a report is read by people, and "NaN%" tells them nothing.
func percentOf(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

// label tracks precision/recall inputs for one cause label: true positives
// (correctly predicted), false positives (predicted but wrong), false
// negatives (should have been predicted but wasn't).
type label struct {
	tp, fp, fn int
}

func buildReport(results []result) string {
	var b strings.Builder

	fmt.Fprintln(&b, "# RCA Evaluation Report")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Generated by `make eval` against %d corpus scenario(s).\n\n", len(results))

	correct := 0
	falseHigh := 0
	labels := map[string]*label{}
	labelOf := func(id string) *label {
		l, ok := labels[id]
		if !ok {
			l = &label{}
			labels[id] = l
		}
		return l
	}

	ambiguous := 0
	for _, r := range results {
		if r.correct {
			correct++
			labelOf(r.want).tp++
		} else {
			labelOf(r.want).fn++
			labelOf(r.got).fp++
		}
		if r.falseHigh {
			falseHigh++
		}
		if strings.HasPrefix(r.want, "unknown.") {
			ambiguous++
		}
	}

	total := len(results)
	accuracy := percentOf(correct, total)

	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- **Top-1 accuracy:** %d/%d (%.1f%%)\n", correct, total, accuracy)
	fmt.Fprintf(&b, "- **False-high verdicts:** %d (a wrong verdict reported at `high` confidence — the DoD's zero-tolerance metric)\n", falseHigh)
	fmt.Fprintf(&b, "- **Corpus size:** %d of the %d scenarios the release gate requires\n", total, releaseMinScenarios)
	fmt.Fprintf(&b, "- **Causes exercised:** %d (a cause with no scenario is unmeasured, which is not the same as passing)\n", len(labels))
	// A scenario expecting unknown.* asserts that the engine declines to
	// attribute. That is a required property (I-8) and the DoD asks for those
	// cases explicitly, but a corpus made mostly of them measures refusal far
	// more than it measures attribution — and top-1 accuracy does not
	// distinguish the two. State the split so the headline cannot be read as
	// "the engine names the right cause this often".
	fmt.Fprintf(&b, "- **Expecting `unknown.*`:** %d of %d (%.1f%%) — these assert that attribution is correctly refused, not that a cause was identified\n",
		ambiguous, total, percentOf(ambiguous, total))
	fmt.Fprintln(&b)
	// Read the accuracy figure honestly. The corpus only holds scenarios whose
	// labelled phenomenon the fault harness actually reproduced; where a fault
	// never reproduced its phenomenon the scenario is removed rather than
	// counted as a miss (tools/faultinjector/scenarios/ records each such
	// attempt and why it stopped). So this percentage measures the engine over
	// reproducible faults, and it rises when a hard scenario is dropped — the
	// denominator is the number that says how much was actually tested.
	fmt.Fprintln(&b, "Accuracy is measured only over scenarios whose labelled phenomenon the fault")
	fmt.Fprintln(&b, "harness reproduced. Faults that never reproduced their phenomenon were removed")
	fmt.Fprintln(&b, "from the corpus rather than counted as misses, so this percentage can rise")
	fmt.Fprintln(&b, "because evidence was withdrawn, not because the engine improved — read it")
	fmt.Fprintln(&b, "together with the corpus size above, and see")
	fmt.Fprintln(&b, "`tools/faultinjector/scenarios/` for what was attempted and abandoned.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Per-cause precision / recall")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Cause | TP | FP | FN | Precision | Recall |")
	fmt.Fprintln(&b, "|---|---|---|---|---|---|")
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		l := labels[name]
		precision := ratio(l.tp, l.tp+l.fp)
		recall := ratio(l.tp, l.tp+l.fn)
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %s | %s |\n", name, l.tp, l.fp, l.fn, precision, recall)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Per-scenario detail")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Scenario | Expected | Got | Confidence (want / got) | Result |")
	fmt.Fprintln(&b, "|---|---|---|---|---|")
	for _, r := range results {
		status := "MISS"
		if r.correct {
			status = "match"
		}
		fmt.Fprintf(&b, "| %s | `%s` | `%s` | %s / %s | %s |\n", r.id, r.want, r.got, r.wantConfidence, r.gotConfidence, status)
	}

	return b.String()
}

func ratio(numerator, denominator int) string {
	if denominator == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(numerator)/float64(denominator)*100)
}
