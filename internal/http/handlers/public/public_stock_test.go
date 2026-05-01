package public

import (
	"testing"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
)

func TestDecorateProductStock_AutoSkipsInactiveSKUs(t *testing.T) {
	h := &Handler{}
	product := &models.Product{
		ID:              1,
		FulfillmentType: constants.FulfillmentTypeAuto,
		SKUs: []models.ProductSKU{
			{
				ID:                 11,
				SKUCode:            models.DefaultSKUCode,
				IsActive:           true,
				AutoStockAvailable: 2,
				AutoStockTotal:     3,
				AutoStockLocked:    1,
				AutoStockSold:      4,
			},
			{
				ID:                 12,
				SKUCode:            "DISABLED",
				IsActive:           false,
				AutoStockAvailable: 100,
				AutoStockTotal:     120,
				AutoStockLocked:    20,
				AutoStockSold:      50,
			},
		},
	}

	item := publicProductView{Product: *product}
	h.decorateProductStock(product, &item)

	if item.AutoStockAvailable != 2 {
		t.Fatalf("expected auto_stock_available=2, got %d", item.AutoStockAvailable)
	}
	if item.AutoStockTotal != 3 {
		t.Fatalf("expected auto_stock_total=3, got %d", item.AutoStockTotal)
	}
	if item.AutoStockLocked != 1 {
		t.Fatalf("expected auto_stock_locked=1, got %d", item.AutoStockLocked)
	}
	if item.AutoStockSold != 4 {
		t.Fatalf("expected auto_stock_sold=4, got %d", item.AutoStockSold)
	}
	if item.IsSoldOut {
		t.Fatalf("expected product not sold out when active sku has stock")
	}
}

func TestProductRespIncludesDisplayStockWithoutChangingRealStock(t *testing.T) {
	displayStock := 1
	product := &models.Product{
		ID:                   2,
		FulfillmentType:      constants.FulfillmentTypeAuto,
		DisplayStockQuantity: &displayStock,
		SKUs: []models.ProductSKU{
			{
				ID:                 21,
				SKUCode:            models.DefaultSKUCode,
				IsActive:           true,
				AutoStockAvailable: 0,
			},
		},
	}

	item := publicProductView{Product: *product}
	(&Handler{}).decorateProductStock(product, &item)
	resp := item.toProductResp()

	if resp.DisplayStockQuantity == nil || *resp.DisplayStockQuantity != 1 {
		t.Fatalf("display_stock_quantity want 1 got %#v", resp.DisplayStockQuantity)
	}
	if resp.StockStatus != constants.ProductStockStatusOutOfStock {
		t.Fatalf("stock_status should still use real stock, got %s", resp.StockStatus)
	}
	if !resp.IsSoldOut {
		t.Fatalf("is_sold_out should still use real stock")
	}
	if resp.AutoStockAvailable != 0 {
		t.Fatalf("auto_stock_available should still use real stock, got %d", resp.AutoStockAvailable)
	}
}
