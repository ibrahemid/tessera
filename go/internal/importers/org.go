package importers

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/ibrahemid/tessera/go/internal/account"
)

func rawString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func rawIsTrue(raw json.RawMessage) bool {
	return bytes.Equal(raw, []byte("true"))
}

func rawIsPositive(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return false
	}
	mantissa, _, _ := strings.Cut(strings.ToLower(string(raw)), "e")
	return strings.ContainsAny(mantissa, "123456789")
}

func rawElements(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var out []json.RawMessage
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func rawObject(raw json.RawMessage, v any) bool {
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func rawStrings(raw json.RawMessage) []string {
	var out []string
	for _, e := range rawElements(raw) {
		if s, ok := rawString(e); ok {
			out = append(out, s)
		}
	}
	return out
}

type groupObject interface {
	idName() (id, name json.RawMessage)
}

type idGroup struct {
	ID   json.RawMessage `json:"id"`
	Name json.RawMessage `json:"name"`
}

func (g idGroup) idName() (json.RawMessage, json.RawMessage) { return g.ID, g.Name }

type uuidGroup struct {
	UUID json.RawMessage `json:"uuid"`
	Name json.RawMessage `json:"name"`
}

func (g uuidGroup) idName() (json.RawMessage, json.RawMessage) { return g.UUID, g.Name }

func indexGroups[G groupObject](raw json.RawMessage) map[string]string {
	idx := map[string]string{}
	for _, e := range rawElements(raw) {
		var g G
		if !rawObject(e, &g) {
			continue
		}
		idRaw, nameRaw := g.idName()
		id, ok := rawString(idRaw)
		if !ok {
			continue
		}
		name, ok := rawString(nameRaw)
		if !ok {
			continue
		}
		idx[id] = name
	}
	return idx
}

func groupName(names []string, idx map[string]string, raw json.RawMessage) []string {
	id, ok := rawString(raw)
	if !ok {
		return names
	}
	if name, ok := idx[id]; ok {
		names = append(names, name)
	}
	return names
}

func groupNames(names []string, idx map[string]string, raw json.RawMessage) []string {
	for _, e := range rawElements(raw) {
		names = groupName(names, idx, e)
	}
	return names
}

func cleanTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tags {
		t = account.CleanText(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func placeGroups(names []string) (folder string, tags []string) {
	clean := cleanTags(names)
	switch len(clean) {
	case 0:
		return "", nil
	case 1:
		return clean[0], nil
	default:
		return clean[0], clean[1:]
	}
}
