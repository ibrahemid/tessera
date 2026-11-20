package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/code"
	"github.com/ibrahemid/tessera/go/internal/otp"
	"github.com/ibrahemid/tessera/go/internal/vault"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"google.golang.org/protobuf/encoding/protowire"
)

var errVectorsShape = errors.New("vectorgen: input does not end with the top-level closing brace")

const (
	v2Passphrase    = "correct horse battery staple"
	v2NewPassphrase = "a new passphrase after rotation"
	v2Created       = int64(1700000000)
	v2Deleted       = int64(1760000000)
	v2SecretB32     = "JBSWY3DPEHPK3PXP"
)

var v2Salt = []byte("tessera-salt-v2!")

type member struct {
	key   string
	value any
}

type object []member

type rawValue string

func (o object) index(key string) int {
	for i, m := range o {
		if m.key == key {
			return i
		}
	}
	panic("vectorgen: no member " + key)
}

func (o object) get(key string) any { return o[o.index(key)].value }

func (o object) set(key string, v any) object {
	out := slices.Clone(o)
	out[o.index(key)].value = v
	return out
}

func (o object) drop(key string) object {
	i := o.index(key)
	return slices.Delete(slices.Clone(o), i, i+1)
}

func (o object) rename(old, new string) object {
	out := slices.Clone(o)
	out[o.index(old)].key = new
	return out
}

func (o object) with(key string, v any) object {
	return append(slices.Clone(o), member{key, v})
}

func marshal(v any) []byte {
	var b bytes.Buffer
	writeJSON(&b, v)
	return b.Bytes()
}

func writeList[T any](b *bytes.Buffer, xs []T) {
	b.WriteByte('[')
	for i, x := range xs {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSON(b, any(x))
	}
	b.WriteByte(']')
}

func writeJSON(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case object:
		b.WriteByte('{')
		for i, m := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, m.key)
			b.WriteByte(':')
			writeJSON(b, m.value)
		}
		b.WriteByte('}')
	case []object:
		writeList(b, x)
	case []any:
		writeList(b, x)
	case []string:
		writeList(b, x)
	case []int64:
		writeList(b, x)
	case rawValue:
		b.WriteString(string(x))
	case json.RawMessage:
		b.Write(x)
	case []byte:
		writeJSON(b, base64.StdEncoding.EncodeToString(x))
	case string:
		var s bytes.Buffer
		enc := json.NewEncoder(&s)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(x); err != nil {
			panic(err)
		}
		b.Write(bytes.TrimSuffix(s.Bytes(), []byte{'\n'}))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case uint64:
		b.WriteString(strconv.FormatUint(x, 10))
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case nil:
		b.WriteString("null")
	default:
		panic(fmt.Sprintf("vectorgen: cannot marshal %T", v))
	}
}

func asciiJSON(b []byte) []byte {
	var out bytes.Buffer
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		switch {
		case r < utf8.RuneSelf && r != 0x7f:
			out.WriteByte(b[0])
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&out, "\x5cu%04x\x5cu%04x", hi, lo)
		default:
			fmt.Fprintf(&out, "\x5cu%04x", r)
		}
		b = b[n:]
	}
	return out.Bytes()
}

func v2Blocks() []member {
	return []member{
		{"vault_v2", vaultV2Block()},
		{"envelope_strict", envelopeStrictBlock()},
		{"payload_strict", payloadStrictBlock()},
		{"text_clean", textCleanBlock()},
		{"otpauth_strict", otpauthStrictBlock()},
		{"migration_strict", migrationStrictBlock()},
		{"hotp_counter_range", hotpCounterRangeBlock()},
		{"display_order", displayOrderBlock()},
		{"rotation", rotationBlock()},
		{"rotation_extra_passphrase", rotationExtraPassphraseBlock()},
		{"exports", exportsBlock()},
		{"merge", mergeBlock()},
		{"unknown_type", unknownTypeBlock()},
		{"otp_time_range", otpTimeRangeBlock()},
	}
}

func spliceV2(existing []byte, blocks []member) ([]byte, error) {
	var head []byte
	if i := bytes.Index(existing, []byte(",\n  \"vault_v2\": ")); i >= 0 {
		head = existing[:i]
	} else {
		trimmed := bytes.TrimRight(existing, "\n")
		if !bytes.HasSuffix(trimmed, []byte("\n}")) {
			return nil, errVectorsShape
		}
		head = trimmed[:len(trimmed)-2]
	}
	var out bytes.Buffer
	out.Write(head)
	for _, m := range blocks {
		var indented bytes.Buffer
		if err := json.Indent(&indented, marshal(m.value), "  ", "  "); err != nil {
			return nil, fmt.Errorf("vectorgen: indent %s: %w", m.key, err)
		}
		fmt.Fprintf(&out, ",\n  %s: %s", marshal(m.key), asciiJSON(indented.Bytes()))
	}
	out.WriteString("\n}\n")
	return out.Bytes(), nil
}

func runV2(r io.Reader, w io.Writer) error {
	existing, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("vectorgen: read vectors: %w", err)
	}
	out, err := spliceV2(existing, v2Blocks())
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

func pattern(n int, start byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = start + byte(i)
	}
	return b
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func flip(b []byte, i int) []byte {
	out := slices.Clone(b)
	if i < 0 {
		i += len(out)
	}
	out[i] ^= 1
	return out
}

func mustCanonical(accounts ...account.Account) []byte {
	b, err := account.CanonicalJSON(accounts)
	if err != nil {
		panic(err)
	}
	return b
}

