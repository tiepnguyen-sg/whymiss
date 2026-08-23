package beaconapi

import (
	"context"
	"fmt"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	maxHeadPollEntries = 8
	maxHeadPolls       = 64
	latestHeadCacheTTL = 200 * time.Millisecond
)

type headPollEntry struct {
	ready chan struct{}
	obs   domain.Observation
	found bool
	err   error
}

type latestHeadEntry struct {
	ready    chan struct{}
	header   blockHeader
	found    bool
	err      error
	complete bool
	fetched  time.Time
}

// latestCanonicalHead coalesces the inclusion-window poll shared by every
// tracked validator. Without it, up to three overlapping epochs of duties each
// poll /headers/head independently and exhaust the global request budget.
func (c *Client) latestCanonicalHead(ctx context.Context) (blockHeader, bool, error) {
	c.latestHeadMu.Lock()
	if entry := c.latestHead; entry != nil {
		if !entry.complete || time.Since(entry.fetched) < latestHeadCacheTTL {
			ready := entry.ready
			c.latestHeadMu.Unlock()
			select {
			case <-ctx.Done():
				return blockHeader{}, false, ctx.Err()
			case <-ready:
				return entry.header, entry.found, entry.err
			}
		}
	}
	entry := &latestHeadEntry{ready: make(chan struct{})}
	c.latestHead = entry
	c.latestHeadMu.Unlock()

	header, found, err := c.fetchHeader(ctx, "head")
	c.latestHeadMu.Lock()
	entry.header, entry.found, entry.err = header, found, err
	entry.complete, entry.fetched = true, time.Now()
	close(entry.ready)
	if err != nil && c.latestHead == entry {
		c.latestHead = nil
	}
	c.latestHeadMu.Unlock()
	return header, found, err
}

// HeadUpdated polls the canonical, execution-validated head until it reaches
// slot. It returns no observation if the head advances beyond slot before the
// poll observes it, because the exact update time is then unknowable.
func (c *Client) HeadUpdated(ctx context.Context, slot domain.Slot, deadline time.Time) (domain.Observation, bool, error) {
	key := uint64(slot)
	c.headMu.Lock()
	if entry, ok := c.headPolls[key]; ok {
		ready := entry.ready
		c.headMu.Unlock()
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-ready:
			return entry.obs, entry.found, entry.err
		}
	}
	if len(c.headPolls) >= maxHeadPolls {
		c.headMu.Unlock()
		return c.headUpdatedUncached(ctx, slot, deadline)
	}
	entry := &headPollEntry{ready: make(chan struct{})}
	c.headPolls[key] = entry
	c.headMu.Unlock()

	obs, found, err := c.headUpdatedUncached(ctx, slot, deadline)
	c.headMu.Lock()
	entry.obs, entry.found, entry.err = obs, found, err
	close(entry.ready)
	if err != nil {
		delete(c.headPolls, key)
	} else {
		c.trimHeadPollsLocked()
	}
	c.headMu.Unlock()
	return obs, found, err
}

func (c *Client) headUpdatedUncached(ctx context.Context, slot domain.Slot, deadline time.Time) (domain.Observation, bool, error) {
	const pollInterval = 200 * time.Millisecond
	for {
		header, found, err := c.fetchHeader(ctx, "head")
		if err != nil {
			return domain.Observation{}, false, err
		}
		if found {
			switch {
			case header.Slot == slot:
				obs, err := domain.NewObservation(domain.Observation{
					Slot: slot, Kind: domain.ObsHeadUpdated, At: time.Now().UTC(), Source: domain.SourceBeaconAPI,
					Attrs: map[domain.AttrKey]string{domain.AttrBlockRoot: header.Root},
				})
				if err != nil {
					return domain.Observation{}, false, fmt.Errorf("build head_updated observation for slot %d: %w", slot, err)
				}
				return obs, true, nil
			case header.Slot > slot:
				return domain.Observation{}, false, nil
			}
		}
		if !time.Now().Before(deadline) {
			return domain.Observation{}, false, nil
		}
		select {
		case <-ctx.Done():
			return domain.Observation{}, false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func (c *Client) trimHeadPollsLocked() {
	for len(c.headPolls) > maxHeadPollEntries {
		var oldest uint64
		haveOldest := false
		for slot, entry := range c.headPolls {
			select {
			case <-entry.ready:
				if !haveOldest || slot < oldest {
					oldest, haveOldest = slot, true
				}
			default:
			}
		}
		if !haveOldest {
			return
		}
		delete(c.headPolls, oldest)
	}
}
