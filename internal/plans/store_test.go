package plans

import (
	"errors"
	"testing"
	"time"
)

func TestPlanIsSingleUseAndBoundToFingerprint(t *testing.T) {
	store := NewStore(time.Minute)
	plan, err := store.Create("ВЫБРАТЬ 1", nil, 1, "schema-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Consume(plan.ID, "schema-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong fingerprint error = %v", err)
	}
	if _, err = store.Consume(plan.ID, "schema-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Consume(plan.ID, "schema-a"); !errors.Is(err, ErrUsed) {
		t.Fatalf("second consume error = %v", err)
	}
}

func TestPlanExpires(t *testing.T) {
	store := NewStore(time.Second)
	now := time.Unix(1_000, 0)
	store.now = func() time.Time { return now }
	plan, err := store.Create("ВЫБРАТЬ 1", nil, 1, "schema", nil)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err = store.Consume(plan.ID, "schema"); !errors.Is(err, ErrExpired) {
		t.Fatalf("consume error = %v", err)
	}
}
