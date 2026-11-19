//go:build linux

package screenshot

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/ibrahemid/tessera/go/internal/dbusx"
)

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func writeFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRequestPath(t *testing.T) {
	got := requestPath(":1.42", "tessera_00ff")
	if want := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_42/tessera_00ff"); got != want {
		t.Fatalf("requestPath = %s, want %s", got, want)
	}
	if !got.IsValid() {
		t.Fatalf("requestPath = %s is not a valid object path", got)
	}
}

func TestHandleToken(t *testing.T) {
	a, err := handleToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := handleToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, "tessera_") {
		t.Fatalf("handleToken = %q, %q; want distinct tessera_ tokens", a, b)
	}
	if p := requestPath(":1.1", a); !p.IsValid() {
		t.Fatalf("token %q makes the invalid path %s", a, p)
	}
}

func TestFileFromURI(t *testing.T) {
	dir := t.TempDir()
	spaced := writeFile(t, filepath.Join(dir, "Screen shot.png"))
	plain := writeFile(t, filepath.Join(dir, "shot.png"))
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(plain, link); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		uri  string
		want string
	}{
		{fileURI(spaced), spaced},
		{"file://localhost" + plain, plain},
		{"file://" + dir + "/./shot.png", plain},
	} {
		got, err := fileFromURI(c.uri)
		if err != nil || got != c.want {
			t.Errorf("fileFromURI(%q) = %q, %v; want %q", c.uri, got, err, c.want)
		}
	}

	for _, uri := range []string{
		"",
		"https://example.com/shot.png",
		"file://otherhost" + plain,
		"file:shot.png",
		fileURI(dir),
		fileURI(link),
		fileURI(filepath.Join(dir, "absent.png")),
		"%zz",
	} {
		got, err := fileFromURI(uri)
		var ce *CaptureError
		if !errors.As(err, &ce) || ce.Tool != portalTool || got != "" {
			t.Errorf("fileFromURI(%q) = %q, %v; want a portal CaptureError", uri, got, err)
		}
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the refused symlink changed: %v, %v", fi, err)
	}
}

func TestPortalResult(t *testing.T) {
	shot := writeFile(t, filepath.Join(t.TempDir(), "shot.png"))
	uri := map[string]dbus.Variant{"uri": dbus.MakeVariant(fileURI(shot))}

	got, err := portalResult([]any{uint32(0), uri})
	if err != nil || got != shot {
		t.Fatalf("success = %q, %v; want %q", got, err, shot)
	}
	if _, err := portalResult([]any{uint32(1), map[string]dbus.Variant{}}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("code 1 = %v, want ErrCancelled", err)
	}
	_, err = portalResult([]any{uint32(2), map[string]dbus.Variant{}})
	var ce *CaptureError
	if !errors.As(err, &ce) || !strings.Contains(err.Error(), "response code 2") {
		t.Fatalf("code 2 = %v, want a CaptureError naming the code", err)
	}
	for _, body := range [][]any{
		nil,
		{uint32(0)},
		{int32(0), uri},
		{uint32(0), "uri"},
		{uint32(0), map[string]dbus.Variant{}},
		{uint32(0), map[string]dbus.Variant{"uri": dbus.MakeVariant(uint32(7))}},
	} {
		if got, err := portalResult(body); !errors.As(err, &ce) || got != "" {
			t.Errorf("portalResult(%v) = %q, %v; want a CaptureError", body, got, err)
		}
	}
}

func TestPortalWithoutBusFallsThrough(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := capturePortal(context.Background()); !errors.Is(err, errNoPortal) {
		t.Fatalf("capturePortal = %v, want errNoPortal", err)
	}
	dir := toolDir(t, writers(toolGrim, toolSlurp))
	scratch := t.TempDir()
	path, err := capture(context.Background(), scratch, env{getenv: fixedEnv(waylandVars), lookPath: exec.LookPath, portal: capturePortal})
	if err != nil || path != filepath.Join(scratch, captureName) {
		t.Fatalf("capture = %q, %v; want the grim capture", path, err)
	}
	if len(toolCalls(t, dir, toolGrim)) != 1 {
		t.Fatal("grim did not run after the portal was unavailable")
	}
}

func TestPortalHonoursDoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := capturePortal(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("capturePortal = %v, want context.Canceled", err)
	}
}

type portalCall struct {
	sender dbus.Sender
	parent string
	opts   map[string]dbus.Variant
}

// portalConfig fixes how a fake portal behaves before it goes on the bus.
type portalConfig struct {
	screenshot bool
	moved      bool
	impostor   bool
	respond    func(p *fakePortal, handle dbus.ObjectPath)
}

