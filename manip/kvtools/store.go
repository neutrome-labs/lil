package kvtools

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a cached value does not exist.
var ErrNotFound = errors.New("kvtools: key not found")

// Store is the cache backend interface used by KVTools.
//
// Consumers can provide Redis, database, object-store, or process-local
// implementations without depending on any router framework.
type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
