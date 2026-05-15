package manip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/neutrome-labs/ail/manip"
)

func TestMemoryStoreGetSetDelete(t *testing.T) {
	store := manip.NewMemoryStore(10, time.Hour)
	ctx := context.Background()

	if err := store.Set(ctx, "key", "value", time.Hour); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := store.Get(ctx, "key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "value" {
		t.Fatalf("value = %q", got)
	}

	if err := store.Delete(ctx, "key"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, "key"); !errors.Is(err, manip.ErrNotFound) {
		t.Fatalf("deleted key err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreExpiresValues(t *testing.T) {
	store := manip.NewMemoryStore(10, time.Millisecond)
	ctx := context.Background()

	if err := store.Set(ctx, "key", "value", 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.Get(ctx, "key"); !errors.Is(err, manip.ErrNotFound) {
		t.Fatalf("expired key err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreEvictsOldest(t *testing.T) {
	store := manip.NewMemoryStore(1, time.Hour)
	ctx := context.Background()

	if err := store.Set(ctx, "old", "old-value", time.Hour); err != nil {
		t.Fatalf("set old: %v", err)
	}
	if err := store.Set(ctx, "new", "new-value", time.Hour); err != nil {
		t.Fatalf("set new: %v", err)
	}
	if _, err := store.Get(ctx, "old"); !errors.Is(err, manip.ErrNotFound) {
		t.Fatalf("old key err = %v, want ErrNotFound", err)
	}
	got, err := store.Get(ctx, "new")
	if err != nil {
		t.Fatalf("get new: %v", err)
	}
	if got != "new-value" {
		t.Fatalf("new value = %q", got)
	}
}
