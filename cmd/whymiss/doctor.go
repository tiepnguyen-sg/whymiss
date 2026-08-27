package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
)

func newDoctorCmd(flags *globalFlags) *cobra.Command {
	var ntpServers []string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify every configured endpoint, storage, and clock trust",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := app.Doctor(cmd.Context(), app.DoctorConfig{
				BeaconAPI:          flags.beaconAPI,
				DBPath:             flags.dbPath,
				NTPServers:         ntpServers,
				MinRequestInterval: flags.loaded.Watch.MinRequestInterval,
				ClockOffsetMax:     flags.rcaConfig.ClockOffsetMax,
				CLMetricsAPI:       flags.loaded.Watch.CLMetricsAPI,
				BaselineBeaconAPI:  flags.loaded.Watch.BaselineBeaconAPI,
				BaselineMetricsAPI: flags.loaded.Watch.BaselineMetricsAPI,
			})
			failed, err := renderDoctorChecks(cmd.OutOrStdout(), checks)
			if err != nil {
				return err
			}
			if failed {
				return fmt.Errorf("doctor found one or more blocking problems")
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&ntpServers, "ntp-server", nil, "NTP server to verify; repeatable (required for timing attribution)")
	return cmd
}

// renderDoctorChecks writes one line per check and reports whether any of them
// blocks. Only an Err blocks: a Warn is a real limitation in a configuration the
// operator is entitled to run, and failing the command an operator uses to decide
// whether setup is complete would make a deliberately minimal deployment look
// broken.
func renderDoctorChecks(w io.Writer, checks []app.DoctorCheck) (bool, error) {
	failed := false
	for _, check := range checks {
		status := "OK  "
		detail := check.Detail
		switch {
		case check.Err != nil:
			failed = true
			status, detail = "FAIL", check.Err.Error()
		case check.Warn:
			status = "WARN"
		}
		if _, err := fmt.Fprintf(w, "%-4s %-9s %s\n", status, check.Name, detail); err != nil {
			return failed, fmt.Errorf("write doctor result: %w", err)
		}
	}
	return failed, nil
}
