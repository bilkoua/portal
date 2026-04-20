// Package request implements the Request interface shared by portal methods
// that return a handle and emit a Response signal asynchronously.
package request

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/rymdport/portal"
	"github.com/rymdport/portal/internal/apis"
)

// https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Request.html
const (
	interfaceName  = "org.freedesktop.portal.Request"
	responseMember = "Response"
	closeCallName  = interfaceName + ".Close"

	requestPathPrefix = "/org/freedesktop/portal/desktop/request/"
)

// ResponseStatus of a portal Response signal.
type ResponseStatus = uint32

const (
	Success   ResponseStatus = 0
	Cancelled ResponseStatus = 1
	Ended     ResponseStatus = 2 // closed by the system or other non-user reason
)

// Response is the payload of a portal Request's Response signal.
type Response struct {
	Handle  dbus.ObjectPath
	Status  ResponseStatus
	Results map[string]dbus.Variant
}

// Close closes the portal Request at path.
func Close(path dbus.ObjectPath) error {
	return apis.CallOnObject(path, closeCallName)
}

func generateToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("rymdport/portal: crypto/rand failed: " + err.Error())
	}
	return "rymdportal" + hex.EncodeToString(b[:])
}

// buildRequestPath is the Request path the portal will use for a call
// made by sender with the given handle_token. See the spec link above.
func buildRequestPath(sender, token string) dbus.ObjectPath {
	if sender == "" {
		return ""
	}
	sender = strings.TrimPrefix(sender, ":")
	sender = strings.ReplaceAll(sender, ".", "_")
	return dbus.ObjectPath(requestPathPrefix + sender + "/" + token)
}

func expectedHandle(conn *dbus.Conn, token string) dbus.ObjectPath {
	names := conn.Names()
	if len(names) == 0 {
		return ""
	}
	return buildRequestPath(names[0], token)
}

// SendRequest dispatches a portal method that returns a Request handle and
// waits for its Response signal. buildArgs receives the handle_token to
// write into the call's options map; pass token="" to generate one.
//
// The Response subscription is installed before the call to avoid missing
// the signal on fast backends. Cancelling ctx asks the portal to close the
// Request (best-effort).
func SendRequest(
	ctx context.Context,
	token, callName string,
	buildArgs func(token string) []any,
) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Response{Status: Ended}, err
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		return Response{Status: Ended}, err
	}

	if token == "" {
		token = generateToken()
	}
	expected := expectedHandle(conn, token)

	signal, cleanup, err := apis.ListenOnSignalAt(expected, interfaceName, responseMember)
	if err != nil {
		return Response{Status: Ended}, err
	}
	defer cleanup()

	call := conn.Object(apis.ObjectName, apis.ObjectPath).Call(callName, 0, buildArgs(token)...)
	if call.Err != nil {
		return Response{Status: Ended}, call.Err
	}

	var handle dbus.ObjectPath
	if err := call.Store(&handle); err != nil {
		return Response{Status: Ended}, err
	}

	var sig *dbus.Signal
	select {
	case <-ctx.Done():
		go func() { _ = Close(handle) }()
		return Response{Handle: handle, Status: Ended}, ctx.Err()
	case sig = <-signal:
	}

	if len(sig.Body) != 2 {
		return Response{Handle: handle, Status: Ended}, portal.ErrUnexpectedResponse
	}
	status, ok := sig.Body[0].(ResponseStatus)
	if !ok {
		return Response{Handle: handle, Status: Ended}, portal.ErrUnexpectedResponse
	}
	results, ok := sig.Body[1].(map[string]dbus.Variant)
	if !ok {
		return Response{Handle: handle, Status: Ended}, portal.ErrUnexpectedResponse
	}
	return Response{Handle: handle, Status: status, Results: results}, nil
}
