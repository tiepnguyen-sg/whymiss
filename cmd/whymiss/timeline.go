package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func newTimelineCmd(flags *globalFlags) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "timeline <slot>",
		Short: "Print the raw recorded facts for a slot, no interpretation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slotArg, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid slot %q: %w", args[0], err)
			}

			// MainnetPreEPBS is the schedule this build ships with until
			// Phase 5's configurable SlotSchedule (docs/causes.md §3.1)
			// lands; a replayed corpus scenario would carry its own
			// schedule instead, but the live store has no per-record
			// schedule to read back.
			tl, err := app.GetTimeline(cmd.Context(), flags.dbPath, domain.Slot(slotArg), domain.MainnetPreEPBS())
			if err != nil {
				return err
			}

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tl)
			default:
				return fmt.Errorf("unsupported --format %q (supported: json)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format")
	return cmd
}
