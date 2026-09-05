package service

import (
	"strings"
	"testing"

	"github.com/dujiao-next/internal/models"
)

func TestBehaviorAnalyticsPurchaseDiagnosticContract(t *testing.T) {
	service, db := newBehaviorAnalyticsTestService(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for name := range purchaseDiagnosticEvents {
		accepted, err := service.RecordBatch(RecordBehaviorBatchInput{Events: []RecordBehaviorEventInput{{
			EventName: name, SessionID: "diagnostic-session", VisitorID: "diagnostic-visitor",
			PageURL:  "https://synthetic:synthetic@example.test/payment?signature=synthetic#synthetic",
			Referrer: "https://example.test/private?key=synthetic", GuestEmail: "synthetic@example.test",
			CouponCode: "synthetic", ElementText: "synthetic", Reason: "hidden", OrderNo: "ORDER-DIAGNOSTIC",
			Properties: models.JSON{
				"instrumentation_version": "journey_v1", "order_status": "pending_payment", "amount": "68.00",
				"purchase_route_variant": "native", "purchase_route_actual_route": "dujiao_fallback",
				"experiment_variant": "handoff", "product_ids": []int{1, -1, 3}, "coupon_entered": true,
				"password": "synthetic", "raw_error": "synthetic", "redirect_url": "synthetic",
			},
		}}})
		if err != nil || accepted != 1 {
			t.Fatalf("event %s: accepted=%d err=%v", name, accepted, err)
		}
	}
	var rows []models.BehaviorEvent
	if err := db.Where("session_id = ?", "diagnostic-session").Limit(20).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(purchaseDiagnosticEvents) {
		t.Fatalf("row count=%d", len(rows))
	}
	for _, row := range rows {
		if row.GuestEmail != "" || row.CouponCode != "" || row.ElementText != "" {
			t.Fatalf("private input retained for %s", row.EventName)
		}
		if row.PageURL != "https://example.test/payment" || row.Referrer != "https://example.test" {
			t.Fatalf("unsafe URL for %s", row.EventName)
		}
		if _, found := row.Properties["raw_error"]; found {
			t.Fatalf("raw error retained for %s", row.EventName)
		}
		if row.Properties["purchase_route_actual_route"] != "dujiao_fallback" || row.Properties["experiment_variant"] != "handoff" {
			t.Fatalf("attribution lost for %s", row.EventName)
		}
	}
	for _, name := range []string{"payment_paid_success", "order_fulfill_success", "unknown_event"} {
		if normalizeBehaviorEventName(name) != "" {
			t.Fatalf("unexpected client event accepted: %s", name)
		}
	}
}

func TestBehaviorAnalyticsPurchaseDiagnosticSanitizesTypes(t *testing.T) {
	clean := sanitizePurchaseDiagnosticProperties(models.JSON{
		"loading": "true", "amount": "not-a-price", "position": -1,
		"source": "https://invalid/?key=synthetic", "page_view_id": strings.Repeat("x", 121),
		"coupon_applied": true, "channel_id": 42, "quantity": float64(1e20),
	})
	if len(clean) != 3 || clean["coupon_applied"] != true || clean["channel_id"] != float64(42) || clean["quantity"] != float64(100000000) {
		t.Fatalf("unexpected fields after sanitizing")
	}
}
