package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
)

const maxJSONDepth = 10000

var envelopeKeys = map[string]bool{"version": true, "aead": true, "wraps": true, "payload": true}

type objectWriter struct {
	buf bytes.Buffer
	n   int
	err error
}

func (o *objectWriter) member(key string, value any) {
	if o.err != nil {
		return
	}
	k, err := encodeValue(key)
	if err != nil {
		o.err = err
		return
	}
	v, err := encodeValue(value)
	if err != nil {
		o.err = err
		return
	}
	o.raw(k, v)
}

func (o *objectWriter) raw(key, value []byte) {
	if o.n > 0 {
		o.buf.WriteByte(',')
	}
	o.n++
	o.buf.Write(key)
	o.buf.WriteByte(':')
	o.buf.Write(value)
}

func (o *objectWriter) bytes() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	out := make([]byte, 0, o.buf.Len()+2)
	out = append(out, '{')
	out = append(out, o.buf.Bytes()...)
	return append(out, '}'), nil
}

func encodeValue(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}

func (w wrap) marshal() ([]byte, error) {
	if w.raw != nil {
		return w.raw, nil
	}
	var o objectWriter
	o.member("type", w.Type)
	if w.KDF != "" {
		o.member("kdf", w.KDF)
	}
	if w.Params != nil {
		o.member("params", *w.Params)
	}
	if len(w.Salt) > 0 {
		o.member("salt", w.Salt)
	}
	if len(w.SEKey) > 0 {
		o.member("se_key", w.SEKey)
	}
	o.member("nonce", w.Nonce)
	o.member("ct", w.CT)
	return o.bytes()
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	switch {
	case len(e.Wraps) == 0:
		return nil, ErrNoWraps
	case len(e.Wraps) > maxWraps:
		return nil, ErrTooManyWraps
	}
	var wraps bytes.Buffer
	wraps.WriteByte('[')
	for i, w := range e.Wraps {
		b, err := w.marshal()
		if err != nil {
			return nil, err
		}
		if i > 0 {
			wraps.WriteByte(',')
		}
		wraps.Write(b)
	}
	wraps.WriteByte(']')

	var payload objectWriter
	payload.member("nonce", e.Payload.Nonce)
	payload.member("ct", e.Payload.CT)
	payloadJSON, err := payload.bytes()
	if err != nil {
		return nil, err
	}

	var o objectWriter
	o.member("version", e.Version)
	o.member("aead", e.AEAD)
	o.raw([]byte(`"wraps"`), wraps.Bytes())
	o.raw([]byte(`"payload"`), payloadJSON)
	keys := make([]string, 0, len(e.extra))
	for k := range e.extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.member(k, e.extra[k])
	}
	return o.bytes()
}

