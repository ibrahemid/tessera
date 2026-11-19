//go:build linux

package secretstore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/ibrahemid/tessera/go/internal/dbusx"
)

const (
	fakeLogin = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	fakeOther = dbus.ObjectPath("/org/freedesktop/secrets/collection/other")
)

type promptMode int

const (
	promptComplete promptMode = iota
	promptDismiss
	promptHang
)

type fakeItem struct {
	coll  dbus.ObjectPath
	label string
	attrs map[string]string
	value []byte
	ctype string
}

type fakeCreate struct {
	label   string
	attrs   map[string]string
	ctype   string
	replace bool
}

type fakeSecretService struct {
	conn *dbus.Conn
	t    *testing.T

	mu          sync.Mutex
	alias       dbus.ObjectPath
	locked      map[dbus.ObjectPath]bool
	items       map[dbus.ObjectPath]*fakeItem
	sessions    map[dbus.ObjectPath]bool
	prompts     map[dbus.ObjectPath][]dbus.ObjectPath
	mode        promptMode
	searchDelay time.Duration
	getSecret   *dbus.Error
	calls       []string
	created     []fakeCreate
	opened      int
	closed      int
	next        int
	prompted    chan struct{}
}

func newFakeSecretService(t *testing.T) (*fakeSecretService, secretService) {
	t.Helper()
	if os.Getenv("TESSERA_DBUS_TEST") == "" {
		t.Skip("set TESSERA_DBUS_TEST=1 and run under dbus-run-session to test against a fake Secret Service")
	}
	conn, err := dbusx.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	f := &fakeSecretService{
		conn:     conn,
		t:        t,
		alias:    fakeLogin,
		locked:   map[dbus.ObjectPath]bool{fakeLogin: false, fakeOther: false},
		items:    map[dbus.ObjectPath]*fakeItem{},
		sessions: map[dbus.ObjectPath]bool{},
		prompts:  map[dbus.ObjectPath][]dbus.ObjectPath{},
		prompted: make(chan struct{}, 8),
	}
	f.export(fakeServiceObject{f}, secretServicePath, secretServiceIface)
	for _, coll := range []dbus.ObjectPath{fakeLogin, fakeOther} {
		f.export(fakeCollectionObject{f, coll}, coll, secretCollectionIface)
	}
	return f, secretService{dest: conn.Names()[0]}
}

func (f *fakeSecretService) export(v any, path dbus.ObjectPath, iface string) {
	if err := f.conn.Export(v, path, iface); err != nil {
		f.t.Errorf("export %s: %v", path, err)
	}
}

func (f *fakeSecretService) update(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeSecretService) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeSecretService) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeSecretService) called(prefix string) bool {
	return slices.ContainsFunc(f.callLog(), func(c string) bool { return strings.HasPrefix(c, prefix) })
}

func (f *fakeSecretService) put(coll dbus.ObjectPath, key, value string) dbus.ObjectPath {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addItem(coll, passphraseLabelPrefix+key, passphraseAttributes(key), []byte(value), passphraseContentType)
}

func (f *fakeSecretService) addItem(coll dbus.ObjectPath, label string, attrs map[string]string, value []byte, ctype string) dbus.ObjectPath {
	f.next++
	path := dbus.ObjectPath(fmt.Sprintf("%s/%d", coll, f.next))
	f.items[path] = &fakeItem{coll: coll, label: label, attrs: attrs, value: slices.Clone(value), ctype: ctype}
	f.export(fakeItemObject{f, path}, path, secretItemIface)
	return path
}

func (f *fakeSecretService) values(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, it := range f.items {
		if maps.Equal(it.attrs, passphraseAttributes(key)) {
			out = append(out, string(it.value))
		}
	}
	slices.Sort(out)
	return out
}

func (f *fakeSecretService) sessionCounts() (opened, closed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opened, f.closed
}

func (f *fakeSecretService) newPrompt(objects []dbus.ObjectPath) dbus.ObjectPath {
	f.next++
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/secrets/prompt/p%d", f.next))
	f.prompts[path] = objects
	f.export(fakePromptObject{f, path}, path, secretPromptIface)
	return path
}

