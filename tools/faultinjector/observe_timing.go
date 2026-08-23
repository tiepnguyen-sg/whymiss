package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/source"
	"github.com/tiepnguyen-sg/whymiss/internal/source/beaconapi"
)

type headTimingResult struct {
	Head        domain.Observation
	HeadErr     error
	Timing      source.BlockTiming
	TimingErr   error
	EngineAfter *source.EngineCounters
	EngineErr   error
}

func waitHeadTiming(ctx context.Context, result <-chan headTimingResult, done <-chan struct{}) (headTimingResult, error) {
	select {
	case measured := <-result:
		return measured, nil
	case <-done:
		select {
		case measured := <-result:
			return measured, nil
		default:
			return headTimingResult{}, fmt.Errorf("head timing observer exited without a result")
		}
	case <-ctx.Done():
		return headTimingResult{}, ctx.Err()
	}
}

func observeHeadTiming(ctx context.Context, sampler *source.MetricsSampler, client *beaconapi.Client, consensusClient source.ConsensusClient, metricsURL string, slot domain.Slot, deadline time.Time, sampleEngine bool, result chan<- headTimingResult) {
	head, found, err := client.HeadUpdated(ctx, slot, deadline)
	if err != nil {
		select {
		case result <- headTimingResult{HeadErr: err}:
		case <-ctx.Done():
		}
		return
	}
	if !found {
		select {
		case result <- headTimingResult{HeadErr: fmt.Errorf("exact validated head for slot %d was not observed before it advanced or the deadline elapsed", slot)}:
		case <-ctx.Done():
		}
		return
	}
	timing, timingErr := sampler.SampleBlockTimingForSlot(ctx, consensusClient, metricsURL, slot, time.Now().Add(blockTimingCatchupWindow))
	var engineAfter *source.EngineCounters
	var engineErr error
	if sampleEngine {
		counters, err := sampler.SampleEngineCounters(ctx, consensusClient, metricsURL)
		engineErr = err
		if err == nil {
			engineAfter = &counters
		}
	}
	select {
	case result <- headTimingResult{Head: head, Timing: timing, TimingErr: timingErr, EngineAfter: engineAfter, EngineErr: engineErr}:
	case <-ctx.Done():
	}
}

func prepareHeadTiming(ctx context.Context, beaconAPI, enclave, timingTarget string) (*beaconapi.Client, source.ConsensusClient, string, error) {
	client := beaconapi.NewClient(beaconAPI, 200*time.Millisecond)
	version, err := client.FetchNodeVersion(ctx)
	if err != nil {
		return nil, source.ConsensusUnknown, "", fmt.Errorf("fetch node version: %w", err)
	}
	consensusClient := source.DetectConsensusClient(version)
	if consensusClient == source.ConsensusUnknown {
		return nil, consensusClient, "", fmt.Errorf("unsupported consensus client version %q", version)
	}
	metricsURL, err := resolveCLMetricsURL(ctx, enclave, timingTarget)
	if err != nil {
		return nil, consensusClient, "", err
	}
	return client, consensusClient, metricsURL, nil
}
