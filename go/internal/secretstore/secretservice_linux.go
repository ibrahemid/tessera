//go:build linux

package secretstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/ibrahemid/tessera/go/internal/dbusx"
)

const (
	secretServiceBusName  = "org.freedesktop.secrets"
	secretServicePath     = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceIface    = "org.freedesktop.Secret.Service"
	secretCollectionIface = "org.freedesktop.Secret.Collection"
	secretItemIface       = "org.freedesktop.Secret.Item"
	secretSessionIface    = "org.freedesktop.Secret.Session"
	secretPromptIface     = "org.freedesktop.Secret.Prompt"
	secretErrIsLocked     = "org.freedesktop.Secret.Error.IsLocked"
	secretErrNoSuchObject = "org.freedesktop.Secret.Error.NoSuchObject"
	secretNoObject        = dbus.ObjectPath("/")

	passphraseSchema      = "com.ibrahemid.tessera.Passphrase"
	passphraseContentType = "text/plain; charset=utf8"
	passphraseLabelPrefix = "Tessera vault passphrase: "

	secretServiceQuickTimeout   = 2 * time.Second
	secretServicePromptTimeout  = 120 * time.Second
	secretServiceCleanupTimeout = 2 * time.Second
)

var (
	errSecretServiceQuery     = errors.New("secretstore: could not query the Secret Service; check that the keyring daemon is running and retry")
	errSecretServiceRead      = errors.New("secretstore: could not read the Secret Service entry; unlock the default keyring and retry")
	errSecretServiceWrite     = errors.New("secretstore: could not save the passphrase in the Secret Service; unlock the default keyring and retry")
	errSecretServicePersisted = errors.New("secretstore: the Secret Service entry did not persist; unlock the default keyring and retry")
	errSecretServiceDelete    = errors.New("secretstore: could not delete the Secret Service entry; unlock the default keyring and retry")
	errSecretServiceDismissed = errors.New("secretstore: the keyring unlock was dismissed; run the command again and unlock the keyring")
)

var connectSessionBus = dbusx.Connect

func init() {
	register(10, func() Backend { return secretService{dest: secretServiceBusName} })
}

type secretService struct{ dest string }

type secretServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func (secretService) ID() string   { return "secret-service" }
func (secretService) Name() string { return "Secret Service" }

func (s secretService) Available(ctx context.Context) error {
	c, err := s.dial(ctx, false)
	if err != nil {
		return s.unavailable(err)
	}
	defer c.close()
	has, err := dbusx.HasName(c.ctx, c.conn, s.dest)
	if err != nil {
		return s.unavailable(c.fail(err, errSecretServiceQuery))
	}
	if !has {
		return &UnavailableError{Backend: s.Name(), Reason: "nothing provides " + s.dest + "; start gnome-keyring, KWallet or KeePassXC"}
	}
	if _, err := c.defaultCollection(); err != nil {
		return s.unavailable(err)
	}
	return nil
}

func (s secretService) Store(ctx context.Context, key, secret string, ttl time.Duration) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ValidateSecret(secret); err != nil {
		return err
	}
	if ttl != 0 {
		return errTTLUnsupported(s)
	}
	c, err := s.dial(ctx, true)
	if err != nil {
		return err
	}
	defer c.close()
	coll, err := c.defaultCollection()
	if err != nil {
		return err
	}
	if err := c.unlock([]dbus.ObjectPath{coll}, errSecretServiceWrite); err != nil {
		return err
	}
	if err := c.openSession(); err != nil {
		return err
	}
	value := []byte(secret)
	defer clear(value)
	props := map[string]dbus.Variant{
		secretItemIface + ".Label":      dbus.MakeVariant(passphraseLabelPrefix + key),
		secretItemIface + ".Attributes": dbus.MakeVariant(passphraseAttributes(key)),
	}
	content := secretServiceSecret{Session: c.session, Parameters: []byte{}, Value: value, ContentType: passphraseContentType}
	var item, prompt dbus.ObjectPath
	if err := c.object(coll).CallWithContext(c.ctx, secretCollectionIface+".CreateItem", 0, props, content, true).Store(&item, &prompt); err != nil {
		return c.fail(err, errSecretServiceWrite)
	}
	if err := c.prompt(prompt, errSecretServiceWrite); err != nil {
		return err
	}
	ok, err := c.holds(coll, key, secret)
	if err != nil {
		return err
	}
	if !ok {
		return errSecretServicePersisted
	}
	return nil
}

func (s secretService) Lookup(ctx context.Context, key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	c, err := s.dial(ctx, false)
	if err != nil {
		return "", false, err
	}
	defer c.close()
	unlocked, locked, err := c.search(key)
	if err != nil {
		return "", false, err
	}
	sawLocked := len(locked) > 0
	if len(unlocked) > 0 {
		if err := c.openSession(); err != nil {
			return "", false, err
		}
	}
	for _, item := range unlocked {
		secret, found, err := c.secret(item, errSecretServiceRead)
		switch {
		case err == nil && found:
			return secret, true, nil
		case errors.Is(err, ErrLocked):
			sawLocked = true
		case err != nil:
			return "", false, err
		}
	}
	if sawLocked {
		return "", false, ErrLocked
	}
	return "", false, nil
}

