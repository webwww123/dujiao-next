package public

import "github.com/shopspring/decimal"

func shouldCreatePaymentForOrder(channelID uint, useBalance bool, totalAmount decimal.Decimal) bool {
	if channelID != 0 || useBalance {
		return true
	}
	return totalAmount.Round(2).Equal(decimal.Zero)
}
