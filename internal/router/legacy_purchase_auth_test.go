package router

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func legacyTestSignature(secret, timestamp, nonce, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "\n" + nonce + "\n" + body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestLegacyPurchaseVerifyAuthMiddlewareAcceptsSignedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/verify", LegacyPurchaseVerifyAuthMiddleware("shared-secret", 300), func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.Data(http.StatusOK, "application/json", body)
	})

	body := `{"proof_type":"dujiao_order","order_no":"DJ-1"}`
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "nonce-1"
	req := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader(body))
	req.Header.Set(legacyPurchaseTimestampHeader, timestamp)
	req.Header.Set(legacyPurchaseNonceHeader, nonce)
	req.Header.Set(legacyPurchaseSignatureHeader, legacyTestSignature("shared-secret", timestamp, nonce, body))
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	if resp.Body.String() != body {
		t.Fatalf("body was not restored for handler: %s", resp.Body.String())
	}
}

func TestLegacyPurchaseVerifyAuthMiddlewareRejectsInvalidOrStaleSignatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/verify", LegacyPurchaseVerifyAuthMiddleware("shared-secret", 60), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name      string
		timestamp int64
		signature string
	}{
		{name: "bad signature", timestamp: time.Now().Unix(), signature: "00"},
		{name: "stale timestamp", timestamp: time.Now().Add(-2 * time.Minute).Unix(), signature: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := `{}`
			ts := strconv.FormatInt(tc.timestamp, 10)
			nonce := "nonce-1"
			signature := tc.signature
			if signature == "" {
				signature = legacyTestSignature("shared-secret", ts, nonce, body)
			}
			req := httptest.NewRequest(http.MethodPost, "/verify", strings.NewReader(body))
			req.Header.Set(legacyPurchaseTimestampHeader, ts)
			req.Header.Set(legacyPurchaseNonceHeader, nonce)
			req.Header.Set(legacyPurchaseSignatureHeader, signature)
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d", resp.Code)
			}
		})
	}
}

func TestLegacyPurchaseVerifyAuthMiddlewareReportsMissingSecretAsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/verify", LegacyPurchaseVerifyAuthMiddleware("", 300), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/verify", nil)
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", resp.Code)
	}
}