func accountObject(a account.Account) object {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(mustCanonical(a), &arr); err != nil || len(arr) != 1 {
		panic(fmt.Sprintf("vectorgen: account object: %v", err))
	}
	keys := make([]string, 0, len(arr[0]))
	for k := range arr[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	o := make(object, 0, len(keys))
	for _, k := range keys {
		o = append(o, member{k, rawValue(arr[0][k])})
	}
	return o
}

func payloadText(o object) string { return string(marshal([]object{o})) }

func v2Account(idSuffix string) account.Account {
	return account.Account{
		ID: "00000000-0000-4000-8000-0000000000" + idSuffix, Type: account.TOTP,
		Issuer: "ACME", Account: "alice@example.com", Secret: []byte("12345678901234567890"),
		Algorithm: "SHA1", Digits: 6, Period: 30, CreatedAt: v2Created, UpdatedAt: v2Created,
	}
}

type cost struct{ v, m, t, p int }

var (
	v2DefaultCost = cost{1, 131072, 3, 4}
	v2LowCost     = cost{1, 8, 1, 1}
)

func (c cost) object() object {
	return object{{"v", c.v}, {"m", c.m}, {"t", c.t}, {"p", c.p}}
}

func xseal(key, nonce, plain []byte) []byte {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		panic(err)
	}
	return aead.Seal(nil, nonce, plain, nil)
}

func passWrap(dek []byte, passphrase string, c cost, salt, nonce []byte) object {
	key := argon2.IDKey([]byte(passphrase), salt, uint32(c.t), uint32(c.m), uint8(c.p), 32)
	return object{
		{"type", "passphrase"}, {"kdf", "argon2id"}, {"params", c.object()},
		{"salt", salt}, {"nonce", nonce}, {"ct", xseal(key, nonce, dek)},
	}
}

func seWrapObject() object {
	return object{
		{"type", "secure-enclave"}, {"kdf", "se-keychain-v1"}, {"se_key", pattern(16, 0x60)},
		{"nonce", pattern(24, 0x70)}, {"ct", pattern(48, 0x80)},
	}
}

// foundationStyle renders an envelope the way Foundation's JSONEncoder writes
// it with pretty printing: indented, " : " after keys, "/" escaped.
func foundationStyle(o object) string {
	var b bytes.Buffer
	if err := json.Indent(&b, marshal(o), "", "  "); err != nil {
		panic(err)
	}
	s := strings.ReplaceAll(b.String(), "/", `\/`)
	return strings.ReplaceAll(s, `": `, `" : `)
}

// nest is n arrays inside one another.
func nest(n int) rawValue {
	return rawValue(strings.Repeat("[", n) + strings.Repeat("]", n))
}

func unknownWrap(i int) object {
	return object{{"type", "hardware-key"}, {"slot", i}, {"blob", pattern(8, byte(i))}}
}

func unknownWraps(n int) []any {
	out := make([]any, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, unknownWrap(i))
	}
	return out
}

func envelopeObject(version int, wraps []any, payloadNonce, payloadCT []byte) object {
	if wraps == nil {
		wraps = []any{}
	}
	return object{
		{"version", version},
		{"aead", "xchacha20poly1305"},
		{"wraps", wraps},
		{"payload", object{{"nonce", payloadNonce}, {"ct", payloadCT}}},
	}
}

func extensionFields() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"aliases":     json.RawMessage(`["a","b"]`),
		"label":       json.RawMessage(marshal("q\"b\\/<>&\n\t\x01\u2028\u00e9")),
		"meta":        json.RawMessage(`{"nested":{"a":{"x":1,"y":"2"},"b":[1,true,null]},"z":-1}`),
		"zone_offset": json.RawMessage(`-18000`),
	}
}

func vaultV2Block() object {
	noted := v2Account("01")
	noted.Note = "recovery codes in the safe\nsecond line"
	noted.Order = 2
	noted.Extra = map[string]json.RawMessage{"x_color": json.RawMessage(`"teal"`)}
	steam := account.Account{
		ID: "00000000-0000-4000-8000-000000000002", Type: account.Steam, Issuer: "Steam",
		Account: "gamer", Secret: []byte("abcdefghijklmnop"), Algorithm: "SHA1", Digits: 5, Period: 30,
		CreatedAt: v2Created, UpdatedAt: v2Deleted, DeletedAt: v2Deleted,
	}
	hotp := account.Account{
		ID: "00000000-0000-4000-8000-000000000003", Type: account.HOTP, Issuer: "Bank",
		Account: "ops", Secret: []byte("12345678901234567890123456789012"), Algorithm: "SHA256",
		Digits: 8, Period: 30, Counter: 5, CreatedAt: v2Created, UpdatedAt: v2Created,
	}
	canonical := mustCanonical(noted, steam, hotp)
	dek := pattern(32, 0x10)
	nonce := pattern(24, 0x48)
	env := envelopeObject(2, []any{passWrap(dek, v2Passphrase, v2DefaultCost, v2Salt, pattern(24, 0x30))}, nonce, xseal(dek, nonce, canonical))
	return object{
		{"passphrase", v2Passphrase},
		{"envelope", env},
		{"expected_canonical_json", string(canonical)},
		{"expected_version", 2},
		{"canonical", canonicalCases()},
	}
}

func canonicalCases() []object {
	base := v2Account("20")
	deleted := base
	deleted.DeletedAt = v2Deleted
	noted := base
	noted.Note = "line one\nline two"
	ordered := base
	ordered.Order = 3
	all := base
	all.DeletedAt, all.Note, all.Order = v2Deleted, "line one\nline two", 3
	ext := base
	ext.Extra = extensionFields()
	full := all
	full.Extra = extensionFields()

	resorted := accountObject(ext).set("meta", rawValue(`{"z":-1,"nested":{"b":[1,true,null],"a":{"y":"2","x":1}}}`))
	slices.Reverse(resorted)

	type canonCase struct {
		name  string
		input []byte
		clear []string
		want  account.Account
	}
	cases := []canonCase{
		{"v1-baseline", mustCanonical(base), nil, base},
		{"deleted-at-only", mustCanonical(deleted), nil, deleted},
		{"note-only", mustCanonical(noted), nil, noted},
		{"order-only", mustCanonical(ordered), nil, ordered},
		{"all-v2-fields", mustCanonical(all), nil, all},
		{"v2-fields-cleared", mustCanonical(full), []string{"deleted_at", "note", "order", "aliases", "label", "meta", "zone_offset"}, base},
		{"extension-fields", mustCanonical(ext), nil, ext},
		{"extension-resorted", []byte(payloadText(resorted)), nil, ext},
	}
	out := make([]object, 0, len(cases))
	for _, c := range cases {
		o := object{{"name", c.name}, {"input", string(c.input)}}
		if c.clear != nil {
			o = o.with("clear", c.clear)
		}
		o = o.with("expected_canonical_json", string(mustCanonical(c.want)))
		o = o.with("expected_version", account.FormatVersion([]account.Account{c.want}))
		out = append(out, o)
	}
	return out
}

