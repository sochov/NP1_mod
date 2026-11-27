package numbershare

import (
	"testing"
	"time"
)

func TestUnsentFragmentRetiredWhenRegistrationRejoins(t *testing.T) {
	for _, inFlight := range []bool{false, true} {
		s := New(t.TempDir(), nil)
		at := time.Now().Add(-2 * time.Minute)
		configureTest(t, s, "https://example.test/")
		first := testRecord(at)
		first.ID = "pt-first-1-0"
		second := testRecord(at.Add(time.Second))
		second.ID = "pt-next-1-0"
		if err := s.Enqueue(second); err != nil {
			t.Fatal(err)
		}
		if inFlight {
			if _, _, ok := s.nextDue(); !ok {
				t.Fatal("expected a due event")
			}
		}
		if err := s.EnqueueTrack(first, second.ID); err != nil {
			t.Fatal(err)
		}
		items := s.Status().Items
		if len(items) != 1 && !inFlight || len(items) != 2 && inFlight || items[0].RecordID != first.ID {
			t.Fatalf("inflight=%t queue=%+v", inFlight, items)
		}
	}
}
