package beaconapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

// streamTopics are the SSE topics this package subscribes to. "head" and
// "chain_reorg" are the two events without a clean REST polling substitute
// (see blocks.go's BlockSeen and attestations.go's AttestationPublished doc
// comments for the topics that do have one, and why polling was chosen for
// those instead). Adding a topic here means adding the domain.ObservationKind
// it produces to parseEvent below.
const streamTopics = "head,chain_reorg"

// Stream connects to GET /eth/v1/events and returns a channel of parsed
// observations from the "head" and "chain_reorg" topics. It reconnects with
// backoff (I-5) on any disconnect or read error and keeps retrying
// indefinitely — this is the collector's long-lived connection, meant to
// survive a node restart or a network blip without operator intervention —
// until ctx is done, at which point the channel is closed.
//
// onError is called, if non-nil, with each connection or parse error before
// Stream retries. It is the only way a caller observes a transient failure;
// Stream itself never gives up and never returns an error, since "runs
// beside a node for days without being noticed" (BUILD_PROMPT.md §10.1)
// means a caller not watching for errors should still keep working once the
// underlying problem clears.
func (c *Client) Stream(ctx context.Context, onError func(error)) <-chan domain.Observation {
	out := make(chan domain.Observation)
	go func() {
		defer close(out)
		attempt := 0
		for {
			delivered, err := c.streamOnce(ctx, out)
			if ctx.Err() != nil {
				return
			}
			if err != nil && onError != nil {
				onError(err)
			}
			if delivered {
				attempt = 0
			}
			if err := sleepReconnect(ctx, attempt); err != nil {
				return
			}
			attempt++
		}
	}()
	return out
}

// streamOnce holds one SSE connection open until it errors or ctx is done.
// A clean read to EOF with no error (the node closing the stream normally)
// is treated the same as a transient error: the caller reconnects either way,
// since this connection is meant to be permanent for as long as ctx allows.
func (c *Client) streamOnce(ctx context.Context, out chan<- domain.Observation) (bool, error) {
	if err := c.limiter.wait(ctx); err != nil {
		return false, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := time.AfterFunc(c.streamIdleTimeout, cancel)
	defer idle.Stop()

	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet,
		c.baseURL+"/eth/v1/events?topics="+streamTopics, nil)
	if err != nil {
		return false, fmt.Errorf("build event stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.streamHTTP.Do(req) //nolint:bodyclose // closed via defer below
	if err != nil {
		return false, fmt.Errorf("connect to event stream: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response body; nothing to act on if Close fails
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("connect to event stream: unexpected status %d", resp.StatusCode)
	}

	var eventType string
	delivered := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		idle.Reset(c.streamIdleTimeout)
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			obs, ok, err := parseEvent(eventType, data)
			if err != nil {
				return delivered, fmt.Errorf("parse %s event: %w", eventType, err)
			}
			if !ok {
				continue
			}
			select {
			case out <- obs:
				delivered = true
			case <-ctx.Done():
				return delivered, ctx.Err()
			}
		case line == "":
			eventType = "" // blank line ends one SSE message; next event: line starts fresh
		}
	}
	if streamCtx.Err() != nil && ctx.Err() == nil {
		return delivered, fmt.Errorf("event stream was idle for %s", c.streamIdleTimeout)
	}
	if err := scanner.Err(); err != nil {
		return delivered, fmt.Errorf("read event stream: %w", err)
	}
	return delivered, fmt.Errorf("event stream closed by node")
}

// parseEvent turns one SSE (eventType, data) pair into a domain.Observation.
// ok is false for an eventType this package does not subscribe to — should
// not happen given streamTopics, but a node is free to send more than
// requested, and silently skipping is safer than erroring on it.
func parseEvent(eventType, data string) (domain.Observation, bool, error) {
	switch eventType {
	case "head":
		var payload struct {
			Slot                string `json:"slot"`
			Block               string `json:"block"`
			ExecutionOptimistic *bool  `json:"execution_optimistic"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return domain.Observation{}, false, fmt.Errorf("decode head payload: %w", err)
		}
		slot, err := strconv.ParseUint(payload.Slot, 10, 64)
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("parse head slot %q: %w", payload.Slot, err)
		}
		if err := validateBeaconRoot(payload.Block); err != nil {
			return domain.Observation{}, false, fmt.Errorf("parse head block root: %w", err)
		}
		if payload.ExecutionOptimistic == nil {
			return domain.Observation{}, false, fmt.Errorf("head payload has no execution_optimistic status")
		}
		if *payload.ExecutionOptimistic {
			return domain.Observation{}, false, nil
		}
		obs, err := domain.NewObservation(domain.Observation{
			Slot:   domain.Slot(slot),
			Kind:   domain.ObsHeadUpdated,
			At:     time.Now().UTC(),
			Source: domain.SourceBeaconAPI,
			Attrs:  map[domain.AttrKey]string{domain.AttrBlockRoot: payload.Block},
		})
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("build head_updated observation: %w", err)
		}
		return obs, true, nil

	case "chain_reorg":
		var payload struct {
			Slot string `json:"slot"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return domain.Observation{}, false, fmt.Errorf("decode chain_reorg payload: %w", err)
		}
		slot, err := strconv.ParseUint(payload.Slot, 10, 64)
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("parse chain_reorg slot %q: %w", payload.Slot, err)
		}
		obs, err := domain.NewObservation(domain.Observation{
			Slot:   domain.Slot(slot),
			Kind:   domain.ObsReorg,
			At:     time.Now().UTC(),
			Source: domain.SourceBeaconAPI,
		})
		if err != nil {
			return domain.Observation{}, false, fmt.Errorf("build reorg observation: %w", err)
		}
		return obs, true, nil

	default:
		return domain.Observation{}, false, nil
	}
}
