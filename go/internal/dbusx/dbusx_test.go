//go:build linux

package dbusx

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "bus" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

func TestSessionAddress(t *testing.T) {
	socket := func(string) (os.FileInfo, error) { return fakeInfo{mode: fs.ModeSocket | 0o600}, nil }
	regular := func(string) (os.FileInfo, error) { return fakeInfo{mode: 0o600}, nil }
	missing := func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist }
	unexpected := func(string) (os.FileInfo, error) {
		t.Fatal("stat called although DBUS_SESSION_BUS_ADDRESS is set")
		return nil, nil
	}

	cases := []struct {
		name  string
		env   map[string]string
		stat  func(string) (os.FileInfo, error)
		want  string
		noBus bool
	}{
		{
			name: "env set",
			env:  map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus", "XDG_RUNTIME_DIR": "/run/user/1000"},
			stat: unexpected,
			want: "unix:path=/run/user/1000/bus",
		},
		{
			name: "env unset with socket",
			env:  map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			stat: socket,
			want: "unix:path=/run/user/1000/bus",
		},
		{
			name: "runtime dir is escaped",
			env:  map[string]string{"XDG_RUNTIME_DIR": "/run/user/a b,c"},
			stat: socket,
			want: "unix:path=/run/user/a%20b%2cc/bus",
		},
		{
			name: "autolaunch is treated as unset",
			env:  map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:", "XDG_RUNTIME_DIR": "/run/user/1000"},
			stat: socket,
			want: "unix:path=/run/user/1000/bus",
		},
		{
			name:  "env unset with regular file",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			stat:  regular,
			noBus: true,
		},
		{
			name:  "env unset with nothing at the path",
			env:   map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			stat:  missing,
			noBus: true,
		},
		{
			name:  "relative runtime dir",
			env:   map[string]string{"XDG_RUNTIME_DIR": "run/user/1000"},
			stat:  socket,
			noBus: true,
		},
		{
			name:  "nothing",
			env:   map[string]string{},
			stat:  socket,
			noBus: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			got, err := SessionAddress(getenv, tc.stat)
			if tc.noBus {
				var nb *NoBusError
				if !errors.As(err, &nb) {
					t.Fatalf("SessionAddress = %q, %v; want *NoBusError", got, err)
				}
				if got != "" {
					t.Fatalf("SessionAddress returned address %q with an error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("SessionAddress = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestNoBusErrorMessage(t *testing.T) {
	err := &NoBusError{Reason: "XDG_RUNTIME_DIR is unset"}
	if got, want := err.Error(), "no D-Bus session bus: XDG_RUNTIME_DIR is unset"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestConnectWithoutBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	conn, err := Connect(context.Background())
	var nb *NoBusError
	if !errors.As(err, &nb) {
		if conn != nil {
			conn.Close()
		}
		t.Fatalf("Connect = %v; want *NoBusError", err)
	}
}

func TestConnectToMissingSocket(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(t.TempDir(), "absent"))
	conn, err := Connect(context.Background())
	var nb *NoBusError
	if !errors.As(err, &nb) {
		if conn != nil {
			conn.Close()
		}
		t.Fatalf("Connect = %v; want *NoBusError", err)
	}
}

func requireBus(t *testing.T) (context.Context, *dbus.Conn) {
	t.Helper()
	if os.Getenv("TESSERA_DBUS_TEST") == "" {
		t.Skip("set TESSERA_DBUS_TEST=1 and run under dbus-run-session to test against a real session bus")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	conn, err := Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return ctx, conn
}

func TestHasNameOnSessionBus(t *testing.T) {
	ctx, conn := requireBus(t)
	has, err := HasName(ctx, conn, "org.freedesktop.DBus")
	if err != nil || !has {
		t.Fatalf("HasName(org.freedesktop.DBus) = %v, %v; want true", has, err)
	}
	has, err = HasName(ctx, conn, "com.ibrahemid.tessera.Absent")
	if err != nil || has {
		t.Fatalf("HasName(absent) = %v, %v; want false", has, err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := HasName(ctx, conn, "org.freedesktop.DBus"); err == nil {
		t.Fatal("HasName on a closed connection succeeded")
	}
}

func TestWatchReceivesOnlyItsSignal(t *testing.T) {
	ctx, conn := requireBus(t)
	const (
		path  = dbus.ObjectPath("/com/ibrahemid/tessera/Test")
		iface = "com.ibrahemid.tessera.Test"
	)
	ping, err := WatchSignal(conn, path, iface, "Ping")
	if err != nil {
		t.Fatalf("WatchSignal(Ping): %v", err)
	}
	defer ping.Close()
	pong, err := WatchSignal(conn, path, iface, "Pong")
	if err != nil {
		t.Fatalf("WatchSignal(Pong): %v", err)
	}
	defer pong.Close()

	if err := conn.Emit(path, iface+".Pong", "second"); err != nil {
		t.Fatalf("Emit Pong: %v", err)
	}
	if err := conn.Emit(path, iface+".Ping", "first"); err != nil {
		t.Fatalf("Emit Ping: %v", err)
	}

	sig, err := ping.Next(ctx)
	if err != nil {
		t.Fatalf("ping.Next: %v", err)
	}
	if sig.Name != iface+".Ping" || len(sig.Body) != 1 || sig.Body[0] != "first" {
		t.Fatalf("ping.Next = %s %v", sig.Name, sig.Body)
	}
	sig, err = pong.Next(ctx)
	if err != nil {
		t.Fatalf("pong.Next: %v", err)
	}
	if sig.Name != iface+".Pong" || len(sig.Body) != 1 || sig.Body[0] != "second" {
		t.Fatalf("pong.Next = %s %v", sig.Name, sig.Body)
	}
}

func TestWatchNextHonoursCancel(t *testing.T) {
	_, conn := requireBus(t)
	w, err := WatchSignal(conn, "/com/ibrahemid/tessera/Test", "com.ibrahemid.tessera.Test", "Never")
	if err != nil {
		t.Fatalf("WatchSignal: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	if _, err := w.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next = %v; want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Next returned %v after cancel", elapsed)
	}
}

func TestWatchClose(t *testing.T) {
	ctx, conn := requireBus(t)
	w, err := WatchSignal(conn, "/com/ibrahemid/tessera/Test", "com.ibrahemid.tessera.Test", "Closed")
	if err != nil {
		t.Fatalf("WatchSignal: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := w.Next(ctx); !errors.Is(err, dbus.ErrClosed) {
		t.Fatalf("Next after Close = %v; want dbus.ErrClosed", err)
	}

	other, err := WatchSignal(conn, "/com/ibrahemid/tessera/Test", "com.ibrahemid.tessera.Test", "Gone")
	if err != nil {
		t.Fatalf("WatchSignal: %v", err)
	}
	conn.Close()
	if _, err := other.Next(ctx); !errors.Is(err, dbus.ErrClosed) {
		t.Fatalf("Next after connection close = %v; want dbus.ErrClosed", err)
	}
	if err := other.Close(); err != nil {
		t.Fatalf("Close after connection close: %v", err)
	}
}

func TestWatchSignalRejectsInvalidPath(t *testing.T) {
	_, conn := requireBus(t)
	if _, err := WatchSignal(conn, "not/a/path", "com.ibrahemid.tessera.Test", "Ping"); err == nil {
		t.Fatal("WatchSignal accepted an invalid object path")
	}
}

func TestConnectHonoursCancelledContext(t *testing.T) {
	requireBus(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn, err := Connect(ctx)
	if err == nil {
		conn.Close()
		t.Fatal("Connect with a cancelled context succeeded")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect = %v; want context.Canceled", err)
	}
}
