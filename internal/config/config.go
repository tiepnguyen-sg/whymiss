// Package config loads and validates whymiss operator configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
	"github.com/tiepnguyen-sg/whymiss/internal/rca"
)

const maxConfigBytes = 1 << 20

// Config is the complete operator configuration.
type Config struct {
	BeaconAPI string
	DBPath    string
	Watch     Watch
	Schedule  domain.SlotSchedule
	RCA       rca.Config
}

// Watch contains daemon-only settings.
type Watch struct {
	MinRequestInterval  time.Duration
	HostSampleInterval  time.Duration
	CLMetricsAPI        string
	PeerSampleInterval  time.Duration
	BaselineBeaconAPI   string
	BaselineMetricsAPI  string
	NTPServers          []string
	ClockSampleInterval time.Duration
	RetentionMaxAge     time.Duration
	RetentionMaxBytes   int64
	RetentionInterval   time.Duration
	ValidatorIndices    []uint64
	MetricsAddr         string
}

// Default returns safe defaults with no network endpoint enabled.
func Default() Config {
	return Config{
		DBPath: "whymiss.db",
		Watch: Watch{
			MinRequestInterval: 200 * time.Millisecond, HostSampleInterval: 10 * time.Second,
			PeerSampleInterval: 15 * time.Second, ClockSampleInterval: time.Minute,
			RetentionMaxAge: 14 * 24 * time.Hour, RetentionMaxBytes: 1 << 30,
			RetentionInterval: time.Hour,
		},
		Schedule: domain.MainnetPreEPBS(),
		RCA:      rca.DefaultConfig(),
	}
}

// Load merges defaults, an optional YAML file, then WHYMISS_* variables.
func Load(path string, lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	k := koanf.New(".")
	if err := k.Load(mapProvider{values: configMap(Default())}, nil); err != nil {
		return Config{}, fmt.Errorf("load defaults: %w", err)
	}
	if path != "" {
		values, err := readFile(path)
		if err != nil {
			return Config{}, err
		}
		if err := k.Load(mapProvider{values: values}, nil); err != nil {
			return Config{}, fmt.Errorf("merge config file %s: %w", path, err)
		}
	}
	if err := k.Load(mapProvider{values: envMap(lookup)}, nil); err != nil {
		return Config{}, fmt.Errorf("merge environment: %w", err)
	}
	cfg, err := decode(k)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Schedule.Validate(); err != nil {
		return Config{}, fmt.Errorf("schedule: %w", err)
	}
	if err := cfg.RCA.Validate(); err != nil {
		return Config{}, fmt.Errorf("thresholds: %w", err)
	}
	return cfg, nil
}

type mapProvider struct{ values map[string]any }

func (p mapProvider) Read() (map[string]any, error) { return p.values, nil }
func (p mapProvider) ReadBytes() ([]byte, error)    { return nil, nil }

type fileConfig struct {
	BeaconAPI *string        `yaml:"beacon_api"`
	DBPath    *string        `yaml:"db"`
	Watch     fileWatch      `yaml:"watch"`
	Schedule  fileSchedule   `yaml:"schedule"`
	Threshold fileThresholds `yaml:"thresholds"`
}

type fileWatch struct {
	MinRequestInterval  *string   `yaml:"min_request_interval"`
	HostSampleInterval  *string   `yaml:"host_sample_interval"`
	CLMetricsAPI        *string   `yaml:"cl_metrics_api"`
	PeerSampleInterval  *string   `yaml:"peer_sample_interval"`
	BaselineBeaconAPI   *string   `yaml:"baseline_beacon_api"`
	BaselineMetricsAPI  *string   `yaml:"baseline_metrics_api"`
	NTPServers          *[]string `yaml:"ntp_servers"`
	ClockSampleInterval *string   `yaml:"clock_sample_interval"`
	RetentionMaxAge     *string   `yaml:"retention_max_age"`
	RetentionMaxBytes   *int64    `yaml:"retention_max_bytes"`
	RetentionInterval   *string   `yaml:"retention_interval"`
	ValidatorIndices    *[]uint64 `yaml:"validator_indices"`
	MetricsAddr         *string   `yaml:"metrics_addr"`
}

type fileSchedule struct {
	SecondsPerSlot      *string `yaml:"seconds_per_slot"`
	AttestationDeadline *string `yaml:"attestation_deadline"`
	AggregationDeadline *string `yaml:"aggregation_deadline"`
	// Both zero unless the network separates block from payload (EIP-7732).
	PayloadRevealDeadline *string `yaml:"payload_reveal_deadline"`
	PTCDeadline           *string `yaml:"ptc_deadline"`
}

type fileThresholds struct {
	Dominance             *float64 `yaml:"dominance"`
	ClockOffsetMax        *string  `yaml:"clock_offset_max"`
	ClockSampleMaxAge     *string  `yaml:"clock_sample_max_age"`
	NetworkDeviation      *string  `yaml:"network_deviation"`
	EngineSpikeMultiplier *float64 `yaml:"engine_spike_multiplier"`
	PeerCountMin          *float64 `yaml:"peer_count_min"`
	IOWaitPct             *float64 `yaml:"iowait_pct"`
	CPUStealPct           *float64 `yaml:"cpu_steal_pct"`
	PSIMemAvg10           *float64 `yaml:"psi_mem_avg10"`
}