func trailingBits(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	i := len(s) - 3
	idx := strings.IndexByte(alphabet, s[i])
	return s[:i] + string(alphabet[idx|1]) + s[i+1:]
}

func envelopeStrictBlock() object {
	dek := pattern(32, 0x90)
	acct := v2Account("21")
	acct.Note = "kept note"
	plain := mustCanonical(acct)
	pnonce := bytes.Repeat([]byte{0xfb, 0xff, 0xbf}, 8)
	pct := xseal(dek, pnonce, plain)
	good := passWrap(dek, v2Passphrase, v2LowCost, v2Salt, pattern(24, 0x30))
	goodCT := good.get("ct").([]byte)
	wrong := func(n int) object {
		return passWrap(dek, fmt.Sprintf("wrong passphrase %d", n), v2LowCost, pattern(16, byte(0xa0+n)), pattern(24, byte(0xb0+n)))
	}
	env := func(wraps ...any) object { return envelopeObject(2, wraps, pnonce, pct) }
	text := func(o object) string { return string(marshal(o)) }
	params := func(c cost) object { return good.set("params", c.object()) }
	base := env(good)
	baseText := text(base)
	payload := base.get("payload").(object)
	withPayload := func(k string, v any) object { return base.set("payload", payload.set(k, v)) }
	ctB64 := b64(pct)
	saltB64 := b64(v2Salt)
	future := object{{"note", "kept"}, {"list", []any{1, 2}}}

	vcase := func(name string, o object) object {
		return object{{"name", name}, {"envelope_json", text(o)}}
	}
	valid := []object{
		vcase("base", base),
		vcase("bad-wrap-skipped", env(good.set("ct", flip(goodCT, 0)), good)),
		vcase("malformed-not-counted", env(good.set("kdf", "scrypt"), good.set("salt", pattern(8, 0xa0)), params(cost{1, 8, 0, 1}), params(cost{1, 8, 1, 2}), wrong(1), wrong(2), wrong(3), good)),
		vcase("four-attempts", env(wrong(1), wrong(2), wrong(3), good)),
		vcase("unknown-wrap-first", env(unknownWrap(1), good)),
		vcase("se-and-passphrase", env(seWrapObject(), good)),
		vcase("unknown-top-level-key", base.with("x_future", future)),
		vcase("sixteen-wraps", env(append(unknownWraps(15), good)...)),
		{{"name", "escaped-slashes-indented"}, {"envelope_json", foundationStyle(base)}},
		vcase("nesting-64", base.with("x_deep", nest(account.MaxNesting-1))),
	}

	inv := func(name, envelopeJSON, code string) object {
		return object{{"name", name}, {"envelope_json", envelopeJSON}, {"error", code}}
	}
	invEnv := func(name string, o object, code string) object { return inv(name, text(o), code) }
	invalid := []object{
		inv("truncated", baseText[:len(baseText)/2], "corrupt"),
		invEnv("nesting-65", base.with("x_deep", nest(account.MaxNesting)), "corrupt"),
		inv("not-an-object", "["+baseText+"]", "corrupt"),
		inv("trailing-data", baseText+"{}", "corrupt"),
		invEnv("payload-ct-tamper", withPayload("ct", flip(pct, 0)), "auth_failed"),
		invEnv("payload-tag-tamper", withPayload("ct", flip(pct, -1)), "auth_failed"),
		invEnv("wrap-ct-tamper", env(good.set("ct", flip(goodCT, 0))), "auth_failed"),
		{{"name", "wrong-passphrase"}, {"passphrase", "not the passphrase"}, {"envelope_json", baseText}, {"error", "auth_failed"}},
		invEnv("bad-base64", withPayload("ct", "!"+ctB64[1:]), "corrupt"),
		invEnv("base64url", withPayload("nonce", strings.Repeat("-_", 16)), "corrupt"),
		invEnv("missing-padding", env(good.set("salt", strings.TrimRight(saltB64, "="))), "corrupt"),
		invEnv("embedded-line-break", withPayload("ct", ctB64[:4]+"\n"+ctB64[4:]), "corrupt"),
		invEnv("embedded-space", withPayload("ct", ctB64[:4]+" "+ctB64[4:]), "corrupt"),
		invEnv("trailing-bits", env(good.set("salt", trailingBits(saltB64))), "corrupt"),
		invEnv("duplicate-top-level-key", base.with("version", 2), "corrupt"),
		invEnv("duplicate-wrap-key", env(good.with("salt", v2Salt)), "corrupt"),
		invEnv("duplicate-payload-key", base.set("payload", payload.with("nonce", pnonce)), "corrupt"),
		invEnv("duplicate-nested-key", base.with("x_future", object{{"a", 1}, {"a", 2}}), "corrupt"),
		inv("duplicate-after-unescape", strings.Replace(text(base.with("x_dupe", 2)), `"x_dupe"`, "\"ver\x5cu0073ion\"", 1), "corrupt"),
		invEnv("wrong-case-top-level", base.rename("wraps", "WRAPS"), "corrupt"),
		invEnv("wrong-case-wrap", env(good.rename("salt", "Salt")), "corrupt"),
		invEnv("zero-wraps", env(), "no_wraps"),
		invEnv("seventeen-wraps", env(append(unknownWraps(16), good)...), "too_many_wraps"),
		invEnv("version-3", base.set("version", 3), "unsupported_version"),
		invEnv("version-0", base.set("version", 0), "unsupported_version"),
		invEnv("version-string", base.set("version", "1"), "unsupported_version"),
		invEnv("version-float", base.set("version", rawValue("1.0")), "unsupported_version"),
		invEnv("aead-unknown", base.set("aead", "aes-256-gcm"), "unsupported_aead"),
		invEnv("payload-nonce-12", withPayload("nonce", pattern(12, 0x40)), "corrupt"),
		invEnv("wrap-nonce-12", env(good.set("nonce", pattern(12, 0x40))), "corrupt"),
		invEnv("kdf-scrypt", env(good.set("kdf", "scrypt")), "corrupt"),
		invEnv("salt-8", env(good.set("salt", pattern(8, 0xa0))), "corrupt"),
		invEnv("salt-32", env(good.set("salt", pattern(32, 0xa0))), "corrupt"),
		invEnv("t-0", env(params(cost{1, 8, 0, 1})), "corrupt"),
		invEnv("p-0", env(params(cost{1, 8, 1, 0})), "corrupt"),
		invEnv("p-17", env(params(cost{1, 136, 1, 17})), "corrupt"),
		invEnv("m-below-8p", env(params(cost{1, 8, 1, 2})), "corrupt"),
		invEnv("m-above-max", env(params(cost{1, 1048577, 1, 1})), "corrupt"),
		invEnv("work-above-max", env(params(cost{1, 1048576, 5, 1})), "corrupt"),
		invEnv("params-missing", env(good.drop("params")), "corrupt"),
		invEnv("params-non-integer", env(good.set("params", object{{"v", 1}, {"m", rawValue("8.0")}, {"t", 1}, {"p", 1}})), "corrupt"),
		invEnv("attempt-limit", env(wrong(1), wrong(2), wrong(3), wrong(4), good), "auth_failed"),
		invEnv("secure-enclave-only", env(seWrapObject()), "app_only"),
		invEnv("unknown-wrap-only", env(unknownWrap(1)), "no_passphrase_wrap"),
	}

	preserve := env(good, seWrapObject().with("x_hint", "kept"), unknownWrap(1)).with("x_future", future)
	separators := env(good.with("x_hint", "g\u2029h"), seWrapObject().with("x_hint", "e\u2028f"), unknownWrap(1).with("note", "c\u2029d")).with("x_note", "a\u2028b")
	separatorsText := strings.NewReplacer(`\u2028`, "\u2028", `\u2029`, "\u2029").Replace(text(separators))
	separatorsEnv, err := vault.Unmarshal([]byte(separatorsText))
	if err != nil {
		panic(err)
	}
	separatorsOut, err := separatorsEnv.Marshal()
	if err != nil {
		panic(err)
	}
	return object{
		{"_note", "error classes: corrupt (malformed, duplicate, non-canonical or out-of-bounds input), auth_failed (no passphrase wrap or payload authenticates), no_wraps, too_many_wraps, unsupported_version, unsupported_aead, app_only (secure-enclave wraps only), no_passphrase_wrap (no passphrase or secure-enclave wrap)"},
		{"passphrase", v2Passphrase},
		{"expected_canonical_json", string(plain)},
		{"valid", valid},
		{"invalid", invalid},
		{"preserve", object{
			{"envelope_json", text(preserve)},
			{"unknown_top_level", []string{"x_future"}},
			{"unknown_wrap_types", []string{"hardware-key"}},
		}},
		{"line_separators", object{
			{"envelope_json", separatorsText},
			{"expected_envelope_json", string(separatorsOut)},
		}},
	}
}

