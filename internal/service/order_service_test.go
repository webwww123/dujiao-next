package service

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func TestMergeCreateOrderItems(t *testing.T) {
	items := []CreateOrderItem{
		{ProductID: 1, SKUID: 10, Quantity: 1, FulfillmentType: "auto"},
		{ProductID: 1, SKUID: 10, Quantity: 2, FulfillmentType: "auto"},
		{ProductID: 1, SKUID: 11, Quantity: 1, FulfillmentType: "auto"},
		{ProductID: 2, SKUID: 20, Quantity: 1, FulfillmentType: ""},
	}
	merged, err := mergeCreateOrderItems(items)
	if err != nil {
		t.Fatalf("mergeCreateOrderItems error: %v", err)
	}
	if len(merged) != 3 {
		t.Fatalf("expected 3 items, got %d", len(merged))
	}
	if merged[0].ProductID != 1 || merged[0].SKUID != 10 || merged[0].Quantity != 3 {
		t.Fatalf("unexpected merged item: %+v", merged[0])
	}
	if merged[0].FulfillmentType != "" {
		t.Fatalf("expected empty fulfillment type, got: %s", merged[0].FulfillmentType)
	}
}

func TestMergeCreateOrderItemsConflict(t *testing.T) {
	items := []CreateOrderItem{
		{ProductID: 1, SKUID: 10, Quantity: 1, FulfillmentType: "auto"},
		{ProductID: 1, SKUID: 11, Quantity: 1, FulfillmentType: "manual"},
	}
	merged, err := mergeCreateOrderItems(items)
	if err != nil {
		t.Fatalf("expected no error for conflicting fulfillment type input, got: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("unexpected merged result: %+v", merged)
	}
}

func TestApplyCouponDiscountToItems(t *testing.T) {
	plans := []childOrderPlan{
		{Item: models.OrderItem{ProductID: 1}, TotalAmount: decimal.NewFromInt(100)},
		{Item: models.OrderItem{ProductID: 2}, TotalAmount: decimal.NewFromInt(50)},
		{Item: models.OrderItem{ProductID: 3}, TotalAmount: decimal.NewFromInt(50)},
	}
	coupon := &models.Coupon{
		ScopeType:   constants.ScopeTypeProduct,
		ScopeRefIDs: "[1,2]",
	}
	if err := applyCouponDiscountToItems(plans, coupon, decimal.NewFromInt(30)); err != nil {
		t.Fatalf("applyCouponDiscountToItems error: %v", err)
	}
	if !plans[0].CouponDiscount.Equal(decimal.NewFromInt(20)) {
		t.Fatalf("expected 20, got %s", plans[0].CouponDiscount.String())
	}
	if !plans[1].CouponDiscount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("expected 10, got %s", plans[1].CouponDiscount.String())
	}
	if !plans[2].CouponDiscount.Equal(decimal.Zero) {
		t.Fatalf("expected 0, got %s", plans[2].CouponDiscount.String())
	}
}

func TestResolveManualFormSubmissionPreferOrderItemKey(t *testing.T) {
	data := map[string]models.JSON{
		"1":    {"legacy": "legacy"},
		"1:10": {"current": "current"},
	}
	got := resolveManualFormSubmission(data, 1, 10)
	if got["current"] != "current" {
		t.Fatalf("expected order item key value, got: %+v", got)
	}
}

func TestResolveManualFormSubmissionFallbackLegacyProductKey(t *testing.T) {
	data := map[string]models.JSON{
		"1": {"legacy": "legacy"},
	}
	got := resolveManualFormSubmission(data, 1, 99)
	if got["legacy"] != "legacy" {
		t.Fatalf("expected legacy product key value, got: %+v", got)
	}
}

func TestCalcParentStatus(t *testing.T) {
	children := []models.Order{
		{Status: constants.OrderStatusDelivered},
		{Status: constants.OrderStatusPaid},
	}
	status := calcParentStatus(children, constants.OrderStatusPaid)
	if status != constants.OrderStatusPartiallyDelivered {
		t.Fatalf("expected partially_delivered, got %s", status)
	}

	children = []models.Order{
		{Status: constants.OrderStatusDelivered},
		{Status: constants.OrderStatusDelivered},
	}
	status = calcParentStatus(children, constants.OrderStatusPaid)
	if status != constants.OrderStatusDelivered {
		t.Fatalf("expected delivered, got %s", status)
	}
}

func TestCanCompleteParentOrder(t *testing.T) {
	order := &models.Order{
		Status: constants.OrderStatusDelivered,
		Children: []models.Order{
			{Status: constants.OrderStatusDelivered},
			{Status: constants.OrderStatusCompleted},
		},
	}
	if !canCompleteParentOrder(order) {
		t.Fatalf("expected delivered parent order to be completable")
	}
}

func TestCanCompleteParentOrderRejectInvalidStatus(t *testing.T) {
	order := &models.Order{
		Status: constants.OrderStatusPartiallyDelivered,
		Children: []models.Order{
			{Status: constants.OrderStatusDelivered},
		},
	}
	if canCompleteParentOrder(order) {
		t.Fatalf("expected partially_delivered parent order to be rejected")
	}
}