type fakePortal struct {
	t        *testing.T
	cfg      portalConfig
	conn     *dbus.Conn
	impostor *dbus.Conn
	decoy    string

	mu         sync.Mutex
	calls      []portalCall
	stopped    bool
	done       chan struct{}
	responders sync.WaitGroup
}

type fakeScreenshot struct{ p *fakePortal }

func (o fakeScreenshot) Screenshot(sender dbus.Sender, parent string, opts map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	token, _ := opts["handle_token"].Value().(string)
	unique := strings.ReplaceAll(strings.TrimPrefix(string(sender), ":"), ".", "_")
	handle := dbus.ObjectPath(requestPrefix + unique + "/" + token)
	if o.p.cfg.moved {
		handle = dbus.ObjectPath(requestPrefix + unique + "/legacy")
	}
	o.p.mu.Lock()
	defer o.p.mu.Unlock()
	o.p.calls = append(o.p.calls, portalCall{sender: sender, parent: parent, opts: opts})
	if o.p.cfg.respond != nil && !o.p.stopped {
		o.p.responders.Go(func() { o.p.cfg.respond(o.p, handle) })
	}
	return handle, nil
}

type fakeProperties struct{ screenshot bool }

func (o fakeProperties) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	if o.screenshot && iface == screenshotIface && prop == "version" {
		return dbus.MakeVariant(uint32(4)), nil
	}
	return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", []any{"no such property"})
}

// startFakePortal owns org.freedesktop.portal.Desktop on the session bus and
// serves the Screenshot interface when cfg.screenshot is set.
func startFakePortal(t *testing.T, cfg portalConfig) *fakePortal {
	t.Helper()
	if os.Getenv("TESSERA_PORTAL_TEST") == "" {
		t.Skip("set TESSERA_PORTAL_TEST=1 and run under dbus-run-session to test against a fake screenshot portal")
	}
	conn, err := dbusx.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	p := &fakePortal{t: t, cfg: cfg, conn: conn, done: make(chan struct{})}
	t.Cleanup(func() {
		p.mu.Lock()
		p.stopped = true
		close(p.done)
		p.mu.Unlock()
		p.responders.Wait()
		if p.impostor != nil {
			p.impostor.Close()
		}
		conn.ReleaseName(portalName)
		conn.Close()
	})
	if cfg.impostor {
		p.decoy = writeFile(t, filepath.Join(t.TempDir(), "decoy.png"))
		if p.impostor, err = dbusx.Connect(context.Background()); err != nil {
			t.Fatalf("Connect impostor: %v", err)
		}
	}
	reply, err := conn.RequestName(portalName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName = %v, %v", reply, err)
	}
	if err := conn.Export(fakeProperties{screenshot: cfg.screenshot}, portalPath, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal(err)
	}
	if cfg.screenshot {
		if err := conn.Export(fakeScreenshot{p}, portalPath, screenshotIface); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func (p *fakePortal) recorded() []portalCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]portalCall(nil), p.calls...)
}

