package config

func configMap(cfg Config) map[string]any {
	return map[string]any{
		"beacon_api": cfg.BeaconAPI,
		"db":         cfg.DBPath,
		"watch": map[string]any{
			"min_request_interval": cfg.Watch.MinRequestInterval.String(),
			"host_sample_interval": cfg.Watch.HostSampleInterval.String(),
			"cl_metrics_api":       cfg.Watch.CLMetricsAPI, "peer_sample_interval": cfg.Watch.PeerSampleInterval.String(),
			"baseline_beacon_api": cfg.Watch.BaselineBeaconAPI, "baseline_metrics_api": cfg.Watch.BaselineMetricsAPI,
			"ntp_servers": cfg.Watch.NTPServers, "clock_sample_interval": cfg.Watch.ClockSampleInterval.String(),
			"retention_max_age": cfg.Watch.RetentionMaxAge.String(), "retention_max_bytes": cfg.Watch.RetentionMaxBytes,
			"retention_interval": cfg.Watch.RetentionInterval.String(), "validator_indices": cfg.Watch.ValidatorIndices,
			"metrics_addr": cfg.Watch.MetricsAddr,
		},
		"schedule": map[string]any{
			"seconds_per_slot": cfg.Schedule.SecondsPerSlot.String(), "attestation_deadline": cfg.Schedule.AttestationDeadline.String(),
			"aggregation_deadline": cfg.Schedule.AggregationDeadline.String(),
		},
		"thresholds": map[string]any{
			"dominance": cfg.RCA.Dominance, "clock_offset_max": cfg.RCA.ClockOffsetMax.String(),
			"clock_sample_max_age": cfg.RCA.ClockSampleMaxAge.String(), "network_deviation": cfg.RCA.NetworkDeviation.String(),
			"engine_spike_multiplier": cfg.RCA.EngineSpikeMultiplier, "peer_count_min": cfg.RCA.PeerCountMin,
			"iowait_pct":    cfg.RCA.IOWaitPct,
			"cpu_steal_pct": cfg.RCA.CPUStealPct, "psi_mem_avg10": cfg.RCA.PSIMemAvg10,
		},
	}
}

func fileConfigMap(raw fileConfig) map[string]any {
	out := map[string]any{}
	setPointer(out, "beacon_api", raw.BeaconAPI)
	setPointer(out, "db", raw.DBPath)
	setPointer(out, "watch.min_request_interval", raw.Watch.MinRequestInterval)
	setPointer(out, "watch.host_sample_interval", raw.Watch.HostSampleInterval)
	setPointer(out, "watch.cl_metrics_api", raw.Watch.CLMetricsAPI)
	setPointer(out, "watch.baseline_beacon_api", raw.Watch.BaselineBeaconAPI)
	setPointer(out, "watch.baseline_metrics_api", raw.Watch.BaselineMetricsAPI)
	setPointer(out, "watch.peer_sample_interval", raw.Watch.PeerSampleInterval)
	setPointer(out, "watch.ntp_servers", raw.Watch.NTPServers)
	setPointer(out, "watch.clock_sample_interval", raw.Watch.ClockSampleInterval)
	setPointer(out, "watch.retention_max_age", raw.Watch.RetentionMaxAge)
	setPointer(out, "watch.retention_max_bytes", raw.Watch.RetentionMaxBytes)
	setPointer(out, "watch.retention_interval", raw.Watch.RetentionInterval)
	setPointer(out, "watch.validator_indices", raw.Watch.ValidatorIndices)
	setPointer(out, "watch.metrics_addr", raw.Watch.MetricsAddr)
	setPointer(out, "schedule.seconds_per_slot", raw.Schedule.SecondsPerSlot)
	setPointer(out, "schedule.attestation_deadline", raw.Schedule.AttestationDeadline)
	setPointer(out, "schedule.aggregation_deadline", raw.Schedule.AggregationDeadline)
	setPointer(out, "thresholds.dominance", raw.Threshold.Dominance)
	setPointer(out, "thresholds.clock_offset_max", raw.Threshold.ClockOffsetMax)
	setPointer(out, "thresholds.clock_sample_max_age", raw.Threshold.ClockSampleMaxAge)
	setPointer(out, "thresholds.network_deviation", raw.Threshold.NetworkDeviation)
	setPointer(out, "thresholds.engine_spike_multiplier", raw.Threshold.EngineSpikeMultiplier)
	setPointer(out, "thresholds.peer_count_min", raw.Threshold.PeerCountMin)
	setPointer(out, "thresholds.iowait_pct", raw.Threshold.IOWaitPct)
	setPointer(out, "thresholds.cpu_steal_pct", raw.Threshold.CPUStealPct)
	setPointer(out, "thresholds.psi_mem_avg10", raw.Threshold.PSIMemAvg10)
	return out
}

