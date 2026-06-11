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

func setupApiCredentialServiceTest(t *testing.T) (*ApiCredentialService, repository.ApiCredentialRepository, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := db.AutoMigrate(&models.ApiCredential{}); err != nil {
		t.Fatalf("auto migrate api credential failed: %v", err)
	}

	repo := repository.NewApiCredentialRepository(db)
	return NewApiCredentialService(repo), repo, db
}

func TestApiCredentialServiceApplyCreatesApprovedCredentialWhenMissing(t *testing.T) {
	svc, repo, _ := setupApiCredentialServiceTest(t)

	cred, err := svc.Apply(1001)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Status != constants.ApiCredentialStatusApproved {
		t.Fatalf("expected status %s, got %s", constants.ApiCredentialStatusApproved, cred.Status)
	}
	if cred.UserID != 1001 {
		t.Fatalf("expected user id 1001, got %d", cred.UserID)
	}
	if cred.ApiKey == "" {
		t.Fatal("expected api key to be generated")
	}
	if cred.ApiSecret == "" {
		t.Fatal("expected api secret to be generated")
	}
	if cred.ApprovedAt == nil {
		t.Fatal("expected approved_at to be set")
	}
	if !cred.IsActive {
		t.Fatal("expected credential to be active")
	}

	stored, err := repo.GetByUserID(1001)
	if err != nil {
		t.Fatalf("get by user id failed: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored credential, got nil")
	}
	if stored.Status != constants.ApiCredentialStatusApproved {
		t.Fatalf("expected stored status %s, got %s", constants.ApiCredentialStatusApproved, stored.Status)
	}
}

func TestApiCredentialServiceApplyRestoresDeletedCredential(t *testing.T) {
	svc, repo, _ := setupApiCredentialServiceTest(t)
	now := time.Now()

	cred := &models.ApiCredential{
		UserID:       1002,
		ApiKey:       "legacy-key",
		ApiSecret:    "legacy-secret",
		Status:       constants.ApiCredentialStatusApproved,
		RejectReason: "legacy reason",
		ApprovedAt:   &now,
		LastUsedAt:   &now,
		IsActive:     true,
	}
	if err := repo.Create(cred); err != nil {
		t.Fatalf("create credential failed: %v", err)
	}
	if err := repo.Delete(cred.ID); err != nil {
		t.Fatalf("delete credential failed: %v", err)
	}

	reapplied, err := svc.Apply(1002)
	if err != nil {
		t.Fatalf("reapply failed: %v", err)
	}
	if reapplied.ID != cred.ID {
		t.Fatalf("expected to reuse credential id %d, got %d", cred.ID, reapplied.ID)
	}
	if reapplied.Status != constants.ApiCredentialStatusApproved {
		t.Fatalf("expected status %s, got %s", constants.ApiCredentialStatusApproved, reapplied.Status)
	}
	if reapplied.ApiKey == "" || reapplied.ApiKey == "legacy-key" {
		t.Fatalf("expected new api key, got %q", reapplied.ApiKey)
	}
	if reapplied.ApiSecret == "" || reapplied.ApiSecret == "legacy-secret" {
		t.Fatal("expected new api secret")
	}
	if reapplied.RejectReason != "" {
		t.Fatalf("expected reject reason cleared, got %q", reapplied.RejectReason)
	}
	if reapplied.ApprovedAt == nil {
		t.Fatal("expected approved_at to be set")
	}
	if reapplied.LastUsedAt != nil {
		t.Fatal("expected last_used_at cleared")
	}
	if !reapplied.IsActive {
		t.Fatal("expected active credential after reapply")
	}
	if reapplied.DeletedAt.Valid {
		t.Fatal("expected deleted_at to be cleared")
	}

	stored, err := repo.GetByUserID(1002)
	if err != nil {
		t.Fatalf("get by user id failed: %v", err)
	}
	if stored == nil {
		t.Fatal("expected restored credential to be queryable")
	}
	if stored.ID != cred.ID {
		t.Fatalf("expected stored credential id %d, got %d", cred.ID, stored.ID)
	}
}

