package beaconapi

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tiepnguyen-sg/whymiss/internal/domain"
)

const (
	maxCanonicalRootLookback = 64
	maxCanonicalRootCache    = 16
	maxCanonicalRootFetches  = 64
	canonicalRootCacheTTL    = 2 * time.Second
)

type canonicalRootEntry struct {
	ready    chan struct{}
	root     string
	err      error
	complete bool
	fetched  time.Time
}

func (c *Client) canonicalRootAtSlot(ctx context.Context, slot domain.Slot) (root string, err error) {
	key := uint64(slot)
	c.rootMu.Lock()
	if entry, ok := c.canonicalRoots[key]; ok {
		if entry.complete && time.Since(entry.fetched) >= canonicalRootCacheTTL {
			delete(c.canonicalRoots, key)
		} else {
			ready := entry.ready
			c.rootMu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-ready:
				return entry.root, entry.err
			}
		}
	}
	if len(c.canonicalRoots) >= maxCanonicalRootFetches {
		c.rootMu.Unlock()
		return c.fetchCanonicalRootAtSlot(ctx, slot)
	}
	entry := &canonicalRootEntry{ready: make(chan struct{})}
	c.canonicalRoots[key] = entry
	c.rootMu.Unlock()

	root, err = c.fetchCanonicalRootAtSlot(ctx, slot)
	c.rootMu.Lock()
	entry.root, entry.err, entry.complete, entry.fetched = root, err, true, time.Now()
	close(entry.ready)
	if err != nil {
		delete(c.canonicalRoots, key)
	} else {
		c.trimCanonicalRootCacheLocked()
	}
	c.rootMu.Unlock()
	return root, err
}

func (c *Client) fetchCanonicalRootAtSlot(ctx context.Context, slot domain.Slot) (string, error) {
	for lookback := domain.Slot(0); lookback <= maxCanonicalRootLookback && lookback <= slot; lookback++ {
		header, found, err := c.fetchBlockHeader(ctx, slot-lookback)
		if err != nil {
			return "", err
		}
		if !found {
			continue
		}
		if err := validateBeaconRoot(header.Root); err != nil {
			return "", fmt.Errorf("canonical root at or before slot %d: %w", slot, err)
		}
		return header.Root, nil
	}
	return "", fmt.Errorf("no canonical block found within %d slots at or before slot %d", maxCanonicalRootLookback, slot)
}

func (c *Client) trimCanonicalRootCacheLocked() {
	for len(c.canonicalRoots) > maxCanonicalRootCache {
		var oldest uint64
		haveOldest := false
		for slot, entry := range c.canonicalRoots {
			if entry.complete && (!haveOldest || slot < oldest) {
				oldest, haveOldest = slot, true
			}
		}
		if !haveOldest {
			return
		}
		delete(c.canonicalRoots, oldest)
	}
}

func (c *Client) attestationRewardEvidence(ctx context.Context, dutySlot domain.Slot, att apiAttestation) (headCorrect, targetCorrect bool, err error) {
	if err := validateBeaconRoot(att.Data.BeaconBlockRoot); err != nil {
		return false, false, fmt.Errorf("attested head root: %w", err)
	}
	if err := validateBeaconRoot(att.Data.Target.Root); err != nil {
		return false, false, fmt.Errorf("attested target root: %w", err)
	}
	targetEpoch, err := strconv.ParseUint(att.Data.Target.Epoch, 10, 64)
	if err != nil {
		return false, false, fmt.Errorf("parse attestation target epoch %q: %w", att.Data.Target.Epoch, err)
	}
	if domain.Epoch(targetEpoch) != dutySlot.Epoch() {
		return false, false, fmt.Errorf("attestation target epoch %d does not match duty slot %d epoch %d", targetEpoch, dutySlot, dutySlot.Epoch())
	}

	canonicalHead, err := c.canonicalRootAtSlot(ctx, dutySlot)
	if err != nil {
		return false, false, fmt.Errorf("resolve canonical head for duty slot %d: %w", dutySlot, err)
	}
	canonicalTarget, err := c.canonicalRootAtSlot(ctx, domain.Epoch(targetEpoch).FirstSlot())
	if err != nil {
		return false, false, fmt.Errorf("resolve canonical target for epoch %d: %w", targetEpoch, err)
	}
	return att.Data.BeaconBlockRoot == canonicalHead, att.Data.Target.Root == canonicalTarget, nil
}

func validateBeaconRoot(root string) error {
	if !strings.HasPrefix(root, "0x") || len(root) != 66 {
		return fmt.Errorf("root %q is not 32-byte 0x-prefixed hex", root)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(root, "0x")); err != nil {
		return fmt.Errorf("decode root %q: %w", root, err)
	}
	return nil
}
