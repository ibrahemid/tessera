package code

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/ibrahemid/tessera/go/internal/account"
	"github.com/ibrahemid/tessera/go/internal/spectest"
)

type codeVectors struct {
	HOTPCounterRange struct {
		PayloadAccept []struct {
			Payload string `json:"payload"`
		} `json:"payload_accept"`
		CodeRefused struct {
			Cases []struct {
				Counter int64  `json:"counter"`
				Error   string `json:"error"`
				Message string `json:"message"`
			} `json:"cases"`
		} `json:"code_refused"`
		CodeAtMax struct {
			SecretB64 string `json:"secret_b64"`
		} `json:"code_at_max"`
	} `json:"hotp_counter_range"`
	UnknownType struct {
		Payload  string `json:"payload"`
		Accounts []struct {
			ID          string `json:"id"`
			CodeError   string `json:"code_error"`
			CodeMessage string `json:"code_message"`
		} `json:"accounts"`
	} `json:"unknown_type"`
}

func loadCodeVectors(t *testing.T) codeVectors {
	t.Helper()
	var v codeVectors
	spectest.Load(t, &v)
	return v
}

func TestCodeRefusedVectors(t *testing.T) {
	v := loadCodeVectors(t).HOTPCounterRange
	if len(v.PayloadAccept) == 0 || len(v.CodeRefused.Cases) == 0 {
		t.Fatal("hotp_counter_range.code_refused is incomplete")
	}
	accts, err := account.ParseCanonicalJSON([]byte(v.PayloadAccept[0].Payload))
	if err != nil || len(accts) != 1 {
		t.Fatalf("parse payload_accept: %v", err)
	}
	secret, err := base64.StdEncoding.DecodeString(v.CodeAtMax.SecretB64)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range v.CodeRefused.Cases {
		a := accts[0]
		a.Secret = secret
		a.Counter = c.Counter
		_, err := For(a, time.Unix(1700000000, 0))
		if c.Error != "counter_range" || !errors.Is(err, account.ErrCounterRange) || err.Error() != c.Message {
			t.Fatalf("counter %d: err = %v, want %s %q", c.Counter, err, c.Error, c.Message)
		}
	}
}

func TestUnknownTypeCodeVectors(t *testing.T) {
	v := loadCodeVectors(t).UnknownType
	accts, err := account.ParseCanonicalJSON([]byte(v.Payload))
	if err != nil || len(accts) != len(v.Accounts) || len(accts) == 0 {
		t.Fatalf("parse: %d accounts, %v", len(accts), err)
	}
	for i, c := range v.Accounts {
		_, err := For(accts[i], time.Unix(1700000000, 0))
		var unknown *UnknownTypeError
		if c.CodeError != "unknown_type" || !errors.As(err, &unknown) || err.Error() != c.CodeMessage {
			t.Fatalf("account %q: err = %v, want %s %q", c.ID, err, c.CodeError, c.CodeMessage)
		}
	}
}
