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

func setupLegacyPurchaseServiceTestDB(t *testing.T) (*gorm.DB, repository.OrderRepository, repository.ProductRepository) {
	t.Helper()
	dsn := fmt.Sprintf("file:legacy_purchase_service_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Category{},
		&models.Product{},
		&models.ProductSKU{},
		&models.Order{},
		&models.OrderItem{},
		&models.Fulfillment{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}
	return db, repository.NewOrderRepository(db), repository.NewProductRepository(db)
}

func createLegacyPurchaseFixture(t *testing.T, db *gorm.DB, status string, total, refunded string, slug string) *models.Order {
	t.Helper()
	product := &models.Product{
		CategoryID:      1,
		Slug:            slug,
		TitleJSON:       models.JSON{"zh-CN": "历史商品"},
		PriceAmount:     models.NewMoneyFromDecimal(decimal.RequireFromString(total)),
		IsActive:        true,
		FulfillmentType: constants.FulfillmentTypeAuto,
	}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	paidAt := time.Now().UTC()
	order := &models.Order{
		OrderNo:        "DJ-LEGACY-001",
		Status:         status,
		Currency:       "CNY",
		TotalAmount:    models.NewMoneyFromDecimal(decimal.RequireFromString(total)),
		RefundedAmount: models.NewMoneyFromDecimal(decimal.RequireFromString(refunded)),
		PaidAt:         &paidAt,
		CreatedAt:      paidAt,
		UpdatedAt:      paidAt,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("create order failed: %v", err)
	}
	item := &models.OrderItem{
		OrderID:         order.ID,
		ProductID:       product.ID,
		TitleJSON:       models.JSON{"zh-CN": "历史商品"},
		UnitPrice:       models.NewMoneyFromDecimal(decimal.RequireFromString(total)),
		TotalPrice:      models.NewMoneyFromDecimal(decimal.RequireFromString(total)),
		Quantity:        1,
		FulfillmentType: constants.FulfillmentTypeAuto,
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("create order item failed: %v", err)
	}
	return order
}

func TestLegacyPurchaseServiceVerifiesPaidSupportedOrder(t *testing.T) {
	db, orderRepo, productRepo := setupLegacyPurchaseServiceTestDB(t)
	createLegacyPurchaseFixture(t, db, constants.OrderStatusCompleted, "68.00", "0.00", "gqbxl")

	service := NewLegacyPurchaseService(orderRepo, productRepo)
	result, err := service.VerifyOrder("DJ-LEGACY-001", []string{"gqbxl", "k26"})
	if err != nil {
		t.Fatalf("verify order failed: %v", err)
	}
	if result == nil || !result.Valid || result.SourceSlug != "gqbxl" {
		t.Fatalf("unexpected verification result: %#v", result)
	}
}

func TestLegacyPurchaseServiceRejectsZeroRefundedCanceledAndUnsupportedOrders(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		total    string
		refunded string
		slug     string
	}{
		{name: "zero amount", status: constants.OrderStatusCompleted, total: "0.00", refunded: "0.00", slug: "gqbxl"},
		{name: "fully refunded", status: constants.OrderStatusRefunded, total: "68.00", refunded: "68.00", slug: "gqbxl"},
		{name: "canceled", status: constants.OrderStatusCanceled, total: "68.00", refunded: "0.00", slug: "gqbxl"},
		{name: "unsupported product", status: constants.OrderStatusCompleted, total: "68.00", refunded: "0.00", slug: "other-product"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, orderRepo, productRepo := setupLegacyPurchaseServiceTestDB(t)
			createLegacyPurchaseFixture(t, db, tc.status, tc.total, tc.refunded, tc.slug)
			service := NewLegacyPurchaseService(orderRepo, productRepo)
			_, err := service.VerifyOrder("DJ-LEGACY-001", []string{"gqbxl"})
			if err != ErrLegacyPurchaseNotEligible {
				t.Fatalf("expected ErrLegacyPurchaseNotEligible, got %v", err)
			}
		})
	}
}

func TestLegacyPurchaseServiceRequiresPaidAt(t *testing.T) {
	db, orderRepo, productRepo := setupLegacyPurchaseServiceTestDB(t)
	order := createLegacyPurchaseFixture(t, db, constants.OrderStatusCompleted, "68.00", "0.00", "gqbxl")
	if err := db.Model(&models.Order{}).Where("id = ?", order.ID).Update("paid_at", nil).Error; err != nil {
		t.Fatalf("clear paid_at failed: %v", err)
	}

	service := NewLegacyPurchaseService(orderRepo, productRepo)
	_, err := service.VerifyOrder("DJ-LEGACY-001", []string{"gqbxl"})
	if err != ErrLegacyPurchaseNotEligible {
		t.Fatalf("expected unpaid order rejection, got %v", err)
	}
}
