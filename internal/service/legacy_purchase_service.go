package service

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
)

var (
	ErrLegacyPurchaseNotFound    = errors.New("legacy purchase not found")
	ErrLegacyPurchaseNotEligible = errors.New("legacy purchase not eligible")
)

// LegacyPurchaseVerification 是给服务间核验接口返回的最小结果，不暴露订单顾客信息或卡密。
type LegacyPurchaseVerification struct {
	Valid      bool
	OrderNo    string
	SourceSlug string
}

type LegacyPurchaseService struct {
	orderRepo   repository.OrderRepository
	productRepo repository.ProductRepository
}

func NewLegacyPurchaseService(orderRepo repository.OrderRepository, productRepo repository.ProductRepository) *LegacyPurchaseService {
	return &LegacyPurchaseService{orderRepo: orderRepo, productRepo: productRepo}
}

func (s *LegacyPurchaseService) VerifyOrder(orderNo string, allowedSourceSlugs []string) (*LegacyPurchaseVerification, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" || s == nil || s.orderRepo == nil || s.productRepo == nil {
		return nil, ErrLegacyPurchaseNotFound
	}

	allowed := make(map[string]struct{}, len(allowedSourceSlugs))
	for _, slug := range allowedSourceSlugs {
		if normalized := strings.TrimSpace(slug); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, ErrLegacyPurchaseNotEligible
	}

	order, err := s.orderRepo.GetByOrderNoForInternal(orderNo)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, ErrLegacyPurchaseNotFound
	}
	if !paidOrderEligible(order) {
		return nil, ErrLegacyPurchaseNotEligible
	}

	items := append([]models.OrderItem{}, order.Items...)
	for _, child := range order.Children {
		items = append(items, child.Items...)
	}
	for _, item := range items {
		product, err := s.productRepo.GetByID(strconv.FormatUint(uint64(item.ProductID), 10))
		if err != nil {
			return nil, err
		}
		if product == nil {
			continue
		}
		if _, ok := allowed[strings.TrimSpace(product.Slug)]; ok {
			return &LegacyPurchaseVerification{
				Valid:      true,
				OrderNo:    order.OrderNo,
				SourceSlug: product.Slug,
			}, nil
		}
	}

	return nil, ErrLegacyPurchaseNotEligible
}

func paidOrderEligible(order *models.Order) bool {
	if order == nil || order.PaidAt == nil || order.TotalAmount.Decimal.Sign() <= 0 {
		return false
	}
	if order.RefundedAmount.Decimal.Cmp(order.TotalAmount.Decimal) >= 0 {
		return false
	}

	switch order.Status {
	case constants.OrderStatusPaid,
		constants.OrderStatusFulfilling,
		constants.OrderStatusPartiallyDelivered,
		constants.OrderStatusDelivered,
		constants.OrderStatusCompleted,
		constants.OrderStatusPartiallyRefunded:
		return true
	default:
		return false
	}
}