func (f *fakeSecretService) isLocked(obj dbus.ObjectPath) bool {
	if it, ok := f.items[obj]; ok {
		return f.locked[it.coll]
	}
	return f.locked[obj]
}

func fakeMatches(attrs, query map[string]string) bool {
	for k, v := range query {
		if attrs[k] != v {
			return false
		}
	}
	return true
}

type fakeServiceObject struct{ f *fakeSecretService }

func (o fakeServiceObject) OpenSession(alg string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("OpenSession %s", alg)
	if alg != "plain" {
		return dbus.MakeVariant(""), "/", dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"unsupported algorithm"})
	}
	f.next++
	f.opened++
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/secrets/session/s%d", f.next))
	f.sessions[path] = true
	f.export(fakeSessionObject{f, path}, path, secretSessionIface)
	return dbus.MakeVariant(""), path, nil
}

func (o fakeServiceObject) SearchItems(query map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	f.record("SearchItems")
	delay := f.searchDelay
	f.mu.Unlock()
	time.Sleep(delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	unlocked, locked := []dbus.ObjectPath{}, []dbus.ObjectPath{}
	for path, it := range f.items {
		if !fakeMatches(it.attrs, query) {
			continue
		}
		if f.locked[it.coll] {
			locked = append(locked, path)
		} else {
			unlocked = append(unlocked, path)
		}
	}
	slices.Sort(unlocked)
	slices.Sort(locked)
	return unlocked, locked, nil
}

func (o fakeServiceObject) Unlock(objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Unlock %v", objects)
	unlocked, locked := []dbus.ObjectPath{}, []dbus.ObjectPath{}
	for _, obj := range objects {
		if f.isLocked(obj) {
			locked = append(locked, obj)
		} else {
			unlocked = append(unlocked, obj)
		}
	}
	if len(locked) == 0 {
		return unlocked, "/", nil
	}
	return unlocked, f.newPrompt(locked), nil
}

func (o fakeServiceObject) ReadAlias(name string) (dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ReadAlias %s", name)
	if name != "default" {
		return "/", nil
	}
	return f.alias, nil
}

type fakeCollectionObject struct {
	f    *fakeSecretService
	path dbus.ObjectPath
}

func (o fakeCollectionObject) CreateItem(props map[string]dbus.Variant, secret secretServiceSecret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateItem %s", o.path)
	if f.locked[o.path] {
		return "/", "/", dbus.NewError(secretErrIsLocked, []any{"collection is locked"})
	}
	if !f.sessions[secret.Session] {
		return "/", "/", dbus.NewError(secretErrNoSuchObject, []any{"no such session"})
	}
	label, _ := props[secretItemIface+".Label"].Value().(string)
	attrs, _ := props[secretItemIface+".Attributes"].Value().(map[string]string)
	f.created = append(f.created, fakeCreate{label: label, attrs: attrs, ctype: secret.ContentType, replace: replace})
	if replace {
		for path, it := range f.items {
			if it.coll == o.path && maps.Equal(it.attrs, attrs) {
				it.label, it.value, it.ctype = label, slices.Clone(secret.Value), secret.ContentType
				return path, "/", nil
			}
		}
	}
	return f.addItem(o.path, label, attrs, secret.Value, secret.ContentType), "/", nil
}

func (o fakeCollectionObject) SearchItems(query map[string]string) ([]dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Collection.SearchItems %s", o.path)
	out := []dbus.ObjectPath{}
	for path, it := range f.items {
		if it.coll == o.path && fakeMatches(it.attrs, query) {
			out = append(out, path)
		}
	}
	return out, nil
}

type fakeItemObject struct {
	f    *fakeSecretService
	path dbus.ObjectPath
}

func (o fakeItemObject) GetSecret(session dbus.ObjectPath) (secretServiceSecret, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetSecret %s", o.path)
	it, ok := f.items[o.path]
	switch {
	case f.getSecret != nil:
		return secretServiceSecret{}, f.getSecret
	case !ok:
		return secretServiceSecret{}, dbus.NewError(secretErrNoSuchObject, []any{"no such item"})
	case f.locked[it.coll]:
		return secretServiceSecret{}, dbus.NewError(secretErrIsLocked, []any{"item is locked"})
	case !f.sessions[session]:
		return secretServiceSecret{}, dbus.NewError(secretErrNoSuchObject, []any{"no such session"})
	}
	return secretServiceSecret{Session: session, Parameters: []byte{}, Value: slices.Clone(it.value), ContentType: it.ctype}, nil
}

func (o fakeItemObject) Delete() (dbus.ObjectPath, *dbus.Error) {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Delete %s", o.path)
	it, ok := f.items[o.path]
	if !ok {
		return "/", dbus.NewError(secretErrNoSuchObject, []any{"no such item"})
	}
	if f.locked[it.coll] {
		return "/", dbus.NewError(secretErrIsLocked, []any{"item is locked"})
	}
	delete(f.items, o.path)
	return "/", nil
}

type fakeSessionObject struct {
	f    *fakeSecretService
	path dbus.ObjectPath
}

func (o fakeSessionObject) Close() *dbus.Error {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Session.Close")
	if f.sessions[o.path] {
		f.sessions[o.path] = false
		f.closed++
	}
	return nil
}

type fakePromptObject struct {
	f    *fakeSecretService
	path dbus.ObjectPath
}

func (o fakePromptObject) Prompt(string) *dbus.Error {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Prompt")
	select {
	case f.prompted <- struct{}{}:
	default:
	}
	objects := f.prompts[o.path]
	switch f.mode {
	case promptComplete:
		for _, obj := range objects {
			if it, ok := f.items[obj]; ok {
				f.locked[it.coll] = false
			} else {
				f.locked[obj] = false
			}
		}
		go f.emitCompleted(o.path, false, objects)
	case promptDismiss:
		go f.emitCompleted(o.path, true, []dbus.ObjectPath{})
	}
	return nil
}

func (o fakePromptObject) Dismiss() *dbus.Error {
	f := o.f
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Dismiss")
	return nil
}

func (f *fakeSecretService) emitCompleted(path dbus.ObjectPath, dismissed bool, result []dbus.ObjectPath) {
	time.Sleep(20 * time.Millisecond)
	if err := f.conn.Emit(path, secretPromptIface+".Completed", dismissed, dbus.MakeVariant(result)); err != nil {
		f.t.Errorf("emit Completed: %v", err)
	}
}

func TestSecretServiceIdentity(t *testing.T) {
	s := secretService{dest: secretServiceBusName}
	if s.ID() != "secret-service" || s.Name() != "Secret Service" {
		t.Fatalf("ID, Name = %q, %q", s.ID(), s.Name())
	}
}

func TestLinuxBackendOrder(t *testing.T) {
	var ids []string
	for _, b := range Backends() {
		ids = append(ids, b.ID())
	}
	if want := []string{"secret-service", "kernel-keyring"}; !slices.Equal(ids, want) {
		t.Fatalf("Backends() = %q, want %q", ids, want)
	}
	if s, ok := Backends()[0].(secretService); !ok || s.dest != secretServiceBusName {
		t.Fatalf("first backend = %#v", Backends()[0])
	}
}

func forbidSessionBus(t *testing.T) {
	t.Helper()
	prev := connectSessionBus
	connectSessionBus = func(context.Context) (*dbus.Conn, error) {
		t.Error("connected to the session bus")
		return nil, &dbusx.NoBusError{Reason: "forbidden in this test"}
	}
	t.Cleanup(func() { connectSessionBus = prev })
}

func TestSecretServiceRejectsBeforeConnecting(t *testing.T) {
	forbidSessionBus(t)
	s := secretService{dest: secretServiceBusName}
	ctx := context.Background()
	for _, key := range []string{"", "a\nb", "a\rb", "a\x00b"} {
		if err := s.Store(ctx, key, "secret", 0); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Store(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, _, err := s.Lookup(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Lookup(%q) = %v, want ErrInvalidKey", key, err)
		}
		if _, err := s.Has(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Has(%q) = %v, want ErrInvalidKey", key, err)
		}
		if err := s.Delete(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q) = %v, want ErrInvalidKey", key, err)
		}
	}
	for _, secret := range []string{"", "a\nb", "a\x00b", strings.Repeat("x", maxSecretLen+1)} {
		if err := s.Store(ctx, "vault", secret, 0); !errors.Is(err, ErrInvalidSecret) {
			t.Errorf("Store(secret len %d) = %v, want ErrInvalidSecret", len(secret), err)
		}
	}
	err := s.Store(ctx, "vault", "secret", time.Minute)
	if !errors.Is(err, ErrTTLUnsupported) || !strings.Contains(err.Error(), "Secret Service") {
		t.Errorf("Store with a TTL = %v, want ErrTTLUnsupported naming the Secret Service", err)
	}
}

func TestSecretServiceWithoutBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	s := secretService{dest: secretServiceBusName}
	ctx := context.Background()
	var u *UnavailableError
	if err := s.Available(ctx); !errors.As(err, &u) || u.Backend != "Secret Service" {
		t.Fatalf("Available = %v, want UnavailableError", err)
	}
	if _, _, err := s.Lookup(ctx, "vault"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Lookup = %v, want ErrUnavailable", err)
	}
	if err := s.Store(ctx, "vault", "secret", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Store = %v, want ErrUnavailable", err)
	}
}