func payloadStrictBlock() object {
	base := accountObject(v2Account("30"))
	ext := func(raw string) string { return payloadText(base.with("x_ext", rawValue(raw))) }
	secret := b64([]byte("12345678901234567890"))
	inv := func(name, payload, code string) object {
		return object{{"name", name}, {"payload", payload}, {"error", code}}
	}
	valid := []object{
		{{"name", "extension-nesting-64"}, {"payload", ext(string(nest(account.MaxNesting - 2)))}},
	}
	return object{{"valid", valid}, {"invalid", []object{
		inv("extension-nesting-65", ext(string(nest(account.MaxNesting-1))), "malformed_payload"),
		inv("extension-float", ext("1.5"), "non_canonical_number"),
		inv("extension-exponent", ext("1e3"), "non_canonical_number"),
		inv("extension-negative-zero", ext("-0"), "non_canonical_number"),
		inv("extension-leading-zero", ext("01"), "malformed_payload"),
		inv("extension-int64-overflow", ext("9223372036854775808"), "non_canonical_number"),
		inv("extension-duplicate-key", ext(`{"a":1,"a":2}`), "duplicate_key"),
		inv("extension-duplicate-deep", ext(`{"b":{"c":[{"d":1,"d":1}]}}`), "duplicate_key"),
		inv("duplicate-defined-key", payloadText(base.with("issuer", "Other")), "duplicate_key"),
		inv("duplicate-after-unescape", strings.Replace(payloadText(base.with("x_dupe", "Other")), `"x_dupe"`, "\"issu\x5cu0065r\"", 1), "duplicate_key"),
		inv("pinned-number", payloadText(base.set("pinned", rawValue("1"))), "field_type"),
		inv("pinned-string", payloadText(base.set("pinned", "true")), "field_type"),
		inv("created-at-string", payloadText(base.set("created_at", "1700000000")), "field_type"),
		inv("tags-string", payloadText(base.set("tags", "a")), "field_type"),
		inv("tags-mixed", payloadText(base.set("tags", []any{"a", 1})), "field_type"),
		inv("issuer-number", payloadText(base.set("issuer", 5)), "field_type"),
		inv("digits-float", payloadText(base.set("digits", rawValue("6.0"))), "non_canonical_number"),
		inv("counter-float", payloadText(base.set("counter", rawValue("0.0"))), "non_canonical_number"),
		inv("not-an-array", string(marshal(base)), "malformed_payload"),
		inv("element-not-object", "[1]", "malformed_payload"),
		inv("trailing-data", "[][]", "malformed_payload"),
		inv("secret-missing-padding", payloadText(base.set("secret", strings.TrimRight(secret, "="))), "non_canonical_base64"),
		inv("secret-base64url", payloadText(base.set("secret", "-_-_-_-_")), "non_canonical_base64"),
	}}}
}

