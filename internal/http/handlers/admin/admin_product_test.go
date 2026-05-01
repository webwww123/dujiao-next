package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/provider"
	"github.com/dujiao-next/internal/repository"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupAdminProductHandlerTest(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:admin_product_handler_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Category{},
		&models.Product{},
		&models.ProductSKU{},
		&models.CardSecret{},
		&models.CardSecretBatch{},
		&models.MemberLevelPrice{},
		&models.CartItem{},
		&models.ProductMapping{},
		&models.SKUMapping{},
		&models.Order{},
		&models.OrderItem{},
	); err != nil {
		t.Fatalf("auto migrate failed: %v", err)
	}

	productService := service.NewProductService(
		repository.NewProductRepository(db),
		repository.NewProductSKURepository(db),
		repository.NewCardSecretRepository(db),
		repository.NewCardSecretBatchRepository(db),
		repository.NewCategoryRepository(db),
		repository.NewMemberLevelPriceRepository(db),
		repository.NewCartRepository(db),
		repository.NewProductMappingRepository(db),
		repository.NewOrderRepository(db),
	)

	h := &Handler{Container: &provider.Container{
		ProductService: productService,
	}}
	return h, db
}

func TestCreateProductAllowsZeroPrice(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	category := models.Category{
		Slug:     "free-product-category",
		NameJSON: models.JSON{"zh-CN": "free-product-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	body := map[string]any{
		"category_id":        category.ID,
		"slug":               "free-product-handler",
		"title":              map[string]any{"zh-CN": "free-product-handler"},
		"price_amount":       0,
		"cost_price_amount":  0,
		"purchase_type":      constants.ProductPurchaseMember,
		"fulfillment_type":   constants.FulfillmentTypeManual,
		"manual_stock_total": -1,
		"is_active":          true,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/products", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateProduct(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status code want %d got %d body=%s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp struct {
		StatusCode int `json:"status_code"`
		Data       struct {
			ID          uint   `json:"id"`
			PriceAmount string `json:"price_amount"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if resp.StatusCode != 0 {
		t.Fatalf("status_code want 0 got %d msg=%s body=%s", resp.StatusCode, resp.Msg, w.Body.String())
	}
	if resp.Data.ID == 0 {
		t.Fatalf("expected created product id, body=%s", w.Body.String())
	}
	if resp.Data.PriceAmount != "0" && resp.Data.PriceAmount != "0.00" {
		t.Fatalf("price_amount want zero got %s body=%s", resp.Data.PriceAmount, w.Body.String())
	}
}

func TestCreateProductAllowsZeroSKUPrice(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	category := models.Category{
		Slug:     "free-sku-product-category",
		NameJSON: models.JSON{"zh-CN": "free-sku-product-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	body := map[string]any{
		"category_id":       category.ID,
		"slug":              "free-sku-product-handler",
		"title":             map[string]any{"zh-CN": "free-sku-product-handler"},
		"price_amount":      0,
		"cost_price_amount": 0,
		"purchase_type":     constants.ProductPurchaseMember,
		"fulfillment_type":  constants.FulfillmentTypeManual,
		"is_active":         true,
		"skus": []map[string]any{
			{
				"sku_code":           "FREE",
				"price_amount":       0,
				"cost_price_amount":  0,
				"manual_stock_total": -1,
				"is_active":          true,
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/products", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateProduct(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status code want %d got %d body=%s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp struct {
		StatusCode int    `json:"status_code"`
		Msg        string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if resp.StatusCode != 0 {
		t.Fatalf("status_code want 0 got %d msg=%s body=%s", resp.StatusCode, resp.Msg, w.Body.String())
	}
}

func TestCreateProductPersistsDisplayStockQuantity(t *testing.T) {
	h, db := setupAdminProductHandlerTest(t)

	category := models.Category{
		Slug:     "display-stock-category",
		NameJSON: models.JSON{"zh-CN": "display-stock-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	body := map[string]any{
		"category_id":            category.ID,
		"slug":                   "display-stock-product",
		"title":                  map[string]any{"zh-CN": "display-stock-product"},
		"price_amount":           1,
		"cost_price_amount":      0,
		"purchase_type":          constants.ProductPurchaseMember,
		"fulfillment_type":       constants.FulfillmentTypeAuto,
		"display_stock_quantity": 1,
		"is_active":              true,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body failed: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/products", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateProduct(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status code want %d got %d body=%s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp struct {
		StatusCode int `json:"status_code"`
		Data       struct {
			ID                   uint `json:"id"`
			DisplayStockQuantity *int `json:"display_stock_quantity"`
		} `json:"data"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response failed: %v body=%s", err, w.Body.String())
	}
	if resp.StatusCode != 0 {
		t.Fatalf("status_code want 0 got %d msg=%s body=%s", resp.StatusCode, resp.Msg, w.Body.String())
	}
	if resp.Data.DisplayStockQuantity == nil || *resp.Data.DisplayStockQuantity != 1 {
		t.Fatalf("display_stock_quantity want 1 got %#v body=%s", resp.Data.DisplayStockQuantity, w.Body.String())
	}

	var saved models.Product
	if err := db.First(&saved, resp.Data.ID).Error; err != nil {
		t.Fatalf("fetch product failed: %v", err)
	}
	if saved.DisplayStockQuantity == nil || *saved.DisplayStockQuantity != 1 {
		t.Fatalf("saved display_stock_quantity want 1 got %#v", saved.DisplayStockQuantity)
	}
}
