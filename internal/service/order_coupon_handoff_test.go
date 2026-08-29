package service

import (
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

func setupCouponHandoffTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:coupon_handoff_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Order{},
		&models.OrderItem{},
		&models.Fulfillment{},
		&models.CardSecret{},
		&models.Coupon{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db
}

func newCouponHandoffService(db *gorm.DB) *OrderService {
	return NewOrderService(OrderServiceOptions{
		CardSecretRepo: repository.NewCardSecretRepository(db),
		CouponRepo:     repository.NewCouponRepository(db),
	})
}

func createHandoffCoupon(t *testing.T, db *gorm.DB, id uint, code string) {
	t.Helper()
	value := models.NewMoneyFromDecimal(decimal.NewFromInt(30))
	coupon := &models.Coupon{
		ID:          id,
		Code:        code,
		Type:        constants.CouponTypeFixed,
		Value:       value,
		ScopeType:   constants.ScopeTypeProduct,
		ScopeRefIDs: "[3,4,16,22,25,27]",
		IsActive:    true,
	}
	if err := db.Create(coupon).Error; err != nil {
		t.Fatalf("create coupon failed: %v", err)
	}
}

func newHandoffOrder(sourceProductID uint, status, fulfillmentStatus string, includeFulfillment bool) *models.Order {
	now := time.Now()
	child := models.Order{
		ID:        2,
		OrderNo:   "HANDOFF-001-01",
		Status:    status,
		Currency:  "CNY",
		CreatedAt: now,
		UpdatedAt: now,
		Items: []models.OrderItem{{
			ID:              3,
			OrderID:         2,
			ProductID:       sourceProductID,
			Quantity:        1,
			FulfillmentType: constants.FulfillmentTypeAuto,
			UnitPrice:       models.NewMoneyFromDecimal(decimal.Zero),
			TotalPrice:      models.NewMoneyFromDecimal(decimal.Zero),
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
	}
	if includeFulfillment {
		child.Fulfillment = &models.Fulfillment{
			OrderID:     2,
			Status:      fulfillmentStatus,
			Type:        constants.FulfillmentTypeAuto,
			Payload:     "优惠码：CODE-1234",
			DeliveredAt: &now,
		}
	}
	return &models.Order{
		ID:        1,
		OrderNo:   "HANDOFF-001",
		Status:    status,
		Currency:  "CNY",
		CreatedAt: now,
		UpdatedAt: now,
		Children:  []models.Order{child},
	}
}

func TestResolveCouponHandoffReady(t *testing.T) {
	db := setupCouponHandoffTestDB(t)
	createHandoffCoupon(t, db, 2, "CODE-1234")
	if err := db.Create(&models.CardSecret{
		ID:        10,
		ProductID: 12,
		SKUID:     13,
		Secret:    "优惠码：CODE-1234\n有效期：4小时",
		Status:    models.CardSecretStatusUsed,
		OrderID:   uintPtr(2),
	}).Error; err != nil {
		t.Fatalf("create card secret failed: %v", err)
	}

	result, err := newCouponHandoffService(db).ResolveCouponHandoff(newHandoffOrder(12, constants.OrderStatusCompleted, constants.FulfillmentStatusDelivered, true))
	if err != nil {
		t.Fatalf("resolve handoff failed: %v", err)
	}
	if result.State != CouponHandoffStateReady {
		t.Fatalf("state want ready got %q", result.State)
	}
	if result.CouponCode != "CODE-1234" || result.CouponID != 2 {
		t.Fatalf("unexpected coupon result: %+v", result)
	}
	if result.TargetProductID != 3 || result.TargetSlug != "wuxianliangyueka" {
		t.Fatalf("unexpected target: %+v", result)
	}
}

func TestResolveCouponHandoffPendingUntilDelivered(t *testing.T) {
	db := setupCouponHandoffTestDB(t)
	createHandoffCoupon(t, db, 6, "CODE-1234")
	result, err := newCouponHandoffService(db).ResolveCouponHandoff(newHandoffOrder(14, constants.OrderStatusPaid, constants.FulfillmentStatusPending, false))
	if err != nil {
		t.Fatalf("resolve handoff failed: %v", err)
	}
	if result.State != CouponHandoffStatePending {
		t.Fatalf("state want pending got %q", result.State)
	}
	if result.CouponCode != "" {
		t.Fatalf("pending result must not expose coupon code: %+v", result)
	}
}

func TestResolveCouponHandoffFallsBackWhenSecretAmbiguous(t *testing.T) {
	db := setupCouponHandoffTestDB(t)
	createHandoffCoupon(t, db, 7, "CODE-1234")
	orderID := uint(2)
	for id, secret := range map[uint]string{
		10: "优惠码：CODE-1234",
		11: "优惠码：CODE-1234",
	} {
		if err := db.Create(&models.CardSecret{
			ID:        id,
			ProductID: 17,
			Secret:    secret,
			Status:    models.CardSecretStatusUsed,
			OrderID:   &orderID,
		}).Error; err != nil {
			t.Fatalf("create card secret failed: %v", err)
		}
	}

	result, err := newCouponHandoffService(db).ResolveCouponHandoff(newHandoffOrder(17, constants.OrderStatusCompleted, constants.FulfillmentStatusDelivered, true))
	if err != nil {
		t.Fatalf("resolve handoff failed: %v", err)
	}
	if result.State != CouponHandoffStateUnavailable || result.CouponCode != "" {
		t.Fatalf("ambiguous result should be unavailable without code: %+v", result)
	}
}

func TestResolveCouponHandoffUnsupportedAndTerminal(t *testing.T) {
	db := setupCouponHandoffTestDB(t)
	svc := newCouponHandoffService(db)

	unsupported, err := svc.ResolveCouponHandoff(newHandoffOrder(3, constants.OrderStatusCompleted, constants.FulfillmentStatusDelivered, true))
	if err != nil {
		t.Fatalf("resolve unsupported failed: %v", err)
	}
	if unsupported.State != CouponHandoffStateUnsupported {
		t.Fatalf("unsupported state got %q", unsupported.State)
	}

	createHandoffCoupon(t, db, 2, "CODE-1234")
	terminal, err := svc.ResolveCouponHandoff(newHandoffOrder(12, constants.OrderStatusCanceled, constants.FulfillmentStatusDelivered, true))
	if err != nil {
		t.Fatalf("resolve terminal failed: %v", err)
	}
	if terminal.State != CouponHandoffStateUnavailable || terminal.CouponCode != "" {
		t.Fatalf("terminal result should be unavailable without code: %+v", terminal)
	}
}

func TestResolveCouponHandoffFallsBackWhenCouponScopeChanges(t *testing.T) {
	db := setupCouponHandoffTestDB(t)
	createHandoffCoupon(t, db, 2, "CODE-1234")
	if err := db.Model(&models.Coupon{}).Where("id = ?", 2).Update("scope_ref_ids", "[16]").Error; err != nil {
		t.Fatalf("update coupon scope failed: %v", err)
	}
	orderID := uint(2)
	if err := db.Create(&models.CardSecret{
		ID:        10,
		ProductID: 12,
		Secret:    "优惠码：CODE-1234",
		Status:    models.CardSecretStatusUsed,
		OrderID:   &orderID,
	}).Error; err != nil {
		t.Fatalf("create card secret failed: %v", err)
	}

	result, err := newCouponHandoffService(db).ResolveCouponHandoff(newHandoffOrder(12, constants.OrderStatusCompleted, constants.FulfillmentStatusDelivered, true))
	if err != nil {
		t.Fatalf("resolve handoff failed: %v", err)
	}
	if result.State != CouponHandoffStateUnavailable || result.CouponCode != "" {
		t.Fatalf("scope mismatch should be unavailable without code: %+v", result)
	}
}

func uintPtr(value uint) *uint {
	return &value
}
