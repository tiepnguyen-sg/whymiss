package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func newTimelineCmd(flags *globalFlags) *cobra.Command {
	var format string
	var validatorIndices []uint

	cmd := &cobra.Command{
		Use:   "timeline <slot>",
		Short: "Print the raw recorded facts for a slot, no interpretation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slotArg, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid slot %q: %w", args[0], err)
			}

			validator, err := requestedValidator(validatorIndices)
			if err != nil {
				return err
			}
			schedule := app.ResolveSchedule(cmd.Context(), flags.beaconAPI,
				flags.loaded.Watch.MinRequestInterval, flags.schedule, nil)
			var tl domain.Timeline
			if validator == nil {
				tl, err = app.GetTimeline(cmd.Context(), flags.dbPath, domain.Slot(slotArg), schedule)
			} else {
				tl, err = app.GetTimelineForValidator(cmd.Context(), flags.dbPath, domain.Slot(slotArg), *validator, schedule)
			}
			if err != nil {
				return err
			}

			switch format {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(tl)
			default:
				return fmt.Errorf("unsupported --format %q (supported: json)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format")
	cmd.Flags().UintSliceVar(&validatorIndices, "validator-index", nil, "validator duty to print when multiple tracked validators share the slot")
	return cmd
}
