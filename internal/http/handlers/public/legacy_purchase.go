package public

import (
	"errors"
	"strings"

	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

type legacyPurchaseVerifyRequest struct {
	ProofType           string   `json:"proof_type" binding:"required"`
	OrderNo             string   `json:"order_no"`
	AllowedProductSlugs []string `json:"allowed_product_slugs"`
}

// VerifyLegacyPurchase POST /api/v1/internal/legacy-purchase/verify
// 该接口只由 XiaoEnAI 的签名请求调用，不返回顾客、支付渠道或卡密内容。
func (h *Handler) VerifyLegacyPurchase(c *gin.Context) {
	var req legacyPurchaseVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"status_code": response.CodeBadRequest, "msg": "invalid request"})
		return
	}
	if strings.TrimSpace(req.ProofType) != "dujiao_order" {
		c.JSON(400, gin.H{"status_code": response.CodeBadRequest, "msg": "unsupported proof type"})
		return
	}
	if strings.TrimSpace(req.OrderNo) == "" || len(req.AllowedProductSlugs) == 0 {
		c.JSON(400, gin.H{"status_code": response.CodeBadRequest, "msg": "order number and product allowlist are required"})
		return
	}
	if h.LegacyPurchaseService == nil {
		c.AbortWithStatus(503)
		return
	}

	result, err := h.LegacyPurchaseService.VerifyOrder(req.OrderNo, req.AllowedProductSlugs)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrLegacyPurchaseNotFound), errors.Is(err, service.ErrLegacyPurchaseNotEligible):
			response.Success(c, gin.H{"valid": false})
		default:
			c.AbortWithStatus(503)
		}
		return
	}

	response.Success(c, gin.H{
		"valid":       result.Valid,
		"order_no":    result.OrderNo,
		"source_slug": result.SourceSlug,
	})
}