func readFile(path string) (map[string]any, error) {
	f, err := os.Open(path) //nolint:gosec // the operator explicitly selects the config file
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only file; read/stat errors are the actionable failures
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect config file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config file %s is not a regular file", path)
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("config file %s is %d bytes, limit is %d", path, info.Size(), maxConfigBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("config file %s exceeds %d bytes", path, maxConfigBytes)
	}
	var raw fileConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode config file %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("decode config file %s: multiple YAML documents are not supported", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode config file %s: %w", path, err)
	}
	return fileConfigMap(raw), nil
}

func decode(k *koanf.Koanf) (Config, error) {
	var cfg Config
	cfg.BeaconAPI = k.String("beacon_api")
	cfg.DBPath = k.String("db")
	cfg.Watch.CLMetricsAPI = k.String("watch.cl_metrics_api")
	cfg.Watch.BaselineBeaconAPI = k.String("watch.baseline_beacon_api")
	cfg.Watch.BaselineMetricsAPI = k.String("watch.baseline_metrics_api")
	cfg.Watch.MetricsAddr = k.String("watch.metrics_addr")
	var err error
	cfg.Watch.NTPServers, err = stringsValue(k.Get("watch.ntp_servers"))
	if err != nil {
		return Config{}, fmt.Errorf("watch.ntp_servers: %w", err)
	}
	cfg.Watch.ValidatorIndices, err = uint64sValue(k.Get("watch.validator_indices"))
	if err != nil {
		return Config{}, fmt.Errorf("watch.validator_indices: %w", err)
	}
	for _, field := range []struct {
		key string
		dst *time.Duration
	}{
		{"watch.min_request_interval", &cfg.Watch.MinRequestInterval},
		{"watch.host_sample_interval", &cfg.Watch.HostSampleInterval},
		{"watch.peer_sample_interval", &cfg.Watch.PeerSampleInterval},
		{"watch.clock_sample_interval", &cfg.Watch.ClockSampleInterval},
		{"watch.retention_max_age", &cfg.Watch.RetentionMaxAge},
		{"watch.retention_interval", &cfg.Watch.RetentionInterval},
		{"schedule.seconds_per_slot", &cfg.Schedule.SecondsPerSlot},
		{"schedule.attestation_deadline", &cfg.Schedule.AttestationDeadline},
		{"schedule.aggregation_deadline", &cfg.Schedule.AggregationDeadline},
		// Default to "0s", which parses here and means pre-ePBS in the domain.
		{"schedule.payload_reveal_deadline", &cfg.Schedule.PayloadRevealDeadline},
		{"schedule.ptc_deadline", &cfg.Schedule.PTCDeadline},
		{"thresholds.clock_offset_max", &cfg.RCA.ClockOffsetMax},
		{"thresholds.clock_sample_max_age", &cfg.RCA.ClockSampleMaxAge},
		{"thresholds.network_deviation", &cfg.RCA.NetworkDeviation},
	} {
		*field.dst, err = time.ParseDuration(k.String(field.key))
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", field.key, err)
		}
	}
	cfg.Watch.RetentionMaxBytes, err = int64Value(k.Get("watch.retention_max_bytes"))
	if err != nil {
		return Config{}, fmt.Errorf("watch.retention_max_bytes: %w", err)
	}
	for _, field := range []struct {
		key string
		dst *float64
	}{
		{"thresholds.dominance", &cfg.RCA.Dominance},
		{"thresholds.engine_spike_multiplier", &cfg.RCA.EngineSpikeMultiplier},
		{"thresholds.peer_count_min", &cfg.RCA.PeerCountMin},
		{"thresholds.iowait_pct", &cfg.RCA.IOWaitPct},
		{"thresholds.cpu_steal_pct", &cfg.RCA.CPUStealPct},
		{"thresholds.psi_mem_avg10", &cfg.RCA.PSIMemAvg10},
	} {
		*field.dst, err = float64Value(k.Get(field.key))
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", field.key, err)
		}
	}
	return cfg, nil
}

func int64Value(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, fmt.Errorf("value %d overflows int64", v)
		}
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported value %T", value)
	}
}

func float64Value(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("unsupported value %T", value)
	}
}

func stringsValue(value any) ([]string, error) {
	switch v := value.(type) {
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				return nil, fmt.Errorf("contains an empty value")
			}
		}
		return append([]string(nil), v...), nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		parts := strings.Split(v, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
			if parts[i] == "" {
				return nil, fmt.Errorf("contains an empty value")
			}
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("unsupported value %T", value)
	}
}

func uint64sValue(value any) ([]uint64, error) {
	switch v := value.(type) {
	case []uint64:
		return append([]uint64(nil), v...), nil
	case []int:
		out := make([]uint64, 0, len(v))
		for _, item := range v {
			if item < 0 {
				return nil, fmt.Errorf("contains negative value %d", item)
			}
			out = append(out, uint64(item))
		}
		return out, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		var out []uint64
		for _, item := range strings.Split(v, ",") {
			parsed, err := strconv.ParseUint(strings.TrimSpace(item), 10, 64)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported value %T", value)
	}
}
