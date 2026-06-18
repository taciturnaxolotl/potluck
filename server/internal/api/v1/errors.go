package v1

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is the OpenAI-compatible error body shape. Every /v1/*
// failure response uses this exactly — clients in the wild assume it.
//
//	{ "error": { "message", "type", "code", "param" } }
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

// codeMap maps potluck-internal codes to (OpenAI type, OpenAI code).
//
// "type" buckets are intentionally narrow:
//   - invalid_request_error  — request the client sent is malformed
//   - authentication_error   — bearer token missing/bad
//   - permission_error       — token valid, but not allowed
//   - insufficient_quota     — pool/balance exhausted
//   - rate_limit_error       — too many requests
//   - server_error           — our problem
//   - api_error              — upstream provider problem (Pioneer)
//
// "code" passes through the internal stable code so client SDKs can
// match without reading messages. Add new internal codes here whenever
// they ship; the package doc comment in v1/server.go is the reminder.
var codeMap = map[string]struct {
	Type string
	Code string
}{
	"invalid_request":    {"invalid_request_error", "invalid_request"},
	"unauthenticated":    {"authentication_error", "unauthenticated"},
	"invalid_api_key":    {"authentication_error", "invalid_api_key"},
	"forbidden":          {"permission_error", "forbidden"},
	"insufficient_funds": {"insufficient_quota", "insufficient_funds"},
	"too_many_streams":   {"rate_limit_error", "too_many_streams"},
	"rate_limited":       {"rate_limit_error", "rate_limited"},
	"provider_down":      {"api_error", "provider_down"},
	"provider_error":     {"api_error", "provider_error"},
	"not_implemented":    {"server_error", "not_implemented"},
}

// WriteError serializes an error in the OpenAI envelope. Exposed so main
// can wire it as the ErrorResponder for /v1/* middleware.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeError(w, status, code, message)
}

// writeError serializes an error in the OpenAI envelope. It satisfies
// middleware.ErrorResponder.
func writeError(w http.ResponseWriter, status int, code, message string) {
	mapping, ok := codeMap[code]
	if !ok {
		mapping.Type = "server_error"
		mapping.Code = code
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Message: message,
		Type:    mapping.Type,
		Code:    mapping.Code,
	}})
}