func textCleanBlock() object {
	c := func(name, in, out string) object { return object{{"name", name}, {"in", in}, {"out", out}} }
	return object{
		{"text", []object{
			c("c0", "a\x00b\x07c\x1fd", "abcd"),
			c("del-c1", "a\x7fb\u0080c\u0085d\u009fe", "abcde"),
			c("bidi-embed-override", "\u202aA\u202bB\u202cC\u202dD\u202eE", "ABCDE"),
			c("bidi-isolates", "\u2066A\u2067B\u2068C\u2069", "ABC"),
			c("bom", "\ufeffACME\ufeff", "ACME"),
			c("line-breaks-removed", "Acme\nCorp\r\nInc\tX", "AcmeCorpIncX"),
			c("unicode-space-trim", "\u00a0\u2003 ACME \u3000\u2009", "ACME"),
			c("interior-spaces-kept", "Acme  Corp", "Acme  Corp"),
			c("marks-kept", "a\u200eb\u200fc\u061cd\u200ce\u200df", "a\u200eb\u200fc\u061cd\u200ce\u200df"),
			c("arabic-hebrew", "\u0634\u0631\u0643\u0629 \u05d7\u05d1\u05e8\u05d4", "\u0634\u0631\u0643\u0629 \u05d7\u05d1\u05e8\u05d4"),
			c("only-removed", "\u202e\x07\ufeff", ""),
			c("removal-then-trim", "\u202e Acme \u202c", "Acme"),
			c("emoji", "\U0001F510 Vault", "\U0001F510 Vault"),
		}},
		{"note", []object{
			c("crlf", "a\r\nb", "a\nb"),
			c("lone-cr", "a\rb\r", "a\nb"),
			c("cr-crlf", "a\r\r\nb", "a\n\nb"),
			c("lf-tab-kept", "a\n\tb", "a\n\tb"),
			c("other-c0", "a\x00b\x0bc\x1bd", "abcd"),
			c("bidi-bom", "\ufeffa\u202eb\u2066c", "abc"),
			c("c1", "a\u0085b", "ab"),
			c("trim", " \n\ta\n \t", "a"),
			c("interior-blank-lines", "a\n\n\nb", "a\n\n\nb"),
			c("cr-before-removal", "a\r\x07\nb", "a\n\nb"),
		}},
	}
}

func escapeLabel(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

func otpauthStrictBlock() object {
	sec := "?secret=" + v2SecretB32
	type want struct {
		typ, issuer, account, algorithm string
		digits                          int
		counter                         int64
	}
	valid := func(name, uri string, w want) object {
		return object{
			{"name", name}, {"uri", uri}, {"type", w.typ}, {"issuer", w.issuer}, {"account", w.account},
			{"secret_b32", v2SecretB32}, {"algorithm", w.algorithm}, {"digits", w.digits},
			{"period", 30}, {"counter", w.counter},
		}
	}
	inv := func(name, uri string) object { return object{{"name", name}, {"uri", uri}} }
	return object{
		{"valid", []object{
			valid("scheme-uppercase", "OTPAUTH://totp/ACME:alice"+sec+"&issuer=ACME", want{"totp", "ACME", "alice", "SHA1", 6, 0}),
			valid("scheme-host-mixed-case", "OtpAuth://TOTP/ACME:alice"+sec, want{"totp", "ACME", "alice", "SHA1", 6, 0}),
			valid("hotp-counter-zero", "otpauth://hotp/Bank:ops"+sec+"&counter=0", want{"hotp", "Bank", "ops", "SHA1", 6, 0}),
			valid("hotp-counter-max", "otpauth://hotp/Bank:ops"+sec+"&counter=9223372036854775806", want{"hotp", "Bank", "ops", "SHA1", 6, account.MaxCounter}),
			valid("plus-then-percent", "otpauth://totp/Acme%20Corp%2B:alice"+sec+"&issuer=Acme+Corp%2B", want{"totp", "Acme Corp+", "alice", "SHA1", 6, 0}),
			valid("text-cleaned", "otpauth://totp/"+escapeLabel("\u202eGit\x07Hub ")+":"+escapeLabel("\ufeffjohn\u200d\u0085")+sec, want{"totp", "GitHub", "john\u200d", "SHA1", 6, 0}),
			valid("marks-kept", "otpauth://totp/"+escapeLabel("\u0634\u0631\u0643\u0629\u200f")+":"+escapeLabel("\u200eali")+sec, want{"totp", "\u0634\u0631\u0643\u0629\u200f", "\u200eali", "SHA1", 6, 0}),
		}},
		{"invalid", []object{
			inv("label-bad-escape", "otpauth://totp/Ac%ZZme:alice"+sec),
			inv("label-truncated-escape", "otpauth://totp/ACME:alice%2"+sec),
			inv("issuer-bad-escape", "otpauth://totp/ACME:alice"+sec+"&issuer=%ZZ"),
			inv("secret-bad-escape", "otpauth://totp/ACME:alice?secret=%G1"),
			inv("unknown-param-bad-escape", "otpauth://totp/ACME:alice"+sec+"&x-note=%ZZ"),
			inv("counter-int64-max", "otpauth://hotp/Bank:ops"+sec+"&counter=9223372036854775807"),
			inv("counter-overflow", "otpauth://hotp/Bank:ops"+sec+"&counter=9223372036854775808"),
			inv("counter-negative", "otpauth://hotp/Bank:ops"+sec+"&counter=-1"),
			inv("counter-fraction", "otpauth://hotp/Bank:ops"+sec+"&counter=1.5"),
			inv("counter-hex", "otpauth://hotp/Bank:ops"+sec+"&counter=0x10"),
			inv("counter-missing", "otpauth://hotp/Bank:ops"+sec),
		}},
	}
}

var migSecret = []byte("Hello!\xde\xad\xbe\xef")

type migEntry struct {
	name, issuer          string
	algorithm, digits, ty uint64
	counter               uint64
	hasCounter            bool
}

func (e migEntry) encode() []byte {
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	b = protowire.AppendBytes(b, migSecret)
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	b = protowire.AppendString(b, e.name)
	if e.issuer != "" {
		b = protowire.AppendTag(b, 3, protowire.BytesType)
		b = protowire.AppendString(b, e.issuer)
	}
	for _, f := range []struct {
		num protowire.Number
		v   uint64
	}{{4, e.algorithm}, {5, e.digits}, {6, e.ty}} {
		b = protowire.AppendTag(b, f.num, protowire.VarintType)
		b = protowire.AppendVarint(b, f.v)
	}
	if e.hasCounter {
		b = protowire.AppendTag(b, 7, protowire.VarintType)
		b = protowire.AppendVarint(b, e.counter)
	}
	return b
}

func migTrailer(b []byte) []byte {
	for _, f := range []struct {
		num protowire.Number
		v   uint64
	}{{2, 1}, {3, 1}, {4, 0}, {5, 12345}} {
		b = protowire.AppendTag(b, f.num, protowire.VarintType)
		b = protowire.AppendVarint(b, f.v)
	}
	return b
}

func migPayload(entries ...[]byte) []byte {
	var b []byte
	for _, e := range entries {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, e)
	}
	return migTrailer(b)
}

