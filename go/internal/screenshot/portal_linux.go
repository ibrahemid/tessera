//go:build linux

package screenshot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/ibrahemid/tessera/go/internal/dbusx"
)

const (
	portalName      = "org.freedesktop.portal.Desktop"
	portalPath      = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	screenshotIface = "org.freedesktop.portal.Screenshot"
	requestIface    = "org.freedesktop.portal.Request"
	requestPrefix   = "/org/freedesktop/portal/desktop/request/"
	portalTool      = "the desktop portal"
)

const (
	responseSuccess   = 0
	responseCancelled = 1
)

// portalCallTimeout bounds each portal method call, which may start the portal.
const portalCallTimeout = 10 * time.Second

// errNoPortal reports that the session has no screenshot portal to ask.
var errNoPortal = errors.New("screenshot: no desktop screenshot portal")

// capturePortal runs an interactive org.freedesktop.portal.Screenshot request
// and returns the regular file its response names.
func capturePortal(ctx context.Context) (string, error) {
	conn, err := dbusx.Connect(ctx)
	if err != nil {
		return "", noPortalUnlessDone(ctx)
	}
	defer conn.Close()

	owner, err := portalOwner(ctx, conn)
	if err != nil {
		return "", err
	}
	token, err := handleToken()
	if err != nil {
		return "", &CaptureError{Tool: portalTool, Err: err}
	}
	reqPath := requestPath(conn.Names()[0], token)
	watch, err := dbusx.WatchSignal(conn, reqPath, requestIface, "Response")
	if err != nil {
		return "", portalFailure(ctx, err)
	}
	defer func() { watch.Close() }()

	opts := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"interactive":  dbus.MakeVariant(true),
		"modal":        dbus.MakeVariant(true),
	}
	callCtx, cancel := context.WithTimeout(ctx, portalCallTimeout)
	var handle dbus.ObjectPath
	err = conn.Object(portalName, portalPath).CallWithContext(callCtx, screenshotIface+".Screenshot", 0, "", opts).Store(&handle)
	cancel()
	if err != nil {
		return "", portalFailure(ctx, err)
	}
	if handle != reqPath {
		moved, err := dbusx.WatchSignal(conn, handle, requestIface, "Response")
		if err != nil {
			return "", portalFailure(ctx, err)
		}
		watch.Close()
		watch = moved
	}
	for {
		sig, err := watch.Next(ctx)
		if err != nil {
			return "", portalFailure(ctx, err)
		}
		if sig.Sender == owner {
			return portalResult(sig.Body)
		}
	}
}

// portalOwner returns the unique bus name of the portal when it serves the
// Screenshot interface, and errNoPortal when it does not.
func portalOwner(ctx context.Context, conn *dbus.Conn) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, portalCallTimeout)
	defer cancel()
	if has, err := dbusx.HasName(callCtx, conn, portalName); err != nil || !has {
		return "", noPortalUnlessDone(ctx)
	}
	var version uint32
	if err := conn.Object(portalName, portalPath).CallWithContext(callCtx, "org.freedesktop.DBus.Properties.Get", 0, screenshotIface, "version").Store(&version); err != nil {
		return "", noPortalUnlessDone(ctx)
	}
	var owner string
	if err := conn.BusObject().CallWithContext(callCtx, "org.freedesktop.DBus.GetNameOwner", 0, portalName).Store(&owner); err != nil {
		return "", noPortalUnlessDone(ctx)
	}
	return owner, nil
}

func noPortalUnlessDone(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	return errNoPortal
}

func portalFailure(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("screenshot: %w", ctxErr)
	}
	return &CaptureError{Tool: portalTool, Err: err}
}

func handleToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "tessera_" + hex.EncodeToString(b[:]), nil
}

// requestPath predicts the Request object the portal creates for a call from
// the connection named sender with the given handle_token.
func requestPath(sender, token string) dbus.ObjectPath {
	s := strings.ReplaceAll(strings.TrimPrefix(sender, ":"), ".", "_")
	return dbus.ObjectPath(requestPrefix + s + "/" + token)
}

func portalResult(body []any) (string, error) {
	if len(body) != 2 {
		return "", &CaptureError{Tool: portalTool, Detail: "the response was malformed"}
	}
	code, ok := body[0].(uint32)
	results, resultsOK := body[1].(map[string]dbus.Variant)
	if !ok || !resultsOK {
		return "", &CaptureError{Tool: portalTool, Detail: "the response was malformed"}
	}
	switch code {
	case responseSuccess:
		uri, _ := results["uri"].Value().(string)
		return fileFromURI(uri)
	case responseCancelled:
		return "", ErrCancelled
	}
	return "", &CaptureError{Tool: portalTool, Detail: fmt.Sprintf("the request ended with response code %d", code)}
}

// fileFromURI returns the local path a file:// URI names when that path is a
// regular file owned by this user.
func fileFromURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || u.Opaque != "" || (u.Host != "" && u.Host != "localhost") || !filepath.IsAbs(u.Path) {
		return "", &CaptureError{Tool: portalTool, Detail: "the screenshot location is not a local file"}
	}
	path := filepath.Clean(u.Path)
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return "", &CaptureError{Tool: portalTool, Detail: "the screenshot location is not a regular file"}
	}
	if owner, ok := fileOwner(fi); !ok || owner != os.Getuid() {
		return "", &CaptureError{Tool: portalTool, Detail: "the screenshot file belongs to another user"}
	}
	return path, nil
}
