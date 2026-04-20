package memorymonitor

import (
	"github.com/rymdport/portal/internal/apis"
)

// LowMemoryWarning contains the information given in such a warning.
type LowMemoryWarning struct {
	Level byte // Representing the level of low memory warning.
}

// OnSignalLowMemoryWarning listens for the LowMemoryWarning signal.
// Signal is emitted when a particular low memory situation happens,
// with 0 being the lowest level of memory availability warning,
// and 255 being the highest.
//
// This function blocks for the lifetime of the subscription; the
// subscription is released only when the process exits.
func OnSignalLowMemoryWarning(callback func(warning LowMemoryWarning)) error {
	signal, _, err := apis.ListenOnSignal(interfaceName, "LowMemoryWarning")
	if err != nil {
		return err
	}

	for sig := range signal {
		if len(sig.Body) == 0 {
			continue
		}

		level, ok := sig.Body[0].(byte)
		if !ok {
			continue
		}

		callback(LowMemoryWarning{Level: level})
	}

	return nil
}