func migURI(scheme, host string, payload []byte) string {
	return scheme + "://" + host + "?data=" + url.QueryEscape(b64(payload))
}

func migrationStrictBlock() object {
	uri := func(entries ...migEntry) string {
		enc := make([][]byte, len(entries))
		for i, e := range entries {
			enc[i] = e.encode()
		}
		return migURI("otpauth-migration", "offline", migPayload(enc...))
	}
	type want struct {
		typ, issuer, account, algorithm string
		digits                          int
		counter                         int64
	}
	acct := func(w want) object {
		return object{
			{"type", w.typ}, {"issuer", w.issuer}, {"account", w.account}, {"secret_b32", v2SecretB32},
			{"algorithm", w.algorithm}, {"digits", w.digits}, {"counter", w.counter},
		}
	}
	valid := func(name, u string, ws ...want) object {
		accts := make([]object, len(ws))
		for i, w := range ws {
			accts[i] = acct(w)
		}
		return object{{"name", name}, {"uri", u}, {"accounts", accts}}
	}
	inv := func(name, u string) object { return object{{"name", name}, {"uri", u}} }

	alice := migEntry{name: "alice", issuer: "ACME", algorithm: 1, digits: 1, ty: 2}
	aliceWant := want{"totp", "ACME", "alice", "SHA1", 6, 0}
	named := func(name, issuer string) migEntry {
		e := alice
		e.name, e.issuer = name, issuer
		return e
	}
	withEnums := func(alg, digits, ty uint64) migEntry {
		e := alice
		e.algorithm, e.digits, e.ty = alg, digits, ty
		return e
	}
	hotp := func(issuer string, counter uint64) migEntry {
		return migEntry{name: "ops", issuer: issuer, algorithm: 1, digits: 1, ty: 1, counter: counter, hasCounter: true}
	}
	steamTOTP := migEntry{name: "gamer", issuer: "STEAM", algorithm: 1, digits: 1, ty: 2}
	steamHOTP := migEntry{name: "gamer", issuer: "Steam", algorithm: 1, digits: 1, ty: 1, counter: 3, hasCounter: true}
	bob := migEntry{name: "bob", issuer: "Corp", algorithm: 2, digits: 1, ty: 2}

	prefix := func(fields func([]byte) []byte) []byte {
		b := protowire.AppendTag(nil, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, migSecret)
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendString(b, "alice")
		return fields(b)
	}
	rawEntry := func(e []byte) string { return migURI("otpauth-migration", "offline", migPayload(e)) }
	aliceBytes := alice.encode()

	return object{
		{"valid", []object{
			valid("scheme-host-uppercase", migURI("OTPAUTH-MIGRATION", "OFFLINE", migPayload(aliceBytes)), aliceWant),
			valid("unspecified-enums", uri(withEnums(0, 0, 0)), aliceWant),
			valid("name-split-no-issuer", uri(named("ACME:alice", "")), aliceWant),
			valid("issuer-replaces-prefix", uri(named("Old:alice", "New")), want{"totp", "New", "alice", "SHA1", 6, 0}),
			valid("split-first-colon", uri(named("ACME:alice:work", "")), want{"totp", "ACME", "alice:work", "SHA1", 6, 0}),
			valid("blank-issuer-keeps-prefix", uri(named("ACME:alice", "\u202e ")), aliceWant),
			valid("text-cleaned", uri(named("\ufeffjohn\x07", " \u202eGitHub\u2069 ")), want{"totp", "GitHub", "john", "SHA1", 6, 0}),
			valid("steam-totp", uri(steamTOTP), want{"steam", "STEAM", "gamer", "SHA1", 6, 0}),
			valid("steam-hotp-stays-hotp", uri(steamHOTP), want{"hotp", "Steam", "gamer", "SHA1", 6, 3}),
			valid("hotp-counter-max", uri(hotp("Bank", uint64(account.MaxCounter))), want{"hotp", "Bank", "ops", "SHA1", 6, account.MaxCounter}),
			valid("sha512-eight-digits", uri(withEnums(3, 2, 2)), want{"totp", "ACME", "alice", "SHA512", 8, 0}),
			valid("multiple-entries", uri(alice, bob), aliceWant, want{"totp", "Corp", "bob", "SHA256", 6, 0}),
		}},
		{"invalid", []object{
			inv("algorithm-md5", uri(withEnums(4, 1, 2))),
			inv("algorithm-unknown", uri(withEnums(5, 1, 2))),
			inv("digits-unknown", uri(withEnums(1, 3, 2))),
			inv("type-unknown", uri(withEnums(1, 1, 3))),
			inv("counter-uint64-max", uri(hotp("Bank", math.MaxUint64))),
			inv("counter-int64-max", uri(hotp("Bank", math.MaxInt64))),
			inv("entry-wrong-wire-type", rawEntry(prefix(func(b []byte) []byte {
				b = protowire.AppendTag(b, 4, protowire.BytesType)
				return protowire.AppendBytes(b, []byte{1})
			}))),
			inv("payload-wrong-wire-type", migURI("otpauth-migration", "offline", migTrailer(protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 1)))),
			inv("varint-too-long", rawEntry(prefix(func(b []byte) []byte {
				b = protowire.AppendTag(b, 4, protowire.VarintType)
				return append(append(b, bytes.Repeat([]byte{0x80}, 10)...), 0x00)
			}))),
			inv("varint-tenth-byte-overflow", rawEntry(prefix(func(b []byte) []byte {
				b = protowire.AppendTag(b, 4, protowire.VarintType)
				return append(append(b, bytes.Repeat([]byte{0xff}, 9)...), 0x02)
			}))),
			inv("length-past-end", migURI("otpauth-migration", "offline", append(protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.BytesType), uint64(len(aliceBytes)+5)), aliceBytes...))),
			inv("length-above-int64", migURI("otpauth-migration", "offline", append(protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.BytesType), 1<<63), aliceBytes...))),
		}},
	}
}

