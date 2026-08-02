package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestClaimPaidGuestOrdersClaimsParentAndChildrenOnly(t *testing.T) {
	dsn := fmt.Sprintf("file:order-guest-claim-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Order{}); err != nil {
		t.Fatalf("migrate orders: %v", err)
	}

	paidAt := time.Now()
	parent := models.Order{
		OrderNo:       "GUEST-CLAIM-PAID",
		GuestEmail:    "buyer@example.com",
		GuestPassword: "claim-pass",
		Status:        constants.OrderStatusCompleted,
		Currency:      "USD",
		PaidAt:        &paidAt,
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create paid parent: %v", err)
	}
	child := models.Order{
		OrderNo:       "GUEST-CLAIM-PAID-CHILD",
		ParentID:      &parent.ID,
		GuestEmail:    parent.GuestEmail,
		GuestPassword: parent.GuestPassword,
		Status:        constants.OrderStatusCompleted,
		Currency:      "USD",
		PaidAt:        &paidAt,
	}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create paid child: %v", err)
	}

	pending := models.Order{
		OrderNo:       "GUEST-CLAIM-PENDING",
		GuestEmail:    parent.GuestEmail,
		GuestPassword: parent.GuestPassword,
		Status:        constants.OrderStatusPendingPayment,
		Currency:      "USD",
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatalf("create pending parent: %v", err)
	}
	otherPassword := models.Order{
		OrderNo:       "GUEST-CLAIM-OTHER-PASSWORD",
		GuestEmail:    parent.GuestEmail,
		GuestPassword: "different-pass",
		Status:        constants.OrderStatusCompleted,
		Currency:      "USD",
		PaidAt:        &paidAt,
	}
	if err := db.Create(&otherPassword).Error; err != nil {
		t.Fatalf("create other-password parent: %v", err)
	}

	repo := NewOrderRepository(db)
	claimed, err := repo.ClaimPaidGuestOrders(42, parent.GuestEmail, parent.GuestPassword)
	if err != nil {
		t.Fatalf("claim guest orders: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("claimed parent count want 1 got %d", claimed)
	}

	for _, tc := range []struct {
		id   uint
		name string
	}{
		{id: parent.ID, name: "parent"},
		{id: child.ID, name: "child"},
	} {
		var got models.Order
		if err := db.First(&got, tc.id).Error; err != nil {
			t.Fatalf("load claimed %s: %v", tc.name, err)
		}
		if got.UserID != 42 {
			t.Fatalf("%s user_id want 42 got %d", tc.name, got.UserID)
		}
		if got.GuestPassword != "" {
			t.Fatalf("%s guest password should be cleared", tc.name)
		}
		if got.GuestEmail != parent.GuestEmail {
			t.Fatalf("%s guest email should be preserved", tc.name)
		}
	}

	for _, tc := range []struct {
		id       uint
		name     string
		password string
	}{
		{id: pending.ID, name: "pending", password: parent.GuestPassword},
		{id: otherPassword.ID, name: "other password", password: otherPassword.GuestPassword},
	} {
		var got models.Order
		if err := db.First(&got, tc.id).Error; err != nil {
			t.Fatalf("load untouched %s: %v", tc.name, err)
		}
		if got.UserID != 0 || got.GuestPassword != tc.password {
			t.Fatalf("%s should remain a guest order", tc.name)
		}
	}

	claimedAgain, err := repo.ClaimPaidGuestOrders(42, parent.GuestEmail, parent.GuestPassword)
	if err != nil {
		t.Fatalf("claim guest orders again: %v", err)
	}
	if claimedAgain != 0 {
		t.Fatalf("idempotent claim want 0 got %d", claimedAgain)
	}
}
