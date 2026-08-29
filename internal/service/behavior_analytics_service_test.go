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

func newBehaviorAnalyticsTestService(t *testing.T) (*BehaviorAnalyticsService, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:behavior_analytics_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(
		&models.BehaviorEvent{},
		&models.Coupon{},
		&models.Order{},
		&models.Product{},
		&models.User{},
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	service := NewBehaviorAnalyticsService(
		repository.NewBehaviorEventRepository(db),
		repository.NewOrderRepository(db),
		repository.NewProductRepository(db),
		repository.NewCouponRepository(db),
		repository.NewUserRepository(db),
	)
	return service, db
}

func TestBehaviorAnalyticsTracksCouponAbandonmentAndPaidSession(t *testing.T) {
	service, db := newBehaviorAnalyticsTestService(t)
	coupon := models.Coupon{
		Code:        "SAVE20",
		Type:        "fixed",
		Value:       models.NewMoneyFromDecimal(decimal.NewFromInt(20)),
		ScopeType:   "product",
		ScopeRefIDs: "[1]",
		IsActive:    true,
	}
	if err := db.Create(&coupon).Error; err != nil {
		t.Fatalf("create coupon: %v", err)
	}

	now := time.Now().UTC()
	abandonedAt := now.Add(-45 * time.Minute)
	paidAt := now.Add(-40 * time.Minute)

	if err := db.Create(&models.Order{
		OrderNo:  "ORDER-ABANDONED",
		Status:   constants.OrderStatusPendingPayment,
		Currency: "CNY",
	}).Error; err != nil {
		t.Fatalf("create abandoned order: %v", err)
	}
	if err := db.Create(&models.Order{
		OrderNo:  "ORDER-PAID",
		Status:   constants.OrderStatusPaid,
		Currency: "CNY",
	}).Error; err != nil {
		t.Fatalf("create paid order: %v", err)
	}

	accepted, err := service.RecordBatch(RecordBehaviorBatchInput{
		ClientIP:  "203.0.113.10",
		UserAgent: "Behavior Test Browser",
		Events: []RecordBehaviorEventInput{
			{EventID: "a-1", VisitorID: "visitor-a", SessionID: "session-a", EventName: "product_view", ProductID: 1, OccurredAt: &abandonedAt},
			{EventID: "a-2", VisitorID: "visitor-a", SessionID: "session-a", EventName: "checkout_view", ProductID: 1, Properties: models.JSON{"product_ids": []uint{1}}, OccurredAt: &abandonedAt},
			{EventID: "a-3", VisitorID: "visitor-a", SessionID: "session-a", EventName: "coupon_applied", CouponCode: coupon.Code, ProductID: 1, OccurredAt: &abandonedAt},
			{EventID: "a-4", VisitorID: "visitor-a", SessionID: "session-a", EventName: "order_created", CouponCode: coupon.Code, OrderNo: "ORDER-ABANDONED", Properties: models.JSON{"product_ids": []uint{1}}, OccurredAt: &abandonedAt},
			{EventID: "b-1", VisitorID: "visitor-b", SessionID: "session-b", EventName: "product_view", ProductID: 1, OccurredAt: &paidAt},
			{EventID: "b-2", VisitorID: "visitor-b", SessionID: "session-b", EventName: "checkout_view", ProductID: 1, Properties: models.JSON{"product_ids": []uint{1}}, OccurredAt: &paidAt},
			{EventID: "b-3", VisitorID: "visitor-b", SessionID: "session-b", EventName: "order_created", OrderNo: "ORDER-PAID", Properties: models.JSON{"product_ids": []uint{1}}, OccurredAt: &paidAt},
			{EventID: "b-4", VisitorID: "visitor-b", SessionID: "session-b", EventName: "payment_started", OrderNo: "ORDER-PAID", PaymentID: 9, OccurredAt: &paidAt},
			{EventID: "b-5", VisitorID: "visitor-b", SessionID: "session-b", EventName: "payment_success", OrderNo: "ORDER-PAID", PaymentID: 9, OccurredAt: &paidAt},
		},
	})
	if err != nil {
		t.Fatalf("record behavior events: %v", err)
	}
	if accepted != 9 {
		t.Fatalf("accepted events = %d, want 9", accepted)
	}

	overview, err := service.GetOverview(DashboardQueryInput{Range: "7d", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("get overview: %v", err)
	}
	if overview.KPI.Sessions != 2 || overview.KPI.PaidSessions != 1 || overview.KPI.CheckoutSessions != 2 {
		t.Fatalf("unexpected KPI: %#v", overview.KPI)
	}
	if overview.Coupon.AppliedSessions != 1 || overview.Coupon.AbandonedSessions != 1 || overview.Coupon.PaidSessions != 0 {
		t.Fatalf("unexpected coupon overview: %#v", overview.Coupon)
	}
	if len(overview.Coupon.Coupons) != 1 || overview.Coupon.Coupons[0].CouponCode != coupon.Code {
		t.Fatalf("unexpected coupon rows: %#v", overview.Coupon.Coupons)
	}

	rows, total, err := service.ListSessions(DashboardQueryInput{Range: "7d", Timezone: "UTC"}, 1, 20)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("session pagination total=%d rows=%d", total, len(rows))
	}
	var abandoned *BehaviorSessionSummary
	for i := range rows {
		if rows[i].SessionID == "session-a" {
			abandoned = &rows[i]
		}
	}
	if abandoned == nil {
		t.Fatal("abandoned session missing")
	}
	if abandoned.DropoffReason != "order_created_no_payment" || abandoned.IsPaid {
		t.Fatalf("unexpected abandoned classification: %#v", abandoned)
	}
}

func TestBehaviorAnalyticsKeepsDiagnosticDataAndDropsSecrets(t *testing.T) {
	service, _ := newBehaviorAnalyticsTestService(t)
	occurredAt := time.Now().UTC().Add(-time.Minute)
	_, err := service.RecordBatch(RecordBehaviorBatchInput{
		ClientIP:  "203.0.113.20",
		UserAgent: "Detailed Browser/1.0",
		Events: []RecordBehaviorEventInput{{
			EventID:         "detail-1",
			VisitorID:       "visitor-detail",
			SessionID:       "session-detail",
			GuestEmail:      "guest@example.test",
			EventName:       "ui_click",
			PageURL:         "https://shop.example.test/pay?order_no=ORDER-1&token=fake-token&session_id=fake-session",
			ElementText:     "立即支付",
			ElementSelector: "main > button.pay",
			CouponCode:      "SAVE20",
			Reason:          "gateway returned a diagnostic error",
			Properties: models.JSON{
				"viewport_width":            1280,
				"raw_error":                 "gateway timeout",
				"experiment_id":             "coupon_handoff_v1",
				"experiment_variant":        "handoff",
				"handoff_flow_id":           "flow-1",
				"handoff_source_order_no":   "SOURCE-1",
				"handoff_target_product_id": 3,
				"handoff_target":            true,
				"password":                  "must-not-be-stored",
				"access_token":              "must-not-be-stored",
			},
			OccurredAt: &occurredAt,
		}},
	})
	if err != nil {
		t.Fatalf("record detail event: %v", err)
	}

	detail, err := service.GetSession("session-detail")
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if detail.Summary.GuestEmail != "guest@example.test" || detail.Summary.ClientIP != "203.0.113.20" {
		t.Fatalf("diagnostic identity missing: %#v", detail.Summary)
	}
	if len(detail.Events) != 1 {
		t.Fatalf("events length = %d", len(detail.Events))
	}
	event := detail.Events[0]
	if event.ElementText != "立即支付" || event.ElementSelector == "" || event.Reason == "" {
		t.Fatalf("diagnostic event fields missing: %#v", event)
	}
	if event.PageURL == "" || event.PageURL == "https://shop.example.test/pay?order_no=ORDER-1&token=fake-token&session_id=fake-session" {
		t.Fatalf("sensitive query values were not removed: %q", event.PageURL)
	}
	if _, exists := event.Properties["password"]; exists {
		t.Fatal("password property should not be stored")
	}
	if _, exists := event.Properties["access_token"]; exists {
		t.Fatal("token property should not be stored")
	}
	if event.Properties["raw_error"] != "gateway timeout" {
		t.Fatalf("raw diagnostic error missing: %#v", event.Properties)
	}
	if event.Properties["experiment_variant"] != "handoff" ||
		event.Properties["handoff_flow_id"] != "flow-1" ||
		event.Properties["handoff_target_product_id"] != float64(3) ||
		event.Properties["handoff_target"] != true {
		t.Fatalf("handoff attribution properties missing: %#v", event.Properties)
	}
}