func TestCanCompleteParentOrderRejectInvalidChild(t *testing.T) {
	order := &models.Order{
		Status: constants.OrderStatusDelivered,
		Children: []models.Order{
			{Status: constants.OrderStatusPaid},
		},
	}
	if canCompleteParentOrder(order) {
		t.Fatalf("expected parent order with paid child to be rejected")
	}
}

func TestBuildOrderResultAllowsZeroPromotionPrice(t *testing.T) {
	dsn := fmt.Sprintf("file:order_service_promo_zero_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.Category{}, &models.Product{}, &models.ProductSKU{}, &models.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := models.Category{
		Slug:      "test-category",
		NameJSON:  models.JSON{"zh-CN": "测试分类"},
		SortOrder: 0,
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := models.Product{
		CategoryID:      category.ID,
		Slug:            "test-product",
		TitleJSON:       models.JSON{"zh-CN": "测试商品"},
		PriceAmount:     models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	sku := models.ProductSKU{
		ProductID:         product.ID,
		SKUCode:           models.DefaultSKUCode,
		PriceAmount:       models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		IsActive:          true,
		ManualStockTotal:  constants.ManualStockUnlimited,
		ManualStockLocked: 0,
		ManualStockSold:   0,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	promotion := models.Promotion{
		Name:       "test-100-percent",
		ScopeType:  constants.ScopeTypeProduct,
		ScopeRefID: product.ID,
		Type:       constants.PromotionTypePercent,
		Value:      models.NewMoneyFromDecimal(decimal.NewFromInt(100)),
		MinAmount:  models.NewMoneyFromDecimal(decimal.Zero),
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Create(&promotion).Error; err != nil {
		t.Fatalf("create promotion failed: %v", err)
	}

	svc := NewOrderService(OrderServiceOptions{
		ProductRepo:    repository.NewProductRepository(db),
		ProductSKURepo: repository.NewProductSKURepository(db),
		PromotionRepo:  repository.NewPromotionRepository(db),
		ExpireMinutes:  15,
	})

	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{
				ProductID: product.ID,
				SKUID:     sku.ID,
				Quantity:  1,
			},
		},
	})
	if err != nil {
		t.Fatalf("expected zero promotion price to be allowed, got: %v", err)
	}
	if !result.TotalAmount.Equal(decimal.Zero) {
		t.Fatalf("expected total amount 0, got: %s", result.TotalAmount.String())
	}
	if !result.OriginalAmount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("expected original amount 10, got: %s", result.OriginalAmount.String())
	}
}

func TestBuildOrderResultRejectsProductMaxPurchaseQuantityExceeded(t *testing.T) {
	dsn := fmt.Sprintf("file:order_service_purchase_limit_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.Category{}, &models.Product{}, &models.ProductSKU{}, &models.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := models.Category{
		Slug:      "test-category-limit",
		NameJSON:  models.JSON{"zh-CN": "测试分类"},
		SortOrder: 0,
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := models.Product{
		CategoryID:          category.ID,
		Slug:                "test-product-limit",
		TitleJSON:           models.JSON{"zh-CN": "测试商品"},
		PriceAmount:         models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		PurchaseType:        constants.ProductPurchaseMember,
		FulfillmentType:     constants.FulfillmentTypeManual,
		MaxPurchaseQuantity: 2,
		IsActive:            true,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	sku := models.ProductSKU{
		ProductID:         product.ID,
		SKUCode:           models.DefaultSKUCode,
		PriceAmount:       models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		IsActive:          true,
		ManualStockTotal:  constants.ManualStockUnlimited,
		ManualStockLocked: 0,
		ManualStockSold:   0,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	svc := NewOrderService(OrderServiceOptions{
		ProductRepo:    repository.NewProductRepository(db),
		ProductSKURepo: repository.NewProductSKURepository(db),
		PromotionRepo:  repository.NewPromotionRepository(db),
		ExpireMinutes:  15,
	})

	_, err = svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{
				ProductID: product.ID,
				SKUID:     sku.ID,
				Quantity:  3,
			},
		},
	})
	if !errors.Is(err, ErrProductMaxPurchaseExceeded) {
		t.Fatalf("expected product max purchase exceeded, got: %v", err)
	}
}