func TestSecretServiceAvailable(t *testing.T) {
	f, s := newFakeSecretService(t)
	ctx := context.Background()
	if err := s.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	var u *UnavailableError
	missing := secretService{dest: "org.example.TesseraNobodyHome"}
	if err := missing.Available(ctx); !errors.As(err, &u) || !strings.Contains(u.Reason, "nothing provides org.example.TesseraNobodyHome") {
		t.Fatalf("Available without an owner = %v", err)
	}
	f.update(func() { f.alias = "/" })
	if err := s.Available(ctx); !errors.As(err, &u) || !strings.Contains(u.Reason, "no default keyring collection") {
		t.Fatalf("Available without a default collection = %v", err)
	}
	if err := s.Store(ctx, "vault", "secret", 0); !errors.As(err, &u) || !strings.Contains(u.Reason, "no default keyring collection") {
		t.Fatalf("Store without a default collection = %v", err)
	}
	if f.called("CreateItem") || f.called("Unlock") {
		t.Fatalf("Store without a default collection went on: %q", f.callLog())
	}
}

func TestSecretServiceLookupPrefersUnlocked(t *testing.T) {
	f, s := newFakeSecretService(t)
	ctx := context.Background()
	f.update(func() { f.locked[fakeOther] = true })
	f.put(fakeOther, "vault", "locked value")
	open := f.put(fakeLogin, "vault", "unlocked value")
	f.put(fakeLogin, "other-vault", "unrelated")

	got, ok, err := s.Lookup(ctx, "vault")
	if err != nil || !ok || got != "unlocked value" {
		t.Fatalf("Lookup = %q, %v, %v", got, ok, err)
	}
	if has, err := s.Has(ctx, "vault"); err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	if f.called("Unlock") || f.called("Prompt") {
		t.Fatalf("Lookup or Has prompted: %q", f.callLog())
	}
	gets := slices.DeleteFunc(f.callLog(), func(c string) bool { return !strings.HasPrefix(c, "GetSecret") })
	if want := []string{"GetSecret " + string(open)}; !slices.Equal(gets, want) {
		t.Fatalf("GetSecret calls = %q, want %q", gets, want)
	}
	if !f.called("OpenSession plain") {
		t.Fatalf("no plain session opened: %q", f.callLog())
	}
	if opened, closed := f.sessionCounts(); opened != 1 || closed != 1 {
		t.Fatalf("sessions opened %d, closed %d", opened, closed)
	}
}

