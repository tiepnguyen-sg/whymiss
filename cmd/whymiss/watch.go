package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/tiepnguyen-sg/whymiss/internal/app"
	appconfig "github.com/tiepnguyen-sg/whymiss/internal/config"
	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

func newWatchCmd(flags *globalFlags) *cobra.Command {
	defaults := appconfig.Default().Watch
	var (
		minRequestInterval time.Duration
		hostSampleInterval time.Duration
		clMetricsAPI       string
		peerSampleInterval time.Duration
		ntpServers         []string
		clockInterval      time.Duration
		retentionMaxAge    time.Duration
		retentionMaxBytes  int64
		retentionInterval  time.Duration
		validatorIndices   []uint
		metricsAddr        string
		baselineBeaconAPI  string
		baselineMetricsAPI string
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run the collector daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.beaconAPI == "" {
				return fmt.Errorf("--beacon-api is required")
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
			validators := make([]domain.ValidatorIndex, len(validatorIndices))
			for i, vi := range validatorIndices {
				validators[i] = domain.ValidatorIndex(vi)
			}
			cfg := app.WatchConfig{
				BeaconAPI:           flags.beaconAPI,
				DBPath:              flags.dbPath,
				MinRequestInterval:  minRequestInterval,
				HostSampleInterval:  hostSampleInterval,
				CLMetricsAPI:        clMetricsAPI,
				PeerSampleInterval:  peerSampleInterval,
				BaselineBeaconAPI:   baselineBeaconAPI,
				BaselineMetricsAPI:  baselineMetricsAPI,
				NTPServers:          ntpServers,
				ClockSampleInterval: clockInterval,
				RetentionMaxAge:     retentionMaxAge,
				RetentionMaxBytes:   retentionMaxBytes,
				RetentionInterval:   retentionInterval,
				ValidatorIndices:    validators,
				MetricsAddr:         metricsAddr,
				Schedule:            flags.schedule,
				RCAConfig:           flags.rcaConfig,
				Logger:              logger,
			}
			return app.Watch(ctx, cfg)
		},
	}

	cmd.Flags().DurationVar(&minRequestInterval, "min-request-interval", defaults.MinRequestInterval, "floor between successive beacon API requests (I-5)")
	cmd.Flags().DurationVar(&hostSampleInterval, "host-sample-interval", defaults.HostSampleInterval, "how often to sample host disk/memory/CPU pressure (0 disables)")
	cmd.Flags().StringVar(&clMetricsAPI, "cl-metrics-api", defaults.CLMetricsAPI, "consensus client's own Prometheus endpoint, e.g. http://127.0.0.1:5054/metrics (empty disables peer-count sampling)")
	cmd.Flags().DurationVar(&peerSampleInterval, "peer-sample-interval", defaults.PeerSampleInterval, "how often to sample peer count when --cl-metrics-api is set")
	cmd.Flags().StringVar(&baselineBeaconAPI, "baseline-beacon-api", defaults.BaselineBeaconAPI, "a second, independent beacon node's API, used only to tell network-wide lateness from local lateness (empty disables; must differ from --beacon-api)")
	cmd.Flags().StringVar(&baselineMetricsAPI, "baseline-metrics-api", defaults.BaselineMetricsAPI, "that same independent node's Prometheus endpoint; required with --baseline-beacon-api")
	cmd.Flags().StringSliceVar(&ntpServers, "ntp-server", defaults.NTPServers, "NTP server for clock-offset sampling; repeatable. Empty disables timing attribution (I-9)")
	cmd.Flags().DurationVar(&clockInterval, "clock-sample-interval", defaults.ClockSampleInterval, "how often to sample clock offset")
	cmd.Flags().DurationVar(&retentionMaxAge, "retention-max-age", defaults.RetentionMaxAge, "delete recorded facts older than this")
	cmd.Flags().Int64Var(&retentionMaxBytes, "retention-max-bytes", defaults.RetentionMaxBytes, "delete oldest facts once the store exceeds this many bytes (I-12)")
	cmd.Flags().DurationVar(&retentionInterval, "retention-interval", defaults.RetentionInterval, "how often to run retention (0 disables)")
	cmd.Flags().UintSliceVar(&validatorIndices, "validator-index", nil, "validator index to track duties for; repeatable (e.g. --validator-index 24 --validator-index 40). Empty disables duty tracking and the metrics exporter")
	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", "", "address to serve Prometheus metrics on, e.g. :9101 (empty disables; ignored unless --validator-index is set)")
	return cmd
}
