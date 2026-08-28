package main

import (
	"fmt"
	"sync"
	"time"
)

type requestReplayCache struct {
	mu    sync.Mutex
	items map[string]time.Time
	now   func() time.Time
}

func newRequestReplayCache(now func() time.Time) *requestReplayCache {
	if now == nil {
		now = time.Now
	}
	return &requestReplayCache{
		items: make(map[string]time.Time),
		now:   now,
	}
}

func (c *requestReplayCache) claim(meta *Meta) bool {
	if c == nil || meta == nil {
		return false
	}
	now := c.now()
	expiresAt := time.Unix(meta.Timestamp, 0).Add(metaTimestampWindow)
	if !expiresAt.After(now) {
		return false
	}
	key := fmt.Sprintf("%d/%d/%s/%s", meta.UserID, meta.ConversationID, meta.RequestID, meta.CallID)

	c.mu.Lock()
	defer c.mu.Unlock()
	for candidate, expiry := range c.items {
		if !expiry.After(now) {
			delete(c.items, candidate)
		}
	}
	if expiry, exists := c.items[key]; exists && expiry.After(now) {
		return false
	}
	c.items[key] = expiresAt
	return true
}
