package service

import (
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strings"

	"github.com/dujiao-next/internal/models"
)

var purchaseDiagnosticEvents = map[string]bool{
	"product_impression": true, "quick_buy_click": true, "quick_buy_open": true, "quick_buy_close": true,
	"checkout_exit": true, "payment_exit": true, "checkout_resume": true, "payment_resume": true,
}

var purchaseDiagnosticText = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,120}$`)
var purchaseDiagnosticReason = regexp.MustCompile(`^[a-z_]{1,40}$`)
var purchaseDiagnosticAmount = regexp.MustCompile(`^\d{1,9}(\.\d{1,4})?$`)

var purchaseDiagnosticStrings = diagnosticPropertySet(`instrumentation_version page_view_id source stock_status purchase_type outcome
mode checkout_mode currency provider_type channel_type interaction_mode order_status blocked_reason
experiment_id experiment_variant experiment_revision payment_flow_variant direct_payment_state handoff_flow_id
handoff_source_order_no handoff_phase handoff_attribution purchase_route_experiment_id purchase_route_variant
purchase_route_assignment_id purchase_route_entry purchase_route_actual_route`)
var purchaseDiagnosticBooleans = diagnosticPropertySet(`external sold_out authenticated is_guest use_balance experiment_eligible handoff_target
submitting submit_attempted has_error has_preview_error coupon_entered coupon_applied order_created loading redirecting capturing guest_auth_blocked`)
var purchaseDiagnosticNumbers = diagnosticPropertySet(`position item_count total_quantity quantity channel_id handoff_source_product_id handoff_target_product_id`)

func diagnosticPropertySet(fields string) map[string]bool {
	result := make(map[string]bool)
	for _, field := range strings.Fields(fields) {
		result[field] = true
	}
	return result
}

// 新链路诊断使用字段白名单；即使客户端绕过清洗，也不保存表单或网关原文。
func sanitizePurchaseDiagnosticProperties(raw models.JSON) models.JSON {
	result := models.JSON{}
	for key, value := range raw {
		if purchaseDiagnosticStrings[key] {
			if text, ok := value.(string); ok && purchaseDiagnosticText.MatchString(text) {
				result[key] = text
			}
		}
		if purchaseDiagnosticBooleans[key] {
			if flag, ok := value.(bool); ok {
				result[key] = flag
			}
		}
		if purchaseDiagnosticNumbers[key] {
			if number, ok := purchaseDiagnosticNumber(value); ok && number >= 0 {
				result[key] = math.Min(number, 100000000)
			}
		}
		if key == "amount" || key == "original_amount" {
			if text, ok := value.(string); ok && purchaseDiagnosticAmount.MatchString(text) {
				result[key] = text
			}
		}
		if key == "product_ids" {
			// HTTP JSON 解码是 []interface{}；先规范化也覆盖服务内的 []uint / []int。
			encoded, err := json.Marshal(value)
			if err != nil || len(encoded) > 4096 {
				continue
			}
			var ids []float64
			if json.Unmarshal(encoded, &ids) != nil {
				continue
			}
			clean := make([]float64, 0, 30)
			for _, id := range ids {
				if id > 0 && id <= 9007199254740991 && math.Trunc(id) == id && len(clean) < 30 {
					clean = append(clean, id)
				}
			}
			result[key] = clean
		}
	}
	return result
}

func purchaseDiagnosticNumber(value interface{}) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case int:
		number = float64(value)
	case uint:
		number = float64(value)
	case int64:
		number = float64(value)
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func purchaseDiagnosticURL(raw string, originOnly bool) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if originOnly {
		parsed.Path = ""
		parsed.RawPath = ""
	}
	return truncateBehaviorText(parsed.String(), 1000)
}
