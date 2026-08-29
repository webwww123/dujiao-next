package service

import (
	"errors"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
)

// CouponHandoffState describes the result of resolving a coupon source order.
// The state is intentionally explicit so the frontend can distinguish a
// temporarily pending delivery from an unsupported or unsafe mapping.
const (
	CouponHandoffStateUnsupported = "unsupported"
	CouponHandoffStatePending     = "pending"
	CouponHandoffStateReady       = "ready"
	CouponHandoffStateUnavailable = "unavailable"
)

// CouponHandoff is the minimum data needed to continue from a delivered
// coupon order to the matching product checkout. CouponCode is only populated
// after the caller has already passed the normal order ownership check.
type CouponHandoff struct {
	State           string `json:"state"`
	SourceOrderNo   string `json:"source_order_no,omitempty"`
	SourceProductID uint   `json:"source_product_id,omitempty"`
	CouponID        uint   `json:"coupon_id,omitempty"`
	CouponCode      string `json:"coupon_code,omitempty"`
	TargetProductID uint   `json:"target_product_id,omitempty"`
	TargetSlug      string `json:"target_slug,omitempty"`
	TargetSKUID     uint   `json:"target_sku_id,omitempty"`
}

// couponHandoffSpec is a deliberately small, versioned allowlist for the
// current coupon products. It prevents a generic parser from guessing a
// target product from arbitrary delivery text.
type couponHandoffSpec struct {
	CouponID        uint
	TargetProductID uint
	TargetSlug      string
}

// couponHandoffSpecs is based on the live catalog verified before this change.
// If a product is changed or removed, resolution safely falls back to the
// existing manual copy flow until this allowlist is reviewed.
var couponHandoffSpecs = map[uint]couponHandoffSpec{
	12: {CouponID: 2, TargetProductID: 3, TargetSlug: "wuxianliangyueka"},
	14: {CouponID: 6, TargetProductID: 4, TargetSlug: "wuxianliangzhouka"},
	17: {CouponID: 7, TargetProductID: 16, TargetSlug: "gqbxl"},
	23: {CouponID: 8, TargetProductID: 22, TargetSlug: "k26"},
	26: {CouponID: 9, TargetProductID: 25, TargetSlug: "glm30d"},
	28: {CouponID: 10, TargetProductID: 27, TargetSlug: "grok45"},
}

