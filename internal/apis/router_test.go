package apis

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func resetRouter(t *testing.T) {
	t.Helper()
	routerMu.Lock()
	defer routerMu.Unlock()
	routerSubs = map[routerKey][]chan<- *dbus.Signal{}
}

func k(path dbus.ObjectPath, iface, member string) routerKey {
	return routerKey{path: path, name: iface + "." + member}
}

// drive runs routerLoop against a set of incoming signals and blocks until it
// drains. Helper so each test reads as a flat sequence.
func drive(t *testing.T, sigs ...*dbus.Signal) {
	t.Helper()
	in := make(chan *dbus.Signal, len(sigs))
	for _, s := range sigs {
		in <- s
	}
	close(in)
	done := make(chan struct{})
	go func() { routerLoop(in); close(done) }()
	<-done
}

func TestRouter_DeliversMatchingSignal(t *testing.T) {
	resetRouter(t)

	ch := make(chan *dbus.Signal, 1)
	defer registerSubscriber(k("/p", "i.f", "M"), ch)()

	drive(t, &dbus.Signal{Path: "/p", Name: "i.f.M", Body: []any{"hi"}})

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("signal not delivered")
	}
}

// The router must filter by the full (path, interface.member) tuple; before
// this change memorymonitor and settings were cross-delivering each other's
// signals because they share the portal base path.
func TestRouter_FiltersByFullKey(t *testing.T) {
	resetRouter(t)

	ch := make(chan *dbus.Signal, 4)
	defer registerSubscriber(k("/a", "i.f", "M1"), ch)()

	drive(t,
		&dbus.Signal{Path: "/a", Name: "i.f.M2"},   // wrong member
		&dbus.Signal{Path: "/a", Name: "other.M1"}, // wrong interface
		&dbus.Signal{Path: "/b", Name: "i.f.M1"},   // wrong path
		&dbus.Signal{Path: "/a", Name: "i.f.M1"},   // match
	)

	if got := len(ch); got != 1 {
		t.Fatalf("got %d signals, want 1", got)
	}
}

func TestRouter_CleanupRemovesOnlyOwnChannel(t *testing.T) {
	resetRouter(t)

	ch1 := make(chan *dbus.Signal, 1)
	ch2 := make(chan *dbus.Signal, 1)
	cleanup1 := registerSubscriber(k("/p", "i.f", "M"), ch1)
	defer registerSubscriber(k("/p", "i.f", "M"), ch2)()

	cleanup1()

	drive(t, &dbus.Signal{Path: "/p", Name: "i.f.M"})

	if len(ch1) != 0 {
		t.Fatal("ch1 should be unsubscribed")
	}
	if len(ch2) != 1 {
		t.Fatal("ch2 should still receive")
	}
}

// Cleanup is invoked both explicitly on error paths and via defer, so it must
// tolerate repeated calls.
func TestRouter_CleanupIsIdempotent(t *testing.T) {
	resetRouter(t)

	cleanup := registerSubscriber(k("/p", "i.f", "M"), make(chan *dbus.Signal, 1))
	cleanup()
	cleanup()
}

// A slow subscriber must not block the router or starve other subscribers;
// routerLoop uses a non-blocking send.
func TestRouter_SlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	resetRouter(t)

	slow := make(chan *dbus.Signal) // unbuffered, never read
	fast := make(chan *dbus.Signal, 4)
	defer registerSubscriber(k("/p", "i.f", "M"), slow)()
	defer registerSubscriber(k("/p", "i.f", "M"), fast)()

	drive(t,
		&dbus.Signal{Path: "/p", Name: "i.f.M"},
		&dbus.Signal{Path: "/p", Name: "i.f.M"},
		&dbus.Signal{Path: "/p", Name: "i.f.M"},
	)

	if len(fast) != 3 {
		t.Fatalf("fast: got %d, want 3", len(fast))
	}
	if len(slow) != 0 {
		t.Fatalf("slow: got %d, want 0 (dropped)", len(slow))
	}
}
