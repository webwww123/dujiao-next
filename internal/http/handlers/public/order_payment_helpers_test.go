package public

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestShouldCreatePaymentForOrder(t *testing.T) {
	tests := []struct {
		name        string
		channelID   uint
		useBalance  bool
		totalAmount decimal.Decimal
		want        bool
	}{
		{name: "paid order without payment method returns order only", totalAmount: decimal.RequireFromString("0.01"), want: false},
		{name: "paid order with channel creates payment", channelID: 1, totalAmount: decimal.RequireFromString("0.01"), want: true},
		{name: "paid order with balance creates payment", useBalance: true, totalAmount: decimal.RequireFromString("0.01"), want: true},
		{name: "zero amount order creates payment", totalAmount: decimal.Zero, want: true},
		{name: "rounded zero amount order creates payment", totalAmount: decimal.RequireFromString("0.004"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCreatePaymentForOrder(tt.channelID, tt.useBalance, tt.totalAmount); got != tt.want {
				t.Fatalf("shouldCreatePaymentForOrder() = %v, want %v", got, tt.want)
			}
		})
	}
}
