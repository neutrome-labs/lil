package manip

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a stored value does not exist.
var ErrNotFound = errors.New("ail/manip: key not found")

// Store is a small key/value backend interface shared by manips and runtime
// transforms that need request-scoped persistence.
//
// Implementations can wrap Redis, databases, object stores, or process-local
// caches without depending on any router framework.
type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
