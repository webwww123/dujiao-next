package service

import (
	"path/filepath"
	"testing"

	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 退款工具以 TEXT 写入，商城后台以 BLOB 写入，两种格式都必须真正触发拦截。
func TestRiskControlReadsSQLiteTextAndBlob(t *testing.T) {
	const config = `{"enabled":true,"email_blacklist":["blocked@example.com"],"ip_blacklist":["192.0.2.1"]}`
	for _, storage := range []string{"text", "blob"} {
		t.Run(storage, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "risk.db")), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.AutoMigrate(&models.Setting{}); err != nil {
				t.Fatal(err)
			}
			var value any = config
			if storage == "blob" {
				value = []byte(config)
			}
			if err := db.Exec("INSERT INTO settings (key, value_json) VALUES (?, ?)", "order_risk_control_config", value).Error; err != nil {
				t.Fatal(err)
			}
			svc := NewOrderRiskControlService(NewSettingService(repository.NewSettingRepository(db)), &mockOrderRepoForRisk{})
			for _, input := range []RiskCheckInput{
				{UserID: 1, Email: "BLOCKED@example.com", ClientIP: "192.0.2.2"},
				{IsGuest: true, GuestEmail: "blocked@example.com", ClientIP: "192.0.2.2"},
			} {
				if err := svc.CheckOrderAllowed(input); err != ErrRiskEmailBlacklisted {
					t.Fatalf("expected email blacklist rejection, got %v", err)
				}
			}
			if err := svc.CheckOrderAllowed(RiskCheckInput{ClientIP: "192.0.2.1"}); err != ErrRiskIPBlacklisted {
				t.Fatalf("expected IP blacklist rejection, got %v", err)
			}
			if err := svc.CheckOrderAllowed(RiskCheckInput{Email: "allowed@example.com", ClientIP: "192.0.2.2"}); err != nil {
				t.Fatalf("unlisted customer should remain allowed: %v", err)
			}
		})
	}
}