func TestBuildOrderResultOriginalAmountBeforePromotion(t *testing.T) {
	dsn := fmt.Sprintf("file:order_service_promo_original_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.Category{}, &models.Product{}, &models.ProductSKU{}, &models.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := models.Category{
		Slug:      "test-category-original",
		NameJSON:  models.JSON{"zh-CN": "测试分类"},
		SortOrder: 0,
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := models.Product{
		CategoryID:      category.ID,
		Slug:            "test-product-original",
		TitleJSON:       models.JSON{"zh-CN": "测试商品"},
		PriceAmount:     models.NewMoneyFromDecimal(decimal.RequireFromString("59.90")),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	sku := models.ProductSKU{
		ProductID:         product.ID,
		SKUCode:           models.DefaultSKUCode,
		PriceAmount:       models.NewMoneyFromDecimal(decimal.RequireFromString("59.90")),
		IsActive:          true,
		ManualStockTotal:  constants.ManualStockUnlimited,
		ManualStockLocked: 0,
		ManualStockSold:   0,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	promotion := models.Promotion{
		Name:       "test-20-percent",
		ScopeType:  constants.ScopeTypeProduct,
		ScopeRefID: product.ID,
		Type:       constants.PromotionTypePercent,
		Value:      models.NewMoneyFromDecimal(decimal.NewFromInt(20)),
		MinAmount:  models.NewMoneyFromDecimal(decimal.Zero),
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Create(&promotion).Error; err != nil {
		t.Fatalf("create promotion failed: %v", err)
	}

	svc := NewOrderService(OrderServiceOptions{
		ProductRepo:    repository.NewProductRepository(db),
		ProductSKURepo: repository.NewProductSKURepository(db),
		PromotionRepo:  repository.NewPromotionRepository(db),
		ExpireMinutes:  15,
	})

	result, err := svc.buildOrderResult(orderCreateParams{
		UserID: 1,
		Items: []CreateOrderItem{
			{
				ProductID: product.ID,
				SKUID:     sku.ID,
				Quantity:  2,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildOrderResult failed: %v", err)
	}

	expectedOriginal := decimal.RequireFromString("119.80")
	expectedPromotion := decimal.RequireFromString("23.96")
	expectedTotal := decimal.RequireFromString("95.84")

	if !result.OriginalAmount.Equal(expectedOriginal) {
		t.Fatalf("expected original amount %s, got: %s", expectedOriginal.String(), result.OriginalAmount.String())
	}
	if !result.PromotionDiscountAmount.Equal(expectedPromotion) {
		t.Fatalf("expected promotion discount amount %s, got: %s", expectedPromotion.String(), result.PromotionDiscountAmount.String())
	}
	if !result.DiscountAmount.Equal(decimal.Zero) {
		t.Fatalf("expected coupon discount amount 0, got: %s", result.DiscountAmount.String())
	}
	if !result.TotalAmount.Equal(expectedTotal) {
		t.Fatalf("expected total amount %s, got: %s", expectedTotal.String(), result.TotalAmount.String())
	}
}

func TestBuildOrderResultAllowsZeroTotalAmountAfterCoupon(t *testing.T) {
	dsn := fmt.Sprintf("file:order_service_coupon_zero_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.Category{}, &models.Product{}, &models.ProductSKU{}, &models.Coupon{}, &models.CouponUsage{}, &models.Promotion{}); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	now := time.Now()
	category := models.Category{
		Slug:      "test-category-coupon",
		NameJSON:  models.JSON{"zh-CN": "测试分类"},
		SortOrder: 0,
		CreatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := models.Product{
		CategoryID:      category.ID,
		Slug:            "test-product-coupon",
		TitleJSON:       models.JSON{"zh-CN": "测试商品"},
		PriceAmount:     models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		IsActive:        true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	sku := models.ProductSKU{
		ProductID:         product.ID,
		SKUCode:           models.DefaultSKUCode,
		PriceAmount:       models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		IsActive:          true,
		ManualStockTotal:  constants.ManualStockUnlimited,
		ManualStockLocked: 0,
		ManualStockSold:   0,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := db.Create(&sku).Error; err != nil {
		t.Fatalf("create sku failed: %v", err)
	}

	coupon := models.Coupon{
		Code:        "FREE10",
		Type:        constants.CouponTypeFixed,
		Value:       models.NewMoneyFromDecimal(decimal.NewFromInt(10)),
		MinAmount:   models.NewMoneyFromDecimal(decimal.Zero),
		MaxDiscount: models.NewMoneyFromDecimal(decimal.Zero),
		ScopeType:   constants.ScopeTypeProduct,
		ScopeRefIDs: fmt.Sprintf("[%d]", product.ID),
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(&coupon).Error; err != nil {
		t.Fatalf("create coupon failed: %v", err)
	}

	svc := NewOrderService(OrderServiceOptions{
		ProductRepo:     repository.NewProductRepository(db),
		ProductSKURepo:  repository.NewProductSKURepository(db),
		CouponRepo:      repository.NewCouponRepository(db),
		CouponUsageRepo: repository.NewCouponUsageRepository(db),
		PromotionRepo:   repository.NewPromotionRepository(db),
		ExpireMinutes:   15,
	})

	result, err := svc.buildOrderResult(orderCreateParams{
		UserID:     1,
		CouponCode: "FREE10",
		Items: []CreateOrderItem{
			{
				ProductID: product.ID,
				SKUID:     sku.ID,
				Quantity:  1,
			},
		},
	})
	if err != nil {
		t.Fatalf("expected zero coupon total to be allowed, got: %v", err)
	}
	if !result.TotalAmount.Equal(decimal.Zero) {
		t.Fatalf("expected total amount 0, got: %s", result.TotalAmount.String())
	}
	if !result.DiscountAmount.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("expected discount amount 10, got: %s", result.DiscountAmount.String())
	}
}
