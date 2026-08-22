package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/CHANGEME/whymiss/internal/app"
	"github.com/CHANGEME/whymiss/internal/domain"
)

func newWatchCmd(flags *globalFlags) *cobra.Command {
	var (
		minRequestInterval time.Duration
		hostSampleInterval time.Duration
		clMetricsAPI       string
		peerSampleInterval time.Duration
		retentionMaxAge    time.Duration
		retentionMaxBytes  int64
		retentionInterval  time.Duration
		validatorIndices   []uint
		metricsAddr        string
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
				BeaconAPI:          flags.beaconAPI,
				DBPath:             flags.dbPath,
				MinRequestInterval: minRequestInterval,
				HostSampleInterval: hostSampleInterval,
				CLMetricsAPI:       clMetricsAPI,
				PeerSampleInterval: peerSampleInterval,
				RetentionMaxAge:    retentionMaxAge,
				RetentionMaxBytes:  retentionMaxBytes,
				RetentionInterval:  retentionInterval,
				ValidatorIndices:   validators,
				MetricsAddr:        metricsAddr,
				Logger:             logger,
			}
			return app.Watch(ctx, cfg)
		},
	}

	cmd.Flags().DurationVar(&minRequestInterval, "min-request-interval", 200*time.Millisecond, "floor between successive beacon API requests (I-5)")
	cmd.Flags().DurationVar(&hostSampleInterval, "host-sample-interval", 10*time.Second, "how often to sample host disk/memory/CPU pressure (0 disables)")
	cmd.Flags().StringVar(&clMetricsAPI, "cl-metrics-api", "", "consensus client's own Prometheus endpoint, e.g. http://127.0.0.1:5054/metrics (empty disables peer-count sampling)")
	cmd.Flags().DurationVar(&peerSampleInterval, "peer-sample-interval", 15*time.Second, "how often to sample peer count when --cl-metrics-api is set")
	cmd.Flags().DurationVar(&retentionMaxAge, "retention-max-age", 14*24*time.Hour, "delete recorded facts older than this")
	cmd.Flags().Int64Var(&retentionMaxBytes, "retention-max-bytes", 1<<30, "delete oldest facts once the store exceeds this many bytes (I-12)")
	cmd.Flags().DurationVar(&retentionInterval, "retention-interval", time.Hour, "how often to run retention (0 disables)")
	cmd.Flags().UintSliceVar(&validatorIndices, "validator-index", nil, "validator index to track duties for; repeatable (e.g. --validator-index 24 --validator-index 40). Empty disables duty tracking and the metrics exporter")
	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", "", "address to serve Prometheus metrics on, e.g. :9101 (empty disables; ignored unless --validator-index is set)")
	return cmd
}