func hotpAccount(counter int64) account.Account {
	a := v2Account("40")
	a.Type, a.Issuer, a.Account, a.Counter = account.HOTP, "Bank", "ops", counter
	return a
}

func hotpCounterRangeBlock() object {
	accept := make([]object, 0, 3)
	for _, c := range []int64{0, 5, account.MaxCounter} {
		accept = append(accept, object{{"counter", c}, {"payload", string(mustCanonical(hotpAccount(c)))}})
	}
	reject := make([]object, 0, 2)
	for _, c := range []int64{-1, math.MaxInt64} {
		reject = append(reject, object{{"counter", c}, {"payload", payloadText(accountObject(hotpAccount(0)).set("counter", c))}, {"error", "counter_range"}})
	}
	advance := make([]object, 0, 3)
	for _, c := range []int64{0, 41, account.MaxCounter - 1} {
		advance = append(advance, object{{"from", c}, {"to", c + 1}})
	}
	secret := []byte("12345678901234567890")
	atMax, err := otp.HOTP(secret, uint64(account.MaxCounter), 6, otp.SHA1)
	if err != nil {
		panic(err)
	}
	refused := make([]object, 0, 2)
	for _, c := range []int64{-1, math.MaxInt64} {
		a := hotpAccount(0)
		a.Counter = c
		_, err := code.For(a, time.Unix(v2Created, 0))
		if !errors.Is(err, account.ErrCounterRange) {
			panic(fmt.Sprintf("vectorgen: code at counter %d: %v", c, err))
		}
		refused = append(refused, object{{"counter", c}, {"error", "counter_range"}, {"message", err.Error()}})
	}
	return object{
		{"max_counter", account.MaxCounter},
		{"payload_accept", accept},
		{"payload_reject", reject},
		{"advance", advance},
		{"advance_refused", []int64{account.MaxCounter}},
		{"code_at_max", object{
			{"secret_b64", b64(secret)}, {"counter", account.MaxCounter}, {"digits", 6},
			{"algorithm", "SHA1"}, {"code", atMax},
		}},
		{"code_refused", object{
			{"_note", "the payload_accept account with each counter; code generation is refused"},
			{"cases", refused},
		}},
	}
}

func unknownTypeBlock() object {
	future := accountObject(v2Account("60")).set("type", "future-otp").with("x_future", object{{"kept", true}})
	missing := accountObject(v2Account("61")).drop("type")
	payload := string(marshal([]object{future, missing}))
	accts, err := account.ParseCanonicalJSON([]byte(payload))
	if err != nil {
		panic(err)
	}
	cases := make([]object, 0, len(accts))
	for _, a := range accts {
		verr := a.Validate()
		_, cerr := code.For(a, time.Unix(v2Created, 0))
		var unknown *code.UnknownTypeError
		if verr == nil || !errors.As(cerr, &unknown) {
			panic(fmt.Sprintf("vectorgen: account %q of type %q: validate %v, code %v", a.ID, a.Type, verr, cerr))
		}
		cases = append(cases, object{
			{"id", a.ID}, {"type", string(a.Type)},
			{"validate_error", verr.Error()}, {"code_error", "unknown_type"}, {"code_message", cerr.Error()},
		})
	}
	return object{
		{"_note", "the payload decodes; re-encoding gives expected_canonical_json (a missing type is written as an empty string); validation and code generation refuse each account"},
		{"payload", payload},
		{"expected_canonical_json", string(mustCanonical(accts...))},
		{"accounts", cases},
	}
}

func otpTimeRangeBlock() object {
	secret := []byte("12345678901234567890")
	accepted := make([]object, 0, 5)
	for _, at := range []int64{0, 29, 30, v2Created, 9223372036854774784} {
		t := time.Unix(at, 0)
		totp, err := otp.TOTP(secret, t, 30, 6, otp.SHA1)
		if err != nil {
			panic(err)
		}
		steam, err := otp.Steam(secret, t)
		if err != nil {
			panic(err)
		}
		accepted = append(accepted, object{{"time", at}, {"totp", totp}, {"steam", steam}})
	}
	refused := []int64{-1, -30, math.MinInt64}
	for _, at := range refused {
		if _, err := otp.TOTP(secret, time.Unix(at, 0), 30, 6, otp.SHA1); !errors.Is(err, otp.ErrTimeRange) {
			panic(fmt.Sprintf("vectorgen: totp at %d: %v", at, err))
		}
		if _, err := otp.Steam(secret, time.Unix(at, 0)); !errors.Is(err, otp.ErrTimeRange) {
			panic(fmt.Sprintf("vectorgen: steam at %d: %v", at, err))
		}
	}
	return object{
		{"_note", "time is Unix seconds; 9223372036854774784 is the largest value both an int64 and a double hold exactly; each refused time gives error time_range for TOTP and Steam"},
		{"secret_b64", b64(secret)}, {"algorithm", "SHA1"}, {"digits", 6}, {"period", 30},
		{"accepted", accepted},
		{"refused", refused},
		{"error", "time_range"},
		{"message", otp.ErrTimeRange.Error()},
	}
}

type orderAccount struct {
	id, issuer, account string
	order               int
	deletedAt           int64
}

