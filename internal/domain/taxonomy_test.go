package domain_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// causesDoc is docs/causes.md's location relative to this package. ADR-0005
// designates that file the single source of truth for the taxonomy; this test is
// the mechanism, not documentation of one — it fails the build the moment code and
// contract drift apart.
const causesDoc = "../../docs/causes.md"

// causeIDPattern matches a taxonomy-shaped token in backticks: network.x,
// local.x, local.x.y, or unknown.x. It is deliberately narrow so it does not
// accidentally match an unrelated backtick-quoted identifier elsewhere in the doc.
var causeIDPattern = regexp.MustCompile("`(network|local|unknown)\\.[a-z0-9_]+(\\.[a-z0-9_]+)?`")

// TestTaxonomyMatchesDocs asserts every domain.CauseID is documented in
// docs/causes.md and every taxonomy-shaped token documented there is a known
// domain.CauseID. Either direction of drift is the failure mode ADR-0005 exists to
// prevent: a rule emitting an ID the document never defined, or documentation
// promising an ID no rule can produce.
func TestTaxonomyMatchesDocs(t *testing.T) {
	t.Parallel()

	path := causesDoc
	if !filepath.IsAbs(path) {
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			t.Fatalf("resolve %s: %v", causesDoc, err)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v — this test must run from a full checkout", causesDoc, err)
	}
	defer f.Close()

	documented := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		for _, m := range causeIDPattern.FindAllString(scanner.Text(), -1) {
			documented[m[1:len(m)-1]] = true // strip surrounding backticks
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", causesDoc, err)
	}
	if len(documented) == 0 {
		t.Fatalf("found no cause-id-shaped tokens in %s — pattern or path is stale", causesDoc)
	}

	coded := map[string]bool{}
	for _, id := range domain.CauseIDs() {
		coded[string(id)] = true
	}

	for id := range coded {
		if !documented[id] {
			t.Errorf("domain.CauseIDs() has %q, which docs/causes.md does not document", id)
		}
	}
	for id := range documented {
		if !coded[id] {
			t.Errorf("docs/causes.md documents %q, which is not in domain.CauseIDs()", id)
		}
	}
}

// TestSubCausesDescendFromDocumentedParent enforces the hierarchy rule ADR-0005
// states in prose: a sub-cause must be a genuine descendant of a cause the taxonomy
// also defines, so aggregating on the parent can never miss an emitted child.
func TestSubCausesDescendFromDocumentedParent(t *testing.T) {
	t.Parallel()

	coded := map[domain.CauseID]bool{}
	for _, id := range domain.CauseIDs() {
		coded[id] = true
	}

	for _, id := range domain.CauseIDs() {
		parent, sub := splitParent(id)
		if parent == "" || !coded[parent] {
			// Either top-level (network.x, local.x, unknown.x) or the dotted
			// prefix is a namespace rather than a cause in its own right — e.g.
			// local.host.disk_io has no bare "local.host" cause. Nothing to check.
			continue
		}
		if !sub.IsSubCauseOf(parent) {
			t.Errorf("IsSubCauseOf() disagrees with taxonomy layout for %q under %q", sub, parent)
		}
	}
}

// splitParent returns the immediate parent of a dotted cause id, or empty if the
// id is already top-level (exactly one dot, e.g. "network.late_block").
func splitParent(id domain.CauseID) (domain.CauseID, domain.CauseID) {
	s := string(id)
	last := -1
	dots := 0
	for i, r := range s {
		if r == '.' {
			dots++
			last = i
		}
	}
	if dots < 2 {
		return "", id
	}
	return domain.CauseID(s[:last]), id
}