func (s secretService) Has(ctx context.Context, key string) (bool, error) {
	if err := ValidateKey(key); err != nil {
		return false, err
	}
	c, err := s.dial(ctx, false)
	if err != nil {
		return false, err
	}
	defer c.close()
	unlocked, locked, err := c.search(key)
	switch {
	case err != nil:
		return false, err
	case len(unlocked) > 0:
		return true, nil
	case len(locked) > 0:
		return false, ErrLocked
	}
	return false, nil
}

func (s secretService) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	c, err := s.dial(ctx, true)
	if err != nil {
		return err
	}
	defer c.close()
	unlocked, locked, err := c.search(key)
	if err != nil {
		return err
	}
	if len(locked) > 0 {
		if err := c.unlock(locked, errSecretServiceDelete); err != nil {
			return err
		}
	}
	for _, item := range slices.Concat(unlocked, locked) {
		var prompt dbus.ObjectPath
		err := c.object(item).CallWithContext(c.ctx, secretItemIface+".Delete", 0).Store(&prompt)
		if dbusErrorName(err) == secretErrNoSuchObject {
			continue
		}
		if err != nil {
			return c.fail(err, errSecretServiceDelete)
		}
		if err := c.prompt(prompt, errSecretServiceDelete); err != nil {
			return err
		}
	}
	unlocked, locked, err = c.search(key)
	if err != nil {
		return err
	}
	if len(unlocked)+len(locked) > 0 {
		return errSecretServiceDelete
	}
	return nil
}

func (s secretService) unavailable(err error) error {
	var u *UnavailableError
	if errors.As(err, &u) {
		return u
	}
	return &UnavailableError{Backend: s.Name(), Reason: strings.TrimPrefix(err.Error(), "secretstore: ")}
}

func (s secretService) dial(parent context.Context, prompting bool) (*secretServiceConn, error) {
	limit := secretServiceQuickTimeout
	if prompting {
		limit = secretServicePromptTimeout
	}
	ctx, cancelOp := context.WithTimeout(parent, limit)
	c := &secretServiceConn{dest: s.dest, name: s.Name(), ctx: ctx, parent: parent, limit: limit, prompting: prompting}
	connCtx, cancelConn := ctx, context.CancelFunc(func() {})
	if prompting {
		// The connection outlives a cancelled ctx long enough to dismiss an open prompt.
		connCtx, cancelConn = context.WithTimeout(context.WithoutCancel(parent), limit+2*secretServiceCleanupTimeout)
	}
	stop := context.AfterFunc(ctx, cancelConn)
	conn, err := connectSessionBus(connCtx)
	stop()
	if err == nil && ctx.Err() != nil {
		conn.Close()
		err = ctx.Err()
	}
	if err != nil {
		err = c.connectFail(err)
		cancelConn()
		cancelOp()
		return nil, err
	}
	c.conn = conn
	c.release = func() {
		conn.Close()
		cancelConn()
		cancelOp()
	}
	return c, nil
}

type secretServiceConn struct {
	dest      string
	name      string
	conn      *dbus.Conn
	ctx       context.Context
	parent    context.Context
	limit     time.Duration
	prompting bool
	session   dbus.ObjectPath
	release   func()
}

func (c *secretServiceConn) object(path dbus.ObjectPath) dbus.BusObject {
	return c.conn.Object(c.dest, path)
}

func (c *secretServiceConn) cleanupContext() (context.Context, context.CancelFunc) {
	if !c.prompting {
		return c.ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(c.parent), secretServiceCleanupTimeout)
}

func (c *secretServiceConn) close() {
	if c.session != "" {
		ctx, cancel := c.cleanupContext()
		_ = c.object(c.session).CallWithContext(ctx, secretSessionIface+".Close", 0).Err
		cancel()
	}
	c.release()
}

func (c *secretServiceConn) defaultCollection() (dbus.ObjectPath, error) {
	var coll dbus.ObjectPath
	if err := c.object(secretServicePath).CallWithContext(c.ctx, secretServiceIface+".ReadAlias", 0, "default").Store(&coll); err != nil {
		return "", c.fail(err, errSecretServiceQuery)
	}
	if coll == secretNoObject || !coll.IsValid() {
		return "", &UnavailableError{Backend: c.name, Reason: "no default keyring collection; create a default keyring in your keyring manager, such as Seahorse"}
	}
	return coll, nil
}