func envMap(lookup func(string) (string, bool)) map[string]any {
	out := map[string]any{}
	for env, path := range map[string]string{
		"WHYMISS_BEACON_API": "beacon_api", "WHYMISS_DB": "db",
		"WHYMISS_MIN_REQUEST_INTERVAL": "watch.min_request_interval", "WHYMISS_HOST_SAMPLE_INTERVAL": "watch.host_sample_interval",
		"WHYMISS_CL_METRICS_API": "watch.cl_metrics_api", "WHYMISS_PEER_SAMPLE_INTERVAL": "watch.peer_sample_interval",
		"WHYMISS_BASELINE_BEACON_API": "watch.baseline_beacon_api", "WHYMISS_BASELINE_METRICS_API": "watch.baseline_metrics_api",
		"WHYMISS_NTP_SERVERS": "watch.ntp_servers", "WHYMISS_CLOCK_SAMPLE_INTERVAL": "watch.clock_sample_interval",
		"WHYMISS_RETENTION_MAX_AGE": "watch.retention_max_age", "WHYMISS_RETENTION_MAX_BYTES": "watch.retention_max_bytes",
		"WHYMISS_RETENTION_INTERVAL": "watch.retention_interval", "WHYMISS_VALIDATOR_INDICES": "watch.validator_indices",
		"WHYMISS_METRICS_ADDR": "watch.metrics_addr", "WHYMISS_SECONDS_PER_SLOT": "schedule.seconds_per_slot",
		"WHYMISS_ATTESTATION_DEADLINE": "schedule.attestation_deadline", "WHYMISS_AGGREGATION_DEADLINE": "schedule.aggregation_deadline",
		"WHYMISS_DOMINANCE": "thresholds.dominance", "WHYMISS_CLOCK_OFFSET_MAX": "thresholds.clock_offset_max",
		"WHYMISS_CLOCK_SAMPLE_MAX_AGE": "thresholds.clock_sample_max_age", "WHYMISS_NETWORK_DEVIATION": "thresholds.network_deviation",
		"WHYMISS_ENGINE_SPIKE_MULTIPLIER": "thresholds.engine_spike_multiplier", "WHYMISS_PEER_COUNT_MIN": "thresholds.peer_count_min",
		"WHYMISS_IOWAIT_PCT":    "thresholds.iowait_pct",
		"WHYMISS_CPU_STEAL_PCT": "thresholds.cpu_steal_pct", "WHYMISS_PSI_MEM_AVG10": "thresholds.psi_mem_avg10",
	} {
		if value, ok := lookup(env); ok {
			setPath(out, path, value)
		}
	}
	return out
}

func setPointer[T any](out map[string]any, path string, value *T) {
	if value != nil {
		setPath(out, path, *value)
	}
}

func setPath(out map[string]any, path string, value any) {
	for i := range len(path) {
		if path[i] != '.' {
			continue
		}
		parent := path[:i]
		child := path[i+1:]
		nested, ok := out[parent].(map[string]any)
		if !ok {
			nested = map[string]any{}
			out[parent] = nested
		}
		nested[child] = value
		return
	}
	out[path] = value
}