func TestApiCredentialServiceApplyResetsRejectedCredential(t *testing.T) {
	svc, repo, _ := setupApiCredentialServiceTest(t)
	now := time.Now()

	cred := &models.ApiCredential{
		UserID:       1003,
		ApiKey:       "old-key",
		ApiSecret:    "old-secret",
		Status:       constants.ApiCredentialStatusRejected,
		RejectReason: "missing docs",
		ApprovedAt:   &now,
		LastUsedAt:   &now,
		IsActive:     true,
	}
	if err := repo.Create(cred); err != nil {
		t.Fatalf("create rejected credential failed: %v", err)
	}

	reapplied, err := svc.Apply(1003)
	if err != nil {
		t.Fatalf("reapply failed: %v", err)
	}
	if reapplied.ID != cred.ID {
		t.Fatalf("expected to reuse credential id %d, got %d", cred.ID, reapplied.ID)
	}
	if reapplied.Status != constants.ApiCredentialStatusApproved {
		t.Fatalf("expected status %s, got %s", constants.ApiCredentialStatusApproved, reapplied.Status)
	}
	if reapplied.ApiKey == "" || reapplied.ApiKey == "old-key" {
		t.Fatalf("expected new api key, got %q", reapplied.ApiKey)
	}
	if reapplied.ApiSecret == "" || reapplied.ApiSecret == "old-secret" {
		t.Fatal("expected new api secret")
	}
	if reapplied.RejectReason != "" {
		t.Fatalf("expected reject reason cleared, got %q", reapplied.RejectReason)
	}
	if reapplied.ApprovedAt == nil {
		t.Fatal("expected approved_at to be set")
	}
	if reapplied.LastUsedAt != nil {
		t.Fatal("expected last_used_at cleared")
	}
	if !reapplied.IsActive {
		t.Fatal("expected active credential after reapply")
	}
}

func TestApiCredentialServiceApplyApprovesExistingPendingReview(t *testing.T) {
	svc, repo, _ := setupApiCredentialServiceTest(t)

	cred := &models.ApiCredential{
		UserID: 1004,
		Status: constants.ApiCredentialStatusPendingReview,
	}
	if err := repo.Create(cred); err != nil {
		t.Fatalf("create pending credential failed: %v", err)
	}

	reapplied, err := svc.Apply(1004)
	if err != nil {
		t.Fatalf("apply pending credential failed: %v", err)
	}
	if reapplied.ID != cred.ID {
		t.Fatalf("expected to reuse credential id %d, got %d", cred.ID, reapplied.ID)
	}
	if reapplied.Status != constants.ApiCredentialStatusApproved {
		t.Fatalf("expected status %s, got %s", constants.ApiCredentialStatusApproved, reapplied.Status)
	}
	if reapplied.ApiKey == "" {
		t.Fatal("expected api key to be generated")
	}
	if reapplied.ApiSecret == "" {
		t.Fatal("expected api secret to be generated")
	}
	if reapplied.ApprovedAt == nil {
		t.Fatal("expected approved_at to be set")
	}
	if !reapplied.IsActive {
		t.Fatal("expected active credential")
	}
}

func TestApiCredentialServiceApplyBlocksApprovedAndDisabled(t *testing.T) {
	cases := []struct {
		name   string
		userID uint
		status string
	}{
		{name: "approved", userID: 1005, status: constants.ApiCredentialStatusApproved},
		{name: "disabled", userID: 1006, status: constants.ApiCredentialStatusDisabled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := setupApiCredentialServiceTest(t)
			cred := &models.ApiCredential{
				UserID: tc.userID,
				Status: tc.status,
			}
			if err := repo.Create(cred); err != nil {
				t.Fatalf("create credential failed: %v", err)
			}

			_, err := svc.Apply(tc.userID)
			if !errors.Is(err, ErrApiCredentialExists) {
				t.Fatalf("expected ErrApiCredentialExists, got %v", err)
			}
		})
	}
}