// ResolveCouponHandoff resolves a source coupon order after its normal access
// check has succeeded. It never mutates orders, coupons, or card-secret rows.
func (s *OrderService) ResolveCouponHandoff(order *models.Order) (*CouponHandoff, error) {
	if order == nil {
		return nil, ErrOrderNotFound
	}
	if s.cardSecretRepo == nil || s.couponRepo == nil {
		return nil, errors.New("coupon handoff dependencies unavailable")
	}

	result := &CouponHandoff{State: CouponHandoffStateUnsupported, SourceOrderNo: order.OrderNo}
	parts := couponHandoffOrderParts(order)
	var candidate *couponHandoffCandidate
	for _, part := range parts {
		spec, ok := couponHandoffSpecs[part.item.ProductID]
		if !ok {
			continue
		}
		if candidate != nil {
			// Multiple source coupon items make the code-to-target relation
			// ambiguous; do not guess which code should be applied.
			result.State = CouponHandoffStateUnavailable
			return result, nil
		}
		candidate = &couponHandoffCandidate{part: part, spec: spec}
	}
	if candidate == nil {
		return result, nil
	}

	result.SourceProductID = candidate.part.item.ProductID
	result.CouponID = candidate.spec.CouponID
	result.TargetProductID = candidate.spec.TargetProductID
	result.TargetSlug = candidate.spec.TargetSlug
	if candidate.part.item.Quantity != 1 {
		// The current coupon source products are single-use, single-SKU items.
		// A changed quantity would make the source-secret-to-coupon relation
		// ambiguous, so keep the original manual flow instead of guessing.
		result.State = CouponHandoffStateUnavailable
		return result, nil
	}

	if isCouponHandoffTerminallyUnavailable(order.Status) ||
		isCouponHandoffTerminallyUnavailable(candidate.part.order.Status) {
		result.State = CouponHandoffStateUnavailable
		return result, nil
	}

	fulfillment := candidate.part.order.Fulfillment
	if fulfillment == nil || strings.TrimSpace(fulfillment.Status) != constants.FulfillmentStatusDelivered {
		result.State = CouponHandoffStatePending
		return result, nil
	}

	secrets, err := s.cardSecretRepo.ListByOrderAndStatus(candidate.part.order.ID, models.CardSecretStatusUsed)
	if err != nil {
		return nil, err
	}
	if len(secrets) == 0 {
		// A delivered record without a matching used card secret is not safe
		// to auto-fill. The caller can still expose the existing raw delivery.
		result.State = CouponHandoffStateUnavailable
		return result, nil
	}

	coupon, err := s.couponRepo.GetByID(candidate.spec.CouponID)
	if err != nil {
		return nil, err
	}
	if coupon == nil || strings.TrimSpace(coupon.Code) == "" ||
		!isCouponHandoffCouponUsable(coupon) ||
		!isCouponHandoffCouponApplicable(coupon, candidate.spec.TargetProductID) {
		result.State = CouponHandoffStateUnavailable
		return result, nil
	}

	code := strings.TrimSpace(coupon.Code)
	productSecrets := 0
	matched := 0
	for _, secret := range secrets {
		if secret.ProductID != candidate.part.item.ProductID {
			continue
		}
		if candidate.part.item.SKUID > 0 && secret.SKUID != candidate.part.item.SKUID {
			continue
		}
		productSecrets++
		if strings.Count(secret.Secret, code) != 1 {
			continue
		}
		matched++
	}
	if productSecrets != 1 || matched != 1 {
		result.State = CouponHandoffStateUnavailable
		return result, nil
	}

	result.State = CouponHandoffStateReady
	result.CouponCode = code
	return result, nil
}

func isCouponHandoffCouponUsable(coupon *models.Coupon) bool {
	if coupon == nil || !coupon.IsActive {
		return false
	}
	now := time.Now()
	if coupon.StartsAt != nil && now.Before(*coupon.StartsAt) {
		return false
	}
	if coupon.EndsAt != nil && now.After(*coupon.EndsAt) {
		return false
	}
	return coupon.UsageLimit <= 0 || coupon.UsedCount < coupon.UsageLimit
}

func isCouponHandoffCouponApplicable(coupon *models.Coupon, targetProductID uint) bool {
	if coupon == nil || targetProductID == 0 ||
		strings.ToLower(strings.TrimSpace(coupon.ScopeType)) != constants.ScopeTypeProduct {
		return false
	}
	ids, err := decodeScopeIDs(coupon.ScopeRefIDs)
	if err != nil {
		return false
	}
	_, ok := ids[targetProductID]
	return ok
}

type couponHandoffOrderPart struct {
	order *models.Order
	item  models.OrderItem
}

type couponHandoffCandidate struct {
	part couponHandoffOrderPart
	spec couponHandoffSpec
}

func couponHandoffOrderParts(order *models.Order) []couponHandoffOrderPart {
	if order == nil {
		return nil
	}
	parts := make([]couponHandoffOrderPart, 0)
	if len(order.Children) == 0 {
		for _, item := range order.Items {
			parts = append(parts, couponHandoffOrderPart{order: order, item: item})
		}
		return parts
	}
	for i := range order.Children {
		child := &order.Children[i]
		for _, item := range child.Items {
			parts = append(parts, couponHandoffOrderPart{order: child, item: item})
		}
	}
	return parts
}

func isCouponHandoffTerminallyUnavailable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case constants.OrderStatusCanceled,
		constants.OrderStatusRefunded,
		constants.OrderStatusPartiallyRefunded:
		return true
	default:
		return false
	}
}