func displayOrderBlock() object {
	c := func(name string, accts []orderAccount, want ...string) object {
		list := make([]object, len(accts))
		for i, a := range accts {
			o := object{{"id", a.id}, {"issuer", a.issuer}, {"account", a.account}}
			if a.order != 0 {
				o = o.with("order", a.order)
			}
			if a.deletedAt != 0 {
				o = o.with("deleted_at", a.deletedAt)
			}
			list[i] = o
		}
		return object{{"name", name}, {"accounts", list}, {"expected_ids", want}}
	}
	return object{{"cases", []object{
		c("order-first", []orderAccount{{"a", "Alpha", "x", 0, 0}, {"b", "Zeta", "x", 2, 0}, {"c", "Mid", "x", 1, 0}}, "c", "b", "a"),
		c("natural-digits", []orderAccount{{"a", "Acme 10", "x", 0, 0}, {"b", "Acme 9", "x", 0, 0}, {"c", "Acme 100", "x", 0, 0}}, "b", "a", "c"),
		c("case-insensitive", []orderAccount{{"a", "beta", "x", 0, 0}, {"b", "Alpha", "x", 0, 0}, {"c", "alpha2", "x", 0, 0}}, "b", "c", "a"),
		c("issuer-then-account", []orderAccount{{"a", "ACME", "bob", 0, 0}, {"b", "ACME", "alice", 0, 0}, {"c", "AAA", "zed", 0, 0}}, "c", "b", "a"),
		c("id-tiebreak", []orderAccount{{"id-3", "ACME", "x", 0, 0}, {"id-1", "acme", "x", 0, 0}, {"id-2", "Acme", "x", 0, 0}}, "id-1", "id-2", "id-3"),
		c("order-tie-by-id", []orderAccount{{"b", "Alpha", "x", 1, 0}, {"a", "Zeta", "x", 1, 0}}, "a", "b"),
		c("trashed-excluded", []orderAccount{{"a", "Alpha", "x", 0, v2Deleted}, {"b", "Beta", "x", 0, 0}, {"c", "Gamma", "x", 1, v2Deleted}}, "b"),
		c("empty-issuer-first", []orderAccount{{"a", "Acme", "x", 0, 0}, {"b", "", "zz", 0, 0}}, "b", "a"),
		c("multi-digit-runs", []orderAccount{{"a", "v1.10", "x", 0, 0}, {"b", "v1.2", "x", 0, 0}, {"c", "v1.9", "x", 0, 0}}, "b", "c", "a"),
	}}}
}

func rotationBlock() object {
	dek := pattern(32, 0x20)
	acct := v2Account("50")
	acct.Note = "rotated"
	canonical := mustCanonical(acct)
	nonce := pattern(24, 0x58)
	wraps := []any{passWrap(dek, v2Passphrase, v2LowCost, v2Salt, pattern(24, 0x38)), seWrapObject(), unknownWrap(1)}
	env := envelopeObject(2, wraps, nonce, xseal(dek, nonce, canonical)).with("x_future", object{{"note", "kept"}})
	return object{
		{"passphrase", v2Passphrase},
		{"new_passphrase", v2NewPassphrase},
		{"old_dek_b64", b64(dek)},
		{"envelope", env},
		{"expected_canonical_json", string(canonical)},
		{"expected_version", 2},
		{"expected_removed", []string{"secure-enclave", "hardware-key"}},
		{"expected_wrap_types", []string{"passphrase"}},
		{"kept_top_level", []string{"x_future"}},
	}
}

func rotationExtraPassphraseBlock() object {
	dek := pattern(32, 0x21)
	acct := v2Account("51")
	acct.Note = "rotated"
	canonical := mustCanonical(acct)
	nonce := pattern(24, 0x5a)
	wraps := []any{
		passWrap(dek, "another passphrase that also opens", v2LowCost, pattern(16, 0x90), pattern(24, 0x3a)),
		passWrap(dek, v2Passphrase, v2LowCost, v2Salt, pattern(24, 0x39)),
		seWrapObject(),
		passWrap(dek, "a third passphrase that also opens", v2LowCost, pattern(16, 0x91), pattern(24, 0x3b)),
		unknownWrap(1),
	}
	env := envelopeObject(2, wraps, nonce, xseal(dek, nonce, canonical)).with("x_future", object{{"note", "kept"}})
	return object{
		{"passphrase", v2Passphrase},
		{"new_passphrase", v2NewPassphrase},
		{"old_dek_b64", b64(dek)},
		{"envelope", env},
		{"expected_canonical_json", string(canonical)},
		{"expected_version", 2},
		{"expected_removed", []string{"passphrase", "secure-enclave", "hardware-key"}},
		{"expected_wrap_types", []string{"passphrase"}},
		{"kept_top_level", []string{"x_future"}},
	}
}

func edgeAccountsV2() []account.Account {
	return []account.Account{
		{
			ID: "00000000-0000-4000-8000-000000000100", Type: account.TOTP,
			Issuer: "A\"B\\C/D<e>&f \u00e9\U0001F510", Account: "line\u2028sep\u2029para\x01ctl",
			Secret: pattern(20, 0xec), Algorithm: "SHA1", Digits: 6, Period: 30,
			Folder: "Work/\u05d7\u05d1\u05e8\u05d4", Handle: "ev", Tags: []string{"t<1>", "t&2", "\u2028"},
			Pinned: true, CreatedAt: v2Created, UpdatedAt: v2Deleted, DeletedAt: v2Deleted,
			Note: "first\nsecond\tthird \"q\" \\ \b\f\x1f\x7f end", Order: 7,
			Extra: map[string]json.RawMessage{
				"x_list": json.RawMessage(`[1,-2,"s",true,false,null,[],{}]`),
				"x_obj":  json.RawMessage(marshal(object{{"b", object{{"z", 1}, {"a", []any{object{{"y", true}, {"x", nil}}}}}}, {"a", "\u2028"}})),
				"x_str":  json.RawMessage(marshal("c\x01\b\f\x1f\x7f\u2028<&>")),
			},
		},
		{
			ID: "00000000-0000-4000-8000-000000000101", Type: account.HOTP,
			Issuer: "Bank", Account: "ops", Secret: pattern(64, 0x40), Algorithm: "SHA512",
			Digits: 8, Period: 30, Counter: account.MaxCounter, Order: 1,
			CreatedAt: v2Created, UpdatedAt: v2Created,
		},
	}
}

func edgeCanonicalV2() []byte {
	return mustCanonical(edgeAccountsV2()...)
}