func (c *secretServiceConn) search(key string) (unlocked, locked []dbus.ObjectPath, err error) {
	err = c.object(secretServicePath).CallWithContext(c.ctx, secretServiceIface+".SearchItems", 0, passphraseAttributes(key)).Store(&unlocked, &locked)
	if err != nil {
		return nil, nil, c.fail(err, errSecretServiceQuery)
	}
	return unlocked, locked, nil
}

func (c *secretServiceConn) openSession() error {
	var output dbus.Variant
	var session dbus.ObjectPath
	if err := c.object(secretServicePath).CallWithContext(c.ctx, secretServiceIface+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		return c.fail(err, errSecretServiceQuery)
	}
	if session == secretNoObject || !session.IsValid() {
		return fmt.Errorf("%w (the service returned no session)", errSecretServiceQuery)
	}
	c.session = session
	return nil
}

func (c *secretServiceConn) secret(item dbus.ObjectPath, sentinel error) (string, bool, error) {
	var content secretServiceSecret
	err := c.object(item).CallWithContext(c.ctx, secretItemIface+".GetSecret", 0, c.session).Store(&content)
	defer clear(content.Value)
	if dbusErrorName(err) == secretErrNoSuchObject {
		return "", false, nil
	}
	if err != nil {
		return "", false, c.fail(err, sentinel)
	}
	return string(content.Value), true, nil
}

func (c *secretServiceConn) holds(coll dbus.ObjectPath, key, secret string) (bool, error) {
	var items []dbus.ObjectPath
	if err := c.object(coll).CallWithContext(c.ctx, secretCollectionIface+".SearchItems", 0, passphraseAttributes(key)).Store(&items); err != nil {
		return false, c.fail(err, errSecretServicePersisted)
	}
	for _, item := range items {
		got, found, err := c.secret(item, errSecretServicePersisted)
		if err != nil {
			return false, err
		}
		if found && got == secret {
			return true, nil
		}
	}
	return false, nil
}

func (c *secretServiceConn) unlock(objects []dbus.ObjectPath, sentinel error) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.object(secretServicePath).CallWithContext(c.ctx, secretServiceIface+".Unlock", 0, objects).Store(&unlocked, &prompt); err != nil {
		return c.fail(err, sentinel)
	}
	return c.prompt(prompt, sentinel)
}

func (c *secretServiceConn) prompt(prompt dbus.ObjectPath, sentinel error) error {
	if prompt == "" || prompt == secretNoObject {
		return nil
	}
	w, err := dbusx.WatchSignal(c.conn, prompt, secretPromptIface, "Completed")
	if err != nil {
		return c.fail(err, sentinel)
	}
	defer w.Close()
	if err := c.object(prompt).CallWithContext(c.ctx, secretPromptIface+".Prompt", 0, "").Err; err != nil {
		c.dismiss(prompt)
		return c.promptFail(err, sentinel)
	}
	sig, err := w.Next(c.ctx)
	if err != nil {
		c.dismiss(prompt)
		return c.promptFail(err, sentinel)
	}
	dismissed, ok := false, len(sig.Body) > 0
	if ok {
		dismissed, ok = sig.Body[0].(bool)
	}
	if !ok {
		return fmt.Errorf("%w (the prompt sent a malformed Completed signal)", sentinel)
	}
	if dismissed {
		return errSecretServiceDismissed
	}
	return nil
}

func (c *secretServiceConn) dismiss(prompt dbus.ObjectPath) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.parent), secretServiceCleanupTimeout)
	defer cancel()
	_ = c.object(prompt).CallWithContext(ctx, secretPromptIface+".Dismiss", 0).Err
}

func (c *secretServiceConn) promptFail(err, sentinel error) error {
	if c.parent.Err() == nil && errors.Is(c.ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("secretstore: the keyring unlock prompt was not answered within %s; run the command again: %w", c.limit, context.DeadlineExceeded)
	}
	return c.fail(err, sentinel)
}

func (c *secretServiceConn) connectFail(err error) error {
	var nb *dbusx.NoBusError
	if errors.As(err, &nb) {
		return &UnavailableError{Backend: c.name, Reason: nb.Error()}
	}
	return c.fail(err, errSecretServiceQuery)
}

func (c *secretServiceConn) fail(err, sentinel error) error {
	switch {
	case dbusErrorName(err) == secretErrIsLocked:
		return ErrLocked
	case c.parent.Err() != nil:
		return fmt.Errorf("secretstore: %s: %w", c.name, c.parent.Err())
	case errors.Is(c.ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("secretstore: the %s did not answer within %s; check that the keyring daemon is running: %w", c.name, c.limit, context.DeadlineExceeded)
	}
	return fmt.Errorf("%w (%v)", sentinel, err)
}

func passphraseAttributes(key string) map[string]string {
	return map[string]string{"xdg:schema": passphraseSchema, "vault": key}
}

func dbusErrorName(err error) string {
	var v dbus.Error
	if errors.As(err, &v) {
		return v.Name
	}
	var p *dbus.Error
	if errors.As(err, &p) && p != nil {
		return p.Name
	}
	return ""
}
