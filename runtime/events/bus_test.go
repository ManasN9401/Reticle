package events

import (
	"sync"
	"testing"
	"time"
)

func TestOrderedReentrantDeliveryAndUnsubscribe(t *testing.T) {
	b := NewBus("test")
	var mu sync.Mutex
	var ids []EventID
	b.SubscribeAll(func(e RuntimeEvent) { mu.Lock(); ids = append(ids, e.ID); mu.Unlock() })
	done := make(chan struct{})
	b.Subscribe("a", func(e RuntimeEvent) { b.Publish("b", "test", nil) })
	b.Subscribe("b", func(e RuntimeEvent) { close(done) })
	stop := b.Subscribe("a", func(e RuntimeEvent) { t.Error("removed handler fired") })
	stop()
	b.Publish("a", "test", nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reentrant publish blocked")
	}
	b.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || ids[0] >= ids[1] {
		t.Fatalf("unordered events %v", ids)
	}
}

func TestOverflowFailsExplicitlyWithoutDeadlocking(t *testing.T) {
	b := NewBus("fixture")
	defer b.Close()
	observed := make(chan struct{})
	b.Subscribe("RuntimeOverloaded", func(events RuntimeEvent) { close(observed) })
	b.Subscribe("fill", func(RuntimeEvent) {
		for i := 0; i < 5000; i++ {
			b.Publish("required", "fixture", nil)
		}
	})
	b.Publish("fill", "fixture", nil)
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("overflow deadlocked")
	}
	if b.Accepting() {
		t.Fatal("overloaded bus admitted new work")
	}
	b.mu.Lock()
	count := len(b.queue)
	b.mu.Unlock()
	if count > 4096 {
		t.Fatal("unbounded queue")
	}
}

func TestPanickingSubscriberIsQuarantined(t *testing.T) {
	b := NewBus("fixture")
	defer b.Close()
	reported := make(chan RuntimeEvent, 1)
	survived := make(chan struct{}, 2)
	b.Subscribe("work", func(RuntimeEvent) { panic("fixture panic") })
	b.Subscribe("work", func(RuntimeEvent) { survived <- struct{}{} })
	b.Subscribe("SubscriberPanicked", func(event RuntimeEvent) { reported <- event })
	b.Publish("work", "test", nil)
	select {
	case event := <-reported:
		payload := event.Payload.(map[string]any)
		if payload["event_type"] != "work" || payload["error"] != "subscriber panicked" {
			t.Fatalf("bad panic report: %#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber panic stopped dispatch")
	}
	b.Publish("work", "test", nil)
	for i := 0; i < 2; i++ {
		select {
		case <-survived:
		case <-time.After(time.Second):
			t.Fatal("surviving subscriber stopped receiving events")
		}
	}
}