func respondWith(code uint32, results map[string]dbus.Variant) func(*fakePortal, dbus.ObjectPath) {
	return func(p *fakePortal, handle dbus.ObjectPath) {
		if p.impostor != nil {
			if err := p.impostor.Emit(handle, requestIface+".Response", uint32(0), map[string]dbus.Variant{"uri": dbus.MakeVariant(fileURI(p.decoy))}); err != nil {
				p.t.Errorf("impostor Emit: %v", err)
			}
			var id string
			if err := p.impostor.BusObject().Call("org.freedesktop.DBus.GetId", 0).Store(&id); err != nil {
				p.t.Errorf("impostor round trip: %v", err)
			}
		}
		for {
			if err := p.conn.Emit(handle, requestIface+".Response", code, results); err != nil {
				p.t.Errorf("Emit: %v", err)
				return
			}
			if !p.cfg.moved {
				return
			}
			select {
			case <-p.done:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
}

func portalCapture(ctx context.Context, t *testing.T) (string, error) {
	t.Helper()
	dir := toolDir(t, writers(allTools...))
	path, err := capture(ctx, t.TempDir(), env{getenv: fixedEnv(waylandVars), lookPath: exec.LookPath, portal: capturePortal})
	for _, name := range allTools {
		if calls := toolCalls(t, dir, name); len(calls) != 0 {
			t.Fatalf("%s ran although the portal answered: %+v", name, calls)
		}
	}
	return path, err
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestFakePortalSuccess(t *testing.T) {
	shot := writeFile(t, filepath.Join(t.TempDir(), "Screen shot.png"))
	p := startFakePortal(t, portalConfig{screenshot: true, impostor: true, respond: respondWith(0, map[string]dbus.Variant{"uri": dbus.MakeVariant(fileURI(shot))})})

	path, err := portalCapture(testContext(t), t)
	if err != nil || path != shot {
		t.Fatalf("capture = %q, %v; want %q", path, err, shot)
	}
	calls := p.recorded()
	if len(calls) != 1 {
		t.Fatalf("Screenshot calls = %d, want 1", len(calls))
	}
	c := calls[0]
	token, _ := c.opts["handle_token"].Value().(string)
	interactive, _ := c.opts["interactive"].Value().(bool)
	modal, _ := c.opts["modal"].Value().(bool)
	if c.parent != "" || !strings.HasPrefix(token, "tessera_") || !interactive || !modal {
		t.Fatalf("Screenshot(%q, %v); want an empty parent, a tessera_ token, interactive and modal", c.parent, c.opts)
	}
	if _, err := os.Stat(shot); err != nil {
		t.Fatalf("the capture must stay for the caller to decode and remove: %v", err)
	}
}

func TestFakePortalMovedRequest(t *testing.T) {
	shot := writeFile(t, filepath.Join(t.TempDir(), "shot.png"))
	startFakePortal(t, portalConfig{screenshot: true, moved: true, respond: respondWith(0, map[string]dbus.Variant{"uri": dbus.MakeVariant(fileURI(shot))})})
	path, err := portalCapture(testContext(t), t)
	if err != nil || path != shot {
		t.Fatalf("capture = %q, %v; want %q", path, err, shot)
	}
}

func TestFakePortalCancel(t *testing.T) {
	startFakePortal(t, portalConfig{screenshot: true, respond: respondWith(1, map[string]dbus.Variant{})})
	path, err := portalCapture(testContext(t), t)
	if path != "" || !errors.Is(err, ErrCancelled) {
		t.Fatalf("capture = %q, %v; want ErrCancelled", path, err)
	}
}

func TestFakePortalResponseCodeTwo(t *testing.T) {
	startFakePortal(t, portalConfig{screenshot: true, respond: respondWith(2, map[string]dbus.Variant{})})
	path, err := portalCapture(testContext(t), t)
	var ce *CaptureError
	if path != "" || !errors.As(err, &ce) || ce.Tool != portalTool || !strings.Contains(err.Error(), "response code 2") {
		t.Fatalf("capture = %q, %v; want a portal CaptureError naming code 2", path, err)
	}
}

func TestFakePortalRefusesWhatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	target := writeFile(t, filepath.Join(dir, "target.png"))
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{"https://example.com/shot.png", fileURI(dir), fileURI(link)} {
		t.Run(uri, func(t *testing.T) {
			startFakePortal(t, portalConfig{screenshot: true, respond: respondWith(0, map[string]dbus.Variant{"uri": dbus.MakeVariant(uri)})})
			path, err := portalCapture(testContext(t), t)
			var ce *CaptureError
			if path != "" || !errors.As(err, &ce) || ce.Tool != portalTool {
				t.Fatalf("capture = %q, %v; want a portal CaptureError", path, err)
			}
		})
	}
	for _, p := range []string{dir, link, target} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s was touched: %v", p, err)
		}
	}
}

func TestFakePortalMissingInterfaceFallsThrough(t *testing.T) {
	p := startFakePortal(t, portalConfig{respond: respondWith(0, map[string]dbus.Variant{})})
	dir := toolDir(t, writers(toolGrim, toolSlurp))
	scratch := t.TempDir()
	path, err := capture(testContext(t), scratch, env{getenv: fixedEnv(waylandVars), lookPath: exec.LookPath, portal: capturePortal})
	if err != nil || path != filepath.Join(scratch, captureName) {
		t.Fatalf("capture = %q, %v; want the grim capture", path, err)
	}
	if len(toolCalls(t, dir, toolGrim)) != 1 || len(toolCalls(t, dir, toolSlurp)) != 1 {
		t.Fatal("grim and slurp did not run after the portal lacked the Screenshot interface")
	}
	if len(p.recorded()) != 0 {
		t.Fatal("Screenshot was called on a portal without the interface")
	}
}

func TestFakePortalTimeoutDoesNotHang(t *testing.T) {
	p := startFakePortal(t, portalConfig{screenshot: true})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	path, err := portalCapture(ctx, t)
	if path != "" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("capture = %q, %v; want context.DeadlineExceeded", path, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("capture returned after %v", elapsed)
	}
	if len(p.recorded()) != 1 {
		t.Fatal("the timeout fired before the Screenshot request was made")
	}
}
