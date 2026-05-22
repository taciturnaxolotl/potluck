package v1

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorEnvelope(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, 401, "invalid_api_key", "missing bearer token")

	var got errorEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error.Type != "authentication_error" {
		t.Errorf("type=%q want authentication_error", got.Error.Type)
	}
	if got.Error.Code != "invalid_api_key" {
		t.Errorf("code=%q want invalid_api_key", got.Error.Code)
	}
	if !strings.Contains(got.Error.Message, "missing") {
		t.Errorf("message=%q missing keyword", got.Error.Message)
	}
}

func TestErrorEnvelopeUnknownCode(t *testing.T) {
	rr := httptest.NewRecorder()
	WriteError(rr, 500, "novel_failure_mode", "wat")
	var got errorEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Type != "server_error" {
		t.Errorf("unknown code should default to server_error type, got %q", got.Error.Type)
	}
	if got.Error.Code != "novel_failure_mode" {
		t.Errorf("internal code should be passed through verbatim, got %q", got.Error.Code)
	}
}
