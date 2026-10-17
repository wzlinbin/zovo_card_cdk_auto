package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/tuzi/cdk-recharge-system/internal/cardplatform"
	"github.com/tuzi/cdk-recharge-system/internal/db"
)

// PublicCDKSubmitRequest is the minimal request used by an external site.
// The first call contains only code and returns the normal preview result.
// The second call contains the same code and the user's session, then runs
// the existing preflight -> redeem flow.
type PublicCDKSubmitRequest struct {
	Code    string `json:"code"`
	Session string `json:"session"`
}

const publicCDKSubmitBodyLimit = 128 << 10

// PublicCDKSubmit POST /api/v1/public/cdk/submit
//
// This is a thin, one-code-at-a-time wrapper for callers that do not want to
// reproduce the browser flow. With only code it performs preview/validation;
// with code + session it continues through preflight and redeem. It does not
// create a new authentication or batch-management layer.
func PublicCDKSubmit(c *gin.Context) {
	var req PublicCDKSubmitRequest
	if err := decodePublicCDKSubmit(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	code := strings.TrimSpace(req.Code)
	if err := validatePublicCDKSubmitValue(code, "code", 256); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	session := strings.TrimSpace(req.Session)
	if session != "" {
		if err := validatePublicCDKSubmitSession(session); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	cli := cardplatform.NewFromSettings()
	device := deviceFrom(c)
	if session == "" {
		st, raw, err := cli.Preview(c.Request.Context(), code, device)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		bindPublicSubmitPreview(code, st, raw)
		proxyPublicJSON(c, st, raw)
		return
	}

	// A prior code-only call normally created this binding. If the caller
	// skipped that step, preview once here so the two-call flow remains simple.
	bind, err := db.GetBindingByCDK(code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 CDK 绑定失败"})
		return
	}
	redemptionToken := ""
	if bind != nil {
		redemptionToken = strings.TrimSpace(bind.RedemptionToken)
	}
	if redemptionToken == "" {
		st, raw, previewErr := cli.Preview(c.Request.Context(), code, device)
		if previewErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": previewErr.Error()})
			return
		}
		bindPublicSubmitPreview(code, st, raw)
		if st < 200 || st >= 300 {
			proxyPublicJSON(c, st, raw)
			return
		}
		redemptionToken = publicSubmitToken(raw)
		if redemptionToken == "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": "预览未返回 redemption_token"})
			return
		}
	}

	credential := map[string]any{
		"mode":    "session",
		"session": session,
	}
	preflightBody := map[string]any{
		"code":             code,
		"redemption_token": redemptionToken,
		"credential":       credential,
	}
	preflightStatus, preflightRaw, err := cli.Preflight(c.Request.Context(), preflightBody, device)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	// Keep the same binding behavior as the existing public preflight route.
	if sess := extractCredentialSession(credential); sess != "" &&
		!cardplatform.IsXPremiumCredential(sess) && !cardplatform.IsGrokCredential(sess) {
		_ = db.BindCDKSession(code, redemptionToken, sess)
	}
	if preflightStatus < 200 || preflightStatus >= 300 {
		proxyPublicJSON(c, preflightStatus, preflightRaw)
		return
	}

	preflightToken := publicSubmitPreflightToken(preflightRaw)
	redeemBody := map[string]any{
		"redemption_token":  redemptionToken,
		"client_request_id": publicSubmitRequestID(code, redemptionToken),
	}
	// Grok's existing flow may redeem with the credential and no preflight
	// token; all other products require the token returned by preflight.
	if preflightToken != "" {
		redeemBody["preflight_token"] = preflightToken
	} else if cardplatform.IsGrokCredential(session) {
		redeemBody["credential"] = credential
	} else {
		c.JSON(http.StatusBadGateway, gin.H{"error": "预检未返回 preflight_token"})
		return
	}

	// Preserve the same site card-selection policy and blocked-card exclusion
	// that the existing public redeem route applies.
	injectRedeemCardPolicy(redeemBody)
	if _, exists := redeemBody["exclude_card_ids"]; !exists {
		if ids, listErr := db.ListActiveBlockedCardIDs(); listErr == nil && len(ids) > 0 {
			redeemBody["exclude_card_ids"] = ids
		}
	}
	redeemStatus, redeemRaw, err := cli.Redeem(c.Request.Context(), redeemBody, device)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	proxyPublicJSON(c, redeemStatus, redeemRaw)
}

func decodePublicCDKSubmit(c *gin.Context, dst *PublicCDKSubmitRequest) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, publicCDKSubmitBodyLimit)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid request")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid request")
	}
	return nil
}

func validatePublicCDKSubmitValue(value, name string, maxBytes int) error {
	if value == "" {
		return fmt.Errorf("%s required", name)
	}
	if len([]byte(value)) > maxBytes {
		return fmt.Errorf("%s too long", name)
	}
	for _, r := range value {
		if r == '\x00' || r == '\r' || r == '\n' || unicode.IsControl(r) {
			return fmt.Errorf("%s contains invalid characters", name)
		}
	}
	return nil
}

// Session payloads may be a pretty-printed JSON session, so preserve ordinary
// JSON whitespace while still rejecting NUL and other non-whitespace controls.
func validatePublicCDKSubmitSession(value string) error {
	if value == "" {
		return fmt.Errorf("session required")
	}
	if len([]byte(value)) > 96<<10 {
		return fmt.Errorf("session too long")
	}
	for _, r := range value {
		if r == '\x00' || (unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r') {
			return fmt.Errorf("session contains invalid characters")
		}
	}
	return nil
}

func bindPublicSubmitPreview(code string, status int, raw json.RawMessage) {
	if status < 200 || status >= 300 {
		return
	}
	if token := publicSubmitToken(raw); token != "" {
		_ = db.BindCDKRedemptionToken(code, token)
	}
}

func publicSubmitToken(raw json.RawMessage) string {
	if token := extractJSONString(raw, "redemption_token", "token"); token != "" {
		return token
	}
	return extractJSONNestedString(raw, "data", "redemption_token")
}

func publicSubmitPreflightToken(raw json.RawMessage) string {
	if token := extractJSONString(raw, "preflight_token"); token != "" {
		return token
	}
	return extractJSONNestedString(raw, "data", "preflight_token")
}

func publicSubmitRequestID(code, redemptionToken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code) + "\x00" + strings.TrimSpace(redemptionToken)))
	return "site-" + hex.EncodeToString(sum[:])[:48]
}
