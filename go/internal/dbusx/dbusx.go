//go:build linux

// Package dbusx opens private connections to the D-Bus session bus. It never
// autolaunches a bus, never sets environment variables, and bounds every call
// it makes with a deadline.
package dbusx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	callTimeout  = 5 * time.Second
	signalBuffer = 8
)

// NoBusError reports that no session bus can be reached.
type NoBusError struct{ Reason string }

func (e *NoBusError) Error() string { return "no D-Bus session bus: " + e.Reason }

// SessionAddress returns the session bus address: DBUS_SESSION_BUS_ADDRESS when
// set, else unix:path=$XDG_RUNTIME_DIR/bus when that path is a socket. Any other
// case is a *NoBusError.
func SessionAddress(getenv func(string) string, stat func(string) (os.FileInfo, error)) (string, error) {
	if addr := getenv("DBUS_SESSION_BUS_ADDRESS"); addr != "" && addr != "autolaunch:" {
		return addr, nil
	}
	dir := getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", &NoBusError{Reason: "DBUS_SESSION_BUS_ADDRESS and XDG_RUNTIME_DIR are unset"}
	}
	if !filepath.IsAbs(dir) {
		return "", &NoBusError{Reason: "DBUS_SESSION_BUS_ADDRESS is unset and XDG_RUNTIME_DIR is not an absolute path"}
	}
	sock := filepath.Join(dir, "bus")
	fi, err := stat(sock)
	if err != nil || fi.Mode().Type() != fs.ModeSocket {
		return "", &NoBusError{Reason: "DBUS_SESSION_BUS_ADDRESS is unset and " + sock + " is not a socket"}
	}
	return "unix:path=" + dbus.EscapeBusAddressValue(sock), nil
}

// Connect opens a private, authenticated session bus connection and sends
// Hello. The connection closes when ctx is done; the caller closes it otherwise.
func Connect(ctx context.Context) (*dbus.Conn, error) {
	addr, err := SessionAddress(os.Getenv, os.Stat)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dbusx: connect to the session bus: %w", err)
	}
	conn, err := dbus.Connect(addr, dbus.WithContext(ctx))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("dbusx: connect to the session bus: %w", ctxErr)
		}
		return nil, &NoBusError{Reason: fmt.Sprintf("cannot connect to %s: %v", addr, err)}
	}
	return conn, nil
}

// HasName reports whether name has an owner on the bus or can be activated.
func HasName(ctx context.Context, conn *dbus.Conn, name string) (bool, error) {
	bus := conn.BusObject()
	var owned bool
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned); err != nil {
		return false, fmt.Errorf("dbusx: check owner of %s: %w", name, err)
	}
	if owned {
		return true, nil
	}
	var activatable []string
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err != nil {
		return false, fmt.Errorf("dbusx: list activatable names: %w", err)
	}
	return slices.Contains(activatable, name), nil
}

// Watch receives one kind of signal on a connection.
type Watch struct {
	conn      *dbus.Conn
	path      dbus.ObjectPath
	name      string
	match     []dbus.MatchOption
	ch        chan *dbus.Signal
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// WatchSignal subscribes to the signal iface.member emitted from path. Call it
// before the method call that triggers the signal, then read with Next.
func WatchSignal(conn *dbus.Conn, path dbus.ObjectPath, iface, member string) (*Watch, error) {
	if !path.IsValid() {
		return nil, fmt.Errorf("dbusx: invalid object path %q", path)
	}
	w := &Watch{
		conn: conn,
		path: path,
		name: iface + "." + member,
		match: []dbus.MatchOption{
			dbus.WithMatchObjectPath(path),
			dbus.WithMatchInterface(iface),
			dbus.WithMatchMember(member),
		},
		ch:   make(chan *dbus.Signal, signalBuffer),
		done: make(chan struct{}),
	}
	conn.Signal(w.ch)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := conn.AddMatchSignalContext(ctx, w.match...); err != nil {
		conn.RemoveSignal(w.ch)
		return nil, fmt.Errorf("dbusx: subscribe to %s on %s: %w", w.name, path, err)
	}
	return w, nil
}

// Next returns the next matching signal. It returns ctx.Err() when ctx is done
// and dbus.ErrClosed once the watch or its connection is closed.
func (w *Watch) Next(ctx context.Context) (*dbus.Signal, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-w.done:
			return nil, dbus.ErrClosed
		case sig, ok := <-w.ch:
			if !ok {
				return nil, dbus.ErrClosed
			}
			if sig.Path == w.path && sig.Name == w.name {
				return sig, nil
			}
		}
	}
}

// Close stops delivery and removes the match rule. It is safe to call more
// than once.
func (w *Watch) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.conn.RemoveSignal(w.ch)
		if !w.conn.Connected() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		err := w.conn.RemoveMatchSignalContext(ctx, w.match...)
		if err != nil && !errors.Is(err, dbus.ErrClosed) {
			w.closeErr = fmt.Errorf("dbusx: unsubscribe from %s on %s: %w", w.name, w.path, err)
		}
	})
	return w.closeErr
}
