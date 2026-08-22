package report

import (
	"fmt"
	"strings"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// Markdown renders v as a self-contained post-mortem readable pasted
// straight into a forum post or incident channel (task 3.5) — no template
// engine, just a fixed structure over the verdict's own fields.
func Markdown(v domain.Verdict) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Slot %d — %s\n\n", v.Slot, headline(v))
	fmt.Fprintf(&b, "**Outcome:** %s\n", outcomeLine(v))
	fmt.Fprintf(&b, "**Confidence:** %s\n\n", v.Confidence)

	b.WriteString("## Evidence\n\n")
	for _, e := range v.Evidence {
		fmt.Fprintf(&b, "- [%s] %s", e.At.Format("15:04:05.000Z"), e.Statement)
		if e.Comparison != nil {
			fmt.Fprintf(&b, " (%s: %s vs %s %s)", e.Comparison.Label, formatValue(e.Comparison.Observed), formatValue(e.Comparison.Expected), e.Comparison.Unit)
		}
		fmt.Fprintf(&b, " — *%s*\n", e.Source)
	}

	if len(v.Remediation) > 0 {
		b.WriteString("\n## Remediation\n\n")
		for i, r := range v.Remediation {
			fmt.Fprintf(&b, "%d. %s\n", i+1, r)
		}
	}

	fmt.Fprintf(&b, "\n---\nEngine %s · Taxonomy %s\n", v.EngineVersion, v.TaxonomyVersion)

	return b.String()
}

func headline(v domain.Verdict) string {
	if v.Outcome == domain.OutcomeNoDuty {
		return "no duty"
	}
	return string(v.ReportedCause())
}

func outcomeLine(v domain.Verdict) string {
	if v.Flags == nil {
		return string(v.Outcome)
	}
	lost := v.Flags.Lost()
	if len(lost) == 0 {
		return string(v.Outcome)
	}
	return fmt.Sprintf("%s (lost: %s)", v.Outcome, strings.Join(lost, ", "))
}

func formatValue(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
}