// Marshal serializes the envelope to JSON bytes for storage.
func (e *Envelope) Marshal() ([]byte, error) {
	compact, err := e.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Unmarshal parses an envelope from JSON bytes.
func Unmarshal(data []byte) (*Envelope, error) {
	var e Envelope
	if err := e.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	return &e, nil
}

func (e *Envelope) UnmarshalJSON(data []byte) error {
	if err := checkEnvelopeJSON(data); err != nil {
		return err
	}
	top, err := decodeObject("envelope", bytes.TrimSpace(data))
	if err != nil {
		return err
	}
	var out Envelope
	vraw, ok := top["version"]
	if !ok {
		return corruptf("missing key %q", "version")
	}
	switch string(vraw) {
	case "1":
		out.Version = 1
	case "2":
		out.Version = 2
	default:
		return ErrUnsupportedVersion
	}
	if out.AEAD, err = requiredString(top, "aead"); err != nil {
		return err
	}
	if out.AEAD != aeadName {
		return ErrUnsupportedAEAD
	}
	wrapsRaw, ok := top["wraps"]
	if !ok {
		return corruptf("missing key %q", "wraps")
	}
	if !isKind(wrapsRaw, '[') {
		return corruptf("%q is not an array", "wraps")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(wrapsRaw, &items); err != nil {
		return corruptf("wraps: %v", err)
	}
	switch {
	case len(items) == 0:
		return ErrNoWraps
	case len(items) > maxWraps:
		return ErrTooManyWraps
	}
	out.Wraps = make([]wrap, 0, len(items))
	for _, item := range items {
		w, err := parseWrap(item)
		if err != nil {
			return err
		}
		out.Wraps = append(out.Wraps, w)
	}
	payloadRaw, ok := top["payload"]
	if !ok {
		return corruptf("missing key %q", "payload")
	}
	if out.Payload, err = parsePayload(payloadRaw); err != nil {
		return err
	}
	for k, v := range top {
		if envelopeKeys[k] {
			continue
		}
		if out.extra == nil {
			out.extra = make(map[string]json.RawMessage)
		}
		out.extra[k] = compactRaw(v)
	}
	*e = out
	return nil
}

func parseWrap(raw json.RawMessage) (wrap, error) {
	m, err := decodeObject("wrap", raw)
	if err != nil {
		return wrap{}, err
	}
	typ, err := requiredString(m, "type")
	if err != nil {
		return wrap{}, err
	}
	w := wrap{Type: typ, raw: compactRaw(raw)}
	switch typ {
	case passphraseType:
		if w.KDF, err = optionalString(m, "kdf"); err != nil {
			return wrap{}, err
		}
		if p, ok := m["params"]; ok {
			if w.Params, w.paramsErr, err = parseParams(p); err != nil {
				return wrap{}, err
			}
		}
		if w.Salt, err = optionalBase64(m, "salt"); err != nil {
			return wrap{}, err
		}
	case secureEnclaveType:
		if w.KDF, err = optionalString(m, "kdf"); err != nil {
			return wrap{}, err
		}
		if w.SEKey, err = requiredBase64(m, "se_key"); err != nil {
			return wrap{}, err
		}
	default:
		return w, nil
	}
	if w.Nonce, err = requiredNonce(m, "nonce"); err != nil {
		return wrap{}, err
	}
	if w.CT, err = requiredBase64(m, "ct"); err != nil {
		return wrap{}, err
	}
	return w, nil
}

func parseParams(raw json.RawMessage) (*Argon2Params, error, error) {
	m, err := decodeObject("params", raw)
	if err != nil {
		return nil, nil, err
	}
	vals := map[string]int64{}
	var missing error
	for _, k := range []string{"v", "m", "t", "p"} {
		r, ok := m[k]
		if !ok {
			if missing == nil {
				missing = corruptf("argon2 params missing %q", k)
			}
			continue
		}
		n, ok := parseCanonicalInt(r)
		if !ok {
			return nil, nil, corruptf("argon2 %s is not an integer", k)
		}
		vals[k] = n
	}
	if missing != nil {
		return nil, missing, nil
	}
	limits := []struct {
		key string
		max int64
	}{{"m", math.MaxUint32}, {"t", math.MaxUint32}, {"p", math.MaxUint8}}
	for _, l := range limits {
		if n := vals[l.key]; n < 0 || n > l.max {
			return nil, corruptf("argon2 %s=%d out of range", l.key, n), nil
		}
	}
	if v := vals["v"]; v < math.MinInt || v > math.MaxInt {
		return nil, corruptf("argon2 v=%d out of range", v), nil
	}
	return &Argon2Params{
		V: int(vals["v"]),
		M: uint32(vals["m"]),
		T: uint32(vals["t"]),
		P: uint8(vals["p"]),
	}, nil, nil
}

func parsePayload(raw json.RawMessage) (box, error) {
	m, err := decodeObject("payload", raw)
	if err != nil {
		return box{}, err
	}
	for k := range m {
		if k != "nonce" && k != "ct" {
			return box{}, corruptf("unknown payload key %q", k)
		}
	}
	nonce, err := requiredNonce(m, "nonce")
	if err != nil {
		return box{}, err
	}
	ct, err := requiredBase64(m, "ct")
	if err != nil {
		return box{}, err
	}
	return box{Nonce: nonce, CT: ct}, nil
}

func isKind(raw []byte, c byte) bool {
	return len(raw) > 0 && raw[0] == c
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return b.Bytes()
}

func decodeObject(what string, raw []byte) (map[string]json.RawMessage, error) {
	if !isKind(raw, '{') {
		return nil, corruptf("%s is not a JSON object", what)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, corruptf("%s: %v", what, err)
	}
	return m, nil
}

func optionalString(m map[string]json.RawMessage, key string) (string, error) {
	r, ok := m[key]
	if !ok {
		return "", nil
	}
	return decodeString(key, r)
}

func requiredString(m map[string]json.RawMessage, key string) (string, error) {
	r, ok := m[key]
	if !ok {
		return "", corruptf("missing key %q", key)
	}
	return decodeString(key, r)
}

func decodeString(key string, raw json.RawMessage) (string, error) {
	if !isKind(raw, '"') {
		return "", corruptf("%q is not a string", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", corruptf("%q: %v", key, err)
	}
	return s, nil
}

func decodeBase64(key string, raw json.RawMessage) ([]byte, error) {
	s, err := decodeString(key, raw)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || base64.StdEncoding.EncodeToString(b) != s {
		return nil, corruptf("%q is not canonical standard base64", key)
	}
	return b, nil
}

func optionalBase64(m map[string]json.RawMessage, key string) ([]byte, error) {
	r, ok := m[key]
	if !ok {
		return nil, nil
	}
	return decodeBase64(key, r)
}

func requiredBase64(m map[string]json.RawMessage, key string) ([]byte, error) {
	r, ok := m[key]
	if !ok {
		return nil, corruptf("missing key %q", key)
	}
	return decodeBase64(key, r)
}

func requiredNonce(m map[string]json.RawMessage, key string) ([]byte, error) {
	b, err := requiredBase64(m, key)
	if err != nil {
		return nil, err
	}
	if len(b) != nonceLen {
		return nil, corruptf("%q is %d bytes, want %d", key, len(b), nonceLen)
	}
	return b, nil
}

func parseCanonicalInt(raw json.RawMessage) (int64, bool) {
	s := string(raw)
	body := s
	if len(body) > 0 && body[0] == '-' {
		body = body[1:]
	}
	if body == "" || (body[0] == '0' && len(s) != 1) {
		return 0, false
	}
	for i := 0; i < len(body); i++ {
		if body[i] < '0' || body[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func checkEnvelopeJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := walkEnvelope(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return corruptf("trailing data after the envelope")
	}
	return nil
}

func walkEnvelope(dec *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return corruptf("nesting deeper than %d", maxJSONDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return corruptf("%v", err)
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return corruptf("%v", err)
			}
			key, ok := keyTok.(string)
			if !ok {
				return corruptf("object key is not a string")
			}
			if seen[key] {
				return corruptf("duplicate key %q", key)
			}
			seen[key] = true
			if err := walkEnvelope(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walkEnvelope(dec, depth+1); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return corruptf("%v", err)
	}
	return nil
}
