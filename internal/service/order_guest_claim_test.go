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
	"gorm.io/gorm"
)

func TestClaimPaidGuestOrdersUsesCurrentUserEmail(t *testing.T) {
	dsn := fmt.Sprintf("file:order-guest-claim-service-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Order{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	user := models.User{
		Email:        "buyer@example.com",
		PasswordHash: "unused",
		Status:       constants.UserStatusActive,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	paidAt := time.Now()
	order := models.Order{
		OrderNo:       "GUEST-CLAIM-SERVICE",
		GuestEmail:    user.Email,
		GuestPassword: "service-pass",
		Status:        constants.OrderStatusCompleted,
		Currency:      "USD",
		PaidAt:        &paidAt,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create guest order: %v", err)
	}
	otherUser := models.User{
		Email:        "other@example.com",
		PasswordHash: "unused",
		Status:       constants.UserStatusActive,
	}
	if err := db.Create(&otherUser).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}

	orderRepo := repository.NewOrderRepository(db)
	userRepo := repository.NewUserRepository(db)
	svc := NewOrderService(OrderServiceOptions{
		OrderRepo: orderRepo,
		UserRepo:  userRepo,
	})
	wrongAccountClaimed, err := svc.ClaimPaidGuestOrders(otherUser.ID, "service-pass")
	if err != nil {
		t.Fatalf("claim with different account email: %v", err)
	}
	if wrongAccountClaimed != 0 {
		t.Fatalf("different account email should claim 0 orders, got %d", wrongAccountClaimed)
	}

	claimed, err := svc.ClaimPaidGuestOrders(user.ID, "  service-pass  ")
	if err != nil {
		t.Fatalf("claim guest orders: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("claimed count want 1 got %d", claimed)
	}

	var got models.Order
	if err := db.First(&got, order.ID).Error; err != nil {
		t.Fatalf("load claimed order: %v", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("claimed order user_id want %d got %d", user.ID, got.UserID)
	}

	if _, err := svc.ClaimPaidGuestOrders(user.ID, "   "); !errors.Is(err, ErrGuestPasswordRequired) {
		t.Fatalf("empty password error want ErrGuestPasswordRequired got %v", err)
	}
}