func TestSecretServiceLookupLockedOnly(t *testing.T) {
	f, s := newFakeSecretService(t)
	ctx := context.Background()
	f.update(func() { f.locked[fakeLogin] = true })
	f.put(fakeLogin, "vault", "locked value")

	if got, ok, err := s.Lookup(ctx, "vault"); !errors.Is(err, ErrLocked) || ok || got != "" {
		t.Fatalf("Lookup = %q, %v, %v, want ErrLocked", got, ok, err)
	}
	if has, err := s.Has(ctx, "vault"); !errors.Is(err, ErrLocked) || has {
		t.Fatalf("Has = %v, %v, want ErrLocked", has, err)
	}
	for _, call := range []string{"Unlock", "Prompt", "GetSecret", "OpenSession"} {
		if f.called(call) {
			t.Fatalf("locked lookup called %s: %q", call, f.callLog())
		}
	}
}

func TestSecretServiceLookupItemLockedOnRead(t *testing.T) {
	f, s := newFakeSecretService(t)
	f.put(fakeLogin, "vault", "value")
	f.update(func() { f.getSecret = dbus.NewError(secretErrIsLocked, []any{"item is locked"}) })
	if _, ok, err := s.Lookup(context.Background(), "vault"); !errors.Is(err, ErrLocked) || ok {
		t.Fatalf("Lookup = %v, %v, want ErrLocked", ok, err)
	}
	if opened, closed := f.sessionCounts(); opened != 1 || closed != 1 {
		t.Fatalf("sessions opened %d, closed %d", opened, closed)
	}
}

