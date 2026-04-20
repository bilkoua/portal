package apis

import (
	"sync"

	"github.com/godbus/dbus/v5"
)

// Signal routing. godbus delivers every signal on the connection to every
// channel registered via conn.Signal, which is wasteful for portal workloads
// and prone to leaks when callers forget conn.RemoveSignal. We register one
// channel with godbus and fan out here, keyed by (path, interface.member).

type routerKey struct {
	path dbus.ObjectPath
	name string // interface.member
}

var (
	routerInitMu sync.Mutex
	routerReady  bool

	routerMu   sync.Mutex
	routerSubs = map[routerKey][]chan<- *dbus.Signal{}
)

func ensureRouter() error {
	routerInitMu.Lock()
	defer routerInitMu.Unlock()
	if routerReady {
		return nil
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}

	all := make(chan *dbus.Signal, 256)
	conn.Signal(all)
	go routerLoop(all)

	routerReady = true
	return nil
}

func routerLoop(in <-chan *dbus.Signal) {
	for sig := range in {
		key := routerKey{path: sig.Path, name: sig.Name}

		// snapshot the subscriber list so we don't hold the lock while sending
		routerMu.Lock()
		targets := append([]chan<- *dbus.Signal(nil), routerSubs[key]...)
		routerMu.Unlock()

		for _, ch := range targets {
			select {
			case ch <- sig:
			default: // slow subscriber, drop
			}
		}
	}
}

func registerSubscriber(key routerKey, ch chan<- *dbus.Signal) (cleanup func()) {
	routerMu.Lock()
	routerSubs[key] = append(routerSubs[key], ch)
	routerMu.Unlock()

	return func() {
		routerMu.Lock()
		defer routerMu.Unlock()
		subs := routerSubs[key]
		for i, c := range subs {
			if c == ch {
				routerSubs[key] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(routerSubs[key]) == 0 {
			delete(routerSubs, key)
		}
	}
}

// SubscribeSignal returns a channel that receives signals matching
// (path, interface.member), and a cleanup to unsubscribe. The caller is
// still responsible for AddMatchSignal/RemoveMatchSignal so the bus
// forwards the signals to this connection.
func SubscribeSignal(path dbus.ObjectPath, interfaceName, memberName string) (<-chan *dbus.Signal, func(), error) {
	if err := ensureRouter(); err != nil {
		return nil, nil, err
	}
	key := routerKey{path: path, name: interfaceName + "." + memberName}
	ch := make(chan *dbus.Signal, 4)
	return ch, registerSubscriber(key, ch), nil
}
