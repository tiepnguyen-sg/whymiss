package report

import (
	"encoding/json"

	"github.com/CHANGEME/whymiss/internal/domain"
)

// JSON renders v as indented JSON, for machine consumption or a `whymiss
// <slot> --format json` invocation. The field set and names are exactly
// domain.Verdict's own json tags — this is not a separate wire format.
func JSON(v domain.Verdict) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
