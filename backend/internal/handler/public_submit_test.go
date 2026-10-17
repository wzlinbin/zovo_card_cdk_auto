package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicSubmitRequestIDIsStableAndBoundToToken(t *testing.T) {
	a := publicSubmitRequestID("CDK-001", "rt-1")
	if a == "" || a != publicSubmitRequestID("CDK-001", "rt-1") {
		t.Fatalf("request id must be stable: %q", a)
	}
	if a == publicSubmitRequestID("CDK-001", "rt-2") {
		t.Fatal("request id must change when the redemption token changes")
	}
	if len(a) > 80 {
		t.Fatalf("request id exceeds upstream limit: %d", len(a))
	}
}

func TestValidatePublicCDKSubmitValue(t *testing.T) {
	if err := validatePublicCDKSubmitValue("CDK-001", "code", 256); err != nil {
		t.Fatalf("valid code rejected: %v", err)
	}
	for _, value := range []string{"", "CDK\n001", "CDK\x00001"} {
		if err := validatePublicCDKSubmitValue(value, "code", 256); err == nil {
			t.Fatalf("invalid code accepted: %q", value)
		}
	}
	if err := validatePublicCDKSubmitValue(strings.Repeat("x", 5), "code", 4); err == nil {
		t.Fatal("oversized value accepted")
	}
	if err := validatePublicCDKSubmitSession("{\n  \"sessionToken\": \"abc\"\n}"); err != nil {
		t.Fatalf("formatted session rejected: %v", err)
	}
}

func TestPublicSubmitTokenExtraction(t *testing.T) {
	raw := json.RawMessage(`{"data":{"redemption_token":"rt-1","preflight_token":"pf-1"}}`)
	if got := publicSubmitToken(raw); got != "rt-1" {
		t.Fatalf("redemption token = %q", got)
	}
	if got := publicSubmitPreflightToken(raw); got != "pf-1" {
		t.Fatalf("preflight token = %q", got)
	}
}
