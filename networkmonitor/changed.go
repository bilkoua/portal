package networkmonitor

import (
	"github.com/rymdport/portal/internal/apis"
)

// OnSignalChanged calls the passed function when the network configuration changes.
//
// This function blocks for the lifetime of the subscription; the subscription
// is released only when the process exits.
func OnSignalChanged(callback func()) error {
	signal, _, err := apis.ListenOnSignal(interfaceName, "changed")
	if err != nil {
		return err
	}

	for range signal {
		callback()
	}

	return nil
}
