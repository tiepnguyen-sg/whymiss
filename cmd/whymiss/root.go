package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
	appconfig "github.com/tiepnguyen-sg/whymiss/internal/config"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
	"github.com/tiepnguyen-sg/whymiss/internal/report"
)

// globalFlags are shared by every subcommand that talks to a beacon node or
// a database file — declared once here rather than duplicated per command.
type globalFlags struct {
	beaconAPI  string
	dbPath     string
	configPath string
	schedule   domain.SlotSchedule
	rcaConfig  rca.Config
	loaded     appconfig.Config
}

// newRootCmd builds whymiss's fixed CLI surface (AGENTS.md): `whymiss
// <slot>` as the bare root command, plus `watch`, `timeline <slot>`, and `doctor`.
// AGENTS.md fixes this surface; adding anything else requires approval.
func newRootCmd() *cobra.Command {
	defaults := appconfig.Default()
	flags := &globalFlags{schedule: defaults.Schedule, rcaConfig: defaults.RCA, loaded: defaults}
	var format string
	var validatorIndices []uint

	root := &cobra.Command{
		Use:           "whymiss [slot]",
		Short:         "Explain why a validator missed or was late on a duty",
		Version:       version,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := appconfig.Load(flags.configPath, os.LookupEnv)
			if err != nil {
				return fmt.Errorf("load configuration: %w", err)
			}
			flags.schedule, flags.rcaConfig, flags.loaded = cfg.Schedule, cfg.RCA, cfg
			if err := applyConfigFlags(cmd, cfg); err != nil {
				return fmt.Errorf("apply configuration: %w", err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

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
			var v domain.Verdict
			if validator == nil {
				v, err = app.Explain(cmd.Context(), flags.dbPath, domain.Slot(slotArg), schedule, flags.rcaConfig)
			} else {
				v, err = app.ExplainForValidator(cmd.Context(), flags.dbPath, domain.Slot(slotArg), *validator, flags.schedule, flags.rcaConfig)
			}
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
	root.PersistentFlags().StringVar(&flags.beaconAPI, "beacon-api", defaults.BeaconAPI, "beacon node base URL, e.g. http://127.0.0.1:5052")
	root.PersistentFlags().StringVar(&flags.dbPath, "db", defaults.DBPath, "path to the SQLite store")
	root.PersistentFlags().StringVar(&flags.configPath, "config", "", "YAML configuration file (WHYMISS_* variables override it)")
	root.Flags().StringVar(&format, "format", "markdown", "output format (markdown, json)")
	root.Flags().UintSliceVar(&validatorIndices, "validator-index", nil, "validator duty to explain when multiple tracked validators share the slot")

	root.AddCommand(newWatchCmd(flags))
	root.AddCommand(newTimelineCmd(flags))
	root.AddCommand(newDoctorCmd(flags))
	return root
}

func requestedValidator(indices []uint) (*domain.ValidatorIndex, error) {
	if len(indices) == 0 {
		return nil, nil
	}
	if len(indices) != 1 {
		return nil, fmt.Errorf("select exactly one --validator-index, got %d", len(indices))
	}
	value := domain.ValidatorIndex(indices[0])
	return &value, nil
}

func applyConfigFlags(cmd *cobra.Command, cfg appconfig.Config) error {
	values := map[string]string{
		"beacon-api": cfg.BeaconAPI, "db": cfg.DBPath,
		"min-request-interval": cfg.Watch.MinRequestInterval.String(),
		"host-sample-interval": cfg.Watch.HostSampleInterval.String(), "cl-metrics-api": cfg.Watch.CLMetricsAPI,
		"baseline-beacon-api": cfg.Watch.BaselineBeaconAPI, "baseline-metrics-api": cfg.Watch.BaselineMetricsAPI,
		"peer-sample-interval": cfg.Watch.PeerSampleInterval.String(), "ntp-server": strings.Join(cfg.Watch.NTPServers, ","),
		"clock-sample-interval": cfg.Watch.ClockSampleInterval.String(), "retention-max-age": cfg.Watch.RetentionMaxAge.String(),
		"retention-max-bytes": strconv.FormatInt(cfg.Watch.RetentionMaxBytes, 10),
		"retention-interval":  cfg.Watch.RetentionInterval.String(), "metrics-addr": cfg.Watch.MetricsAddr,
	}
	indices := make([]string, len(cfg.Watch.ValidatorIndices))
	for i, index := range cfg.Watch.ValidatorIndices {
		indices[i] = strconv.FormatUint(index, 10)
	}
	values["validator-index"] = strings.Join(indices, ",")
	for name, value := range values {
		flag := cmd.Flag(name)
		if flag == nil || flag.Changed {
			continue
		}
		// pflag's slice parsers interpret an empty string as one empty item;
		// the zero-value configuration is already represented by the flag's
		// empty default, so there is nothing to apply.
		if value == "" && (name == "ntp-server" || name == "validator-index") {
			continue
		}
		if err := flag.Value.Set(value); err != nil {
			return fmt.Errorf("set --%s: %w", name, err)
		}
	}
	return nil
}
