package manip

import (
	"context"
	"sync"
	"time"
)

// DefaultStoreMaxItems is the default capacity used by NewMemoryStore.
const DefaultStoreMaxItems = 10000

// MemoryStore is an in-memory Store with TTL and simple oldest-entry eviction.
type MemoryStore struct {
	mu         sync.RWMutex
	data       map[string]memoryEntry
	order      []string
	maxItems   int
	defaultTTL time.Duration
}

type memoryEntry struct {
	value     string
	expiresAt time.Time
}

// NewMemoryStore creates an in-memory store backend. A maxItems value <= 0 uses
// DefaultStoreMaxItems. defaultTTL is used when Set receives ttl == 0.
func NewMemoryStore(maxItems int, defaultTTL time.Duration) *MemoryStore {
	if maxItems <= 0 {
		maxItems = DefaultStoreMaxItems
	}
	return &MemoryStore{
		data:       make(map[string]memoryEntry, maxItems),
		order:      make([]string, 0, maxItems),
		maxItems:   maxItems,
		defaultTTL: defaultTTL,
	}
}

// Get retrieves a stored value.
func (m *MemoryStore) Get(_ context.Context, key string) (string, error) {
	m.mu.RLock()
	entry, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		m.mu.Lock()
		delete(m.data, key)
		m.mu.Unlock()
		return "", ErrNotFound
	}
	return entry.value, nil
}

// Set stores a value.
func (m *MemoryStore) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ttl == 0 {
		ttl = m.defaultTTL
	}
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	if _, exists := m.data[key]; !exists {
		m.evictUntilSpace()
		m.order = append(m.order, key)
	}
	m.data[key] = memoryEntry{value: value, expiresAt: expiresAt}
	return nil
}

// Delete removes a stored value.
func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *MemoryStore) evictUntilSpace() {
	for len(m.data) >= m.maxItems && len(m.order) > 0 {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.data, oldest)
	}
}
