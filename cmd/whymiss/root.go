package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	_ "github.com/tiepnguyen-sg/whymiss/internal/rca/rules" // registers rca.Order via init
	"github.com/tiepnguyen-sg/whymiss/internal/report"
)

// globalFlags are shared by every subcommand that talks to a beacon node or
// a database file — declared once here rather than duplicated per command.
type globalFlags struct {
	beaconAPI string
	dbPath    string
}

// newRootCmd builds whymiss's fixed CLI surface (AGENTS.md): `whymiss
// <slot>` as the bare root command, plus `watch` and `timeline <slot>`.
// `doctor` arrives with CLI polish (Phase 4) — adding a subcommand beyond
// AGENTS.md's fixed list requires human approval, so none is added here
// ahead of the phase that actually implements it.
func newRootCmd() *cobra.Command {
	flags := &globalFlags{}
	var format string

	root := &cobra.Command{
		Use:           "whymiss [slot]",
		Short:         "Explain why a validator missed or was late on a duty",
		Version:       version,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			slotArg, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid slot %q: %w", args[0], err)
			}

			// MainnetPreEPBS is the schedule this build ships with until
			// Phase 5's configurable SlotSchedule (docs/causes.md §3.1) lands.
			v, err := app.Explain(cmd.Context(), flags.dbPath, domain.Slot(slotArg), domain.MainnetPreEPBS())
			if err != nil {
				return err
			}

			switch format {
			case "markdown":
				_, err := fmt.Fprint(cmd.OutOrStdout(), report.Markdown(v))
				return err
			case "json":
				out, err := report.JSON(v)
				if err != nil {
					return fmt.Errorf("render json: %w", err)
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(out))
				return err
			default:
				return fmt.Errorf("unsupported --format %q (supported: markdown, json)", format)
			}
		},
	}
	root.PersistentFlags().StringVar(&flags.beaconAPI, "beacon-api", "", "beacon node base URL, e.g. http://127.0.0.1:5052")
	root.PersistentFlags().StringVar(&flags.dbPath, "db", "whymiss.db", "path to the SQLite store")
	root.Flags().StringVar(&format, "format", "markdown", "output format (markdown, json)")

	root.AddCommand(newWatchCmd(flags))
	root.AddCommand(newTimelineCmd(flags))
	return root
}
