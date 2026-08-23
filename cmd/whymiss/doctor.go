package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
)

func newDoctorCmd(flags *globalFlags) *cobra.Command {
	var ntpServers []string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify beacon connectivity, storage, and clock trust",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := app.Doctor(cmd.Context(), app.DoctorConfig{
				BeaconAPI:          flags.beaconAPI,
				DBPath:             flags.dbPath,
				NTPServers:         ntpServers,
				MinRequestInterval: flags.loaded.Watch.MinRequestInterval,
				ClockOffsetMax:     flags.rcaConfig.ClockOffsetMax,
			})
			failed := false
			for _, check := range checks {
				if check.Err != nil {
					failed = true
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "FAIL %-8s %v\n", check.Name, check.Err); err != nil {
						return fmt.Errorf("write doctor result: %w", err)
					}
					continue
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "OK   %-8s %s\n", check.Name, check.Detail); err != nil {
					return fmt.Errorf("write doctor result: %w", err)
				}
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