func TestSecretServiceLookupMissing(t *testing.T) {
	f, s := newFakeSecretService(t)
	ctx := context.Background()
	if got, ok, err := s.Lookup(ctx, "vault"); err != nil || ok || got != "" {
		t.Fatalf("Lookup = %q, %v, %v", got, ok, err)
	}
	if has, err := s.Has(ctx, "vault"); err != nil || has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	if f.called("OpenSession") {
		t.Fatalf("a missing entry opened a session: %q", f.callLog())
	}
}

func TestSecretServiceLookupDeadline(t *testing.T) {
	f, s := newFakeSecretService(t)
	f.update(func() { f.searchDelay = 10 * time.Second })
	limit := secretServiceQuickTimeout + 500*time.Millisecond

	start := time.Now()
	_, _, err := s.Lookup(context.Background(), "vault")
	if elapsed := time.Since(start); elapsed > limit {
		t.Fatalf("Lookup took %v, want under %v", elapsed, limit)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "did not answer within 2s") {
		t.Fatalf("Lookup = %v, want a deadline error", err)
	}
	start = time.Now()
	if _, err := s.Has(context.Background(), "vault"); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > limit {
		t.Fatalf("Has = %v after %v", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, _, err := s.Lookup(ctx, "vault"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Lookup with a cancelled context = %v", err)
	}
}

func TestSecretServiceStoreUnlocksThroughPrompt(t *testing.T) {
	f, s := newFakeSecretService(t)
	ctx := context.Background()
	f.update(func() { f.locked[fakeLogin] = true })

	if err := s.Store(ctx, "/home/u/vault.json", "first", 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	if !f.called(fmt.Sprintf("Unlock [%s]", fakeLogin)) || !f.called("Prompt") {
		t.Fatalf("Store did not unlock through a prompt: %q", f.callLog())
	}
	if err := s.Store(ctx, "/home/u/vault.json", "second", 0); err != nil {
		t.Fatalf("Store over an existing entry = %v", err)
	}
	prompts := slices.DeleteFunc(f.callLog(), func(c string) bool { return c != "Prompt" })
	if len(prompts) != 1 {
		t.Fatalf("Store prompted %d times, want once for the locked collection", len(prompts))
	}
	if got := f.values("/home/u/vault.json"); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("stored values = %q, want one replaced entry", got)
	}
	f.mu.Lock()
	created := slices.Clone(f.created)
	f.mu.Unlock()
	want := fakeCreate{
		label:   "Tessera vault passphrase: /home/u/vault.json",
		attrs:   map[string]string{"xdg:schema": "com.ibrahemid.tessera.Passphrase", "vault": "/home/u/vault.json"},
		ctype:   "text/plain; charset=utf8",
		replace: true,
	}
	for _, c := range created {
		if c.label != want.label || !maps.Equal(c.attrs, want.attrs) || c.ctype != want.ctype || !c.replace {
			t.Fatalf("CreateItem = %+v, want %+v", c, want)
		}
	}
	for _, c := range f.callLog() {
		if strings.HasPrefix(c, "OpenSession") && c != "OpenSession plain" {
			t.Fatalf("session algorithm: %q", c)
		}
	}
	if opened, closed := f.sessionCounts(); opened != 2 || closed != 2 {
		t.Fatalf("sessions opened %d, closed %d", opened, closed)
	}
	if got, ok, err := s.Lookup(ctx, "/home/u/vault.json"); err != nil || !ok || got != "second" {
		t.Fatalf("Lookup after Store = %q, %v, %v", got, ok, err)
	}
}

func TestSecretServiceStoreDismissed(t *testing.T) {
	f, s := newFakeSecretService(t)
	f.update(func() { f.locked[fakeLogin], f.mode = true, promptDismiss })
	err := s.Store(context.Background(), "vault", "secret", 0)
	if !errors.Is(err, errSecretServiceDismissed) || !strings.Contains(err.Error(), "dismissed") {
		t.Fatalf("Store = %v, want the dismissed error", err)
	}
	if f.called("CreateItem") || f.called("OpenSession") {
		t.Fatalf("Store went on after a dismissed prompt: %q", f.callLog())
	}
}

func TestSecretServiceStoreCancelledDuringPrompt(t *testing.T) {
	f, s := newFakeSecretService(t)
	f.update(func() { f.locked[fakeLogin], f.mode = true, promptHang })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-f.prompted
		cancel()
	}()
	const secret = "pass-7c41e9"
	start := time.Now()
	err := s.Store(ctx, "vault", secret, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Store = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error carries the secret: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Store took %v after cancel", elapsed)
	}
	if !f.called("Dismiss") {
		t.Fatalf("the open prompt was not dismissed: %q", f.callLog())
	}
	if f.called("CreateItem") {
		t.Fatalf("Store went on after cancel: %q", f.callLog())
	}
}

func TestSecretServiceDelete(t *testing.T) {
	f, s := newFakeSecretService(t)
	ctx := context.Background()
	f.update(func() { f.locked[fakeOther] = true })
	locked := f.put(fakeOther, "vault", "locked value")
	f.put(fakeLogin, "vault", "unlocked value")
	f.put(fakeLogin, "keep", "kept")

	if err := s.Delete(ctx, "vault"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if !f.called(fmt.Sprintf("Unlock [%s]", locked)) {
		t.Fatalf("Delete did not unlock the locked entry: %q", f.callLog())
	}
	if got := f.values("vault"); len(got) != 0 {
		t.Fatalf("entries left after Delete: %d", len(got))
	}
	if got := f.values("keep"); !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("Delete removed an unrelated entry")
	}

	f.update(func() { f.calls = nil })
	if err := s.Delete(ctx, "vault"); err != nil {
		t.Fatalf("Delete of a missing entry = %v", err)
	}
	if f.called("Unlock") || f.called("Delete ") {
		t.Fatalf("Delete of a missing entry did more than search: %q", f.callLog())
	}
}

func TestSecretServiceDeleteDismissed(t *testing.T) {
	f, s := newFakeSecretService(t)
	f.update(func() { f.locked[fakeLogin], f.mode = true, promptDismiss })
	f.put(fakeLogin, "vault", "locked value")
	if err := s.Delete(context.Background(), "vault"); !errors.Is(err, errSecretServiceDismissed) {
		t.Fatalf("Delete = %v, want the dismissed error", err)
	}
	if got := f.values("vault"); len(got) != 1 {
		t.Fatalf("entries after a dismissed Delete: %d", len(got))
	}
}

func requireGnomeKeyring(t *testing.T) (secretService, *dbus.Conn) {
	t.Helper()
	if os.Getenv("TESSERA_SECRET_SERVICE_TEST") == "" {
		t.Skip("set TESSERA_SECRET_SERVICE_TEST=1 and run under dbus-run-session with an unlocked gnome-keyring to test against it")
	}
	conn, err := dbusx.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return secretService{dest: secretServiceBusName}, conn
}

func countEntries(t *testing.T, conn *dbus.Conn, key string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked, locked []dbus.ObjectPath
	err := conn.Object(secretServiceBusName, secretServicePath).CallWithContext(ctx, secretServiceIface+".SearchItems", 0, passphraseAttributes(key)).Store(&unlocked, &locked)
	if err != nil {
		t.Fatalf("SearchItems: %v", err)
	}
	return len(unlocked) + len(locked)
}

func TestSecretServiceGnomeKeyring(t *testing.T) {
	s, conn := requireGnomeKeyring(t)
	ctx := context.Background()
	key := fmt.Sprintf("/tmp/tessera-secret-service-test-%d/vault.json", time.Now().UnixNano())
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })

	if err := s.Available(ctx); err != nil {
		t.Fatalf("Available = %v", err)
	}
	if err := s.Store(ctx, key, "first value", 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	if got, ok, err := s.Lookup(ctx, key); err != nil || !ok || got != "first value" {
		t.Fatalf("Lookup = %q, %v, %v", got, ok, err)
	}
	if has, err := s.Has(ctx, key); err != nil || !has {
		t.Fatalf("Has = %v, %v", has, err)
	}
	if err := s.Store(ctx, key, "second value", 0); err != nil {
		t.Fatalf("Store over an existing entry = %v", err)
	}
	if got, ok, err := s.Lookup(ctx, key); err != nil || !ok || got != "second value" {
		t.Fatalf("Lookup after replace = %q, %v, %v", got, ok, err)
	}
	if n := countEntries(t, conn, key); n != 1 {
		t.Fatalf("entries after replace = %d, want 1", n)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if got, ok, err := s.Lookup(ctx, key); err != nil || ok || got != "" {
		t.Fatalf("Lookup after Delete = %q, %v, %v", got, ok, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a missing entry = %v", err)
	}
}

func TestSecretServiceGnomeKeyringLocked(t *testing.T) {
	s, conn := requireGnomeKeyring(t)
	password := os.Getenv("TESSERA_SECRET_SERVICE_TEST_PASSWORD")
	if password == "" {
		t.Skip("set TESSERA_SECRET_SERVICE_TEST_PASSWORD to the keyring password so the test can unlock it again")
	}
	ctx := context.Background()
	key := fmt.Sprintf("/tmp/tessera-secret-service-test-%d/locked.json", time.Now().UnixNano())
	if err := s.Store(ctx, key, "locked value", 0); err != nil {
		t.Fatalf("Store = %v", err)
	}
	service := conn.Object(secretServiceBusName, secretServicePath)
	var coll dbus.ObjectPath
	if err := service.Call(secretServiceIface+".ReadAlias", 0, "default").Store(&coll); err != nil {
		t.Fatalf("ReadAlias: %v", err)
	}
	t.Cleanup(func() {
		unlockWithPassword(t, conn, coll, password)
		_ = s.Delete(context.Background(), key)
	})
	var lockedObjs []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := service.Call(secretServiceIface+".Lock", 0, []dbus.ObjectPath{coll}).Store(&lockedObjs, &prompt); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if prompt != "/" {
		t.Fatalf("Lock asked for a prompt")
	}

	start := time.Now()
	got, ok, err := s.Lookup(ctx, key)
	if elapsed := time.Since(start); elapsed > secretServiceQuickTimeout {
		t.Fatalf("Lookup took %v, want under %v", elapsed, secretServiceQuickTimeout)
	}
	if !errors.Is(err, ErrLocked) || ok || got != "" {
		t.Fatalf("Lookup of a locked entry = %q, %v, %v, want ErrLocked", got, ok, err)
	}
	start = time.Now()
	if has, err := s.Has(ctx, key); !errors.Is(err, ErrLocked) || has || time.Since(start) > secretServiceQuickTimeout {
		t.Fatalf("Has of a locked entry = %v, %v after %v", has, err, time.Since(start))
	}
}

func unlockWithPassword(t *testing.T, conn *dbus.Conn, coll dbus.ObjectPath, password string) {
	t.Helper()
	service := conn.Object(secretServiceBusName, secretServicePath)
	var output dbus.Variant
	var session dbus.ObjectPath
	if err := service.Call(secretServiceIface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		t.Errorf("OpenSession: %v", err)
		return
	}
	defer conn.Object(secretServiceBusName, session).Call(secretSessionIface+".Close", 0)
	master := secretServiceSecret{Session: session, Parameters: []byte{}, Value: []byte(password), ContentType: "text/plain"}
	if err := service.Call("org.gnome.keyring.InternalUnsupportedGuiltRiddenInterface.UnlockWithMasterPassword", 0, coll, master).Err; err != nil {
		t.Errorf("unlock the keyring again: %v", err)
	}
}
