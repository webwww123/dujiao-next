package router

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	legacyPurchaseTimestampHeader = "X-XiaoEnAI-Timestamp"
	legacyPurchaseNonceHeader     = "X-XiaoEnAI-Nonce"
	legacyPurchaseSignatureHeader = "X-XiaoEnAI-Signature"
	legacyPurchaseMaxBodyBytes    = 64 * 1024
)

// LegacyPurchaseVerifyAuthMiddleware 校验 XiaoEnAI 发来的内部订单核验请求。
// 签名原文与客户端约定为：timestamp + "\\n" + nonce + "\\n" + body。
func LegacyPurchaseVerifyAuthMiddleware(secret string, maxSkewSeconds int) gin.HandlerFunc {
	secret = strings.TrimSpace(secret)
	if maxSkewSeconds <= 0 {
		maxSkewSeconds = 300
	}

	return func(c *gin.Context) {
		if secret == "" {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}

		timestampRaw := strings.TrimSpace(c.GetHeader(legacyPurchaseTimestampHeader))
		nonce := strings.TrimSpace(c.GetHeader(legacyPurchaseNonceHeader))
		signatureRaw := strings.TrimSpace(c.GetHeader(legacyPurchaseSignatureHeader))
		timestamp, err := strconv.ParseInt(timestampRaw, 10, 64)
		if err != nil || timestamp <= 0 || nonce == "" || len(nonce) > 128 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if diff := time.Now().Unix() - timestamp; diff > int64(maxSkewSeconds) || diff < -int64(maxSkewSeconds) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		signature, err := hex.DecodeString(signatureRaw)
		if err != nil || len(signature) != sha256.Size {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		var body []byte
		if c.Request.Body != nil {
			body, err = io.ReadAll(io.LimitReader(c.Request.Body, legacyPurchaseMaxBodyBytes+1))
		}
		if err != nil || len(body) > legacyPurchaseMaxBodyBytes {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(timestampRaw + "\n" + nonce + "\n"))
		_, _ = mac.Write(body)
		if !hmac.Equal(signature, mac.Sum(nil)) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Next()
	}
}
