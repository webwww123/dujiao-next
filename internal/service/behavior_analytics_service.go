package service

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"

	"github.com/google/uuid"
)

const (
	behaviorEventBatchMax      = 50
	behaviorEventRetention     = 180 * 24 * time.Hour
	behaviorCleanupInterval    = 12 * time.Hour
	behaviorSessionIdleTimeout = 30 * time.Minute
)

var allowedBehaviorEventNames = map[string]struct{}{
	"product_impression":       {},
	"quick_buy_click":          {},
	"quick_buy_close":          {},
	"checkout_exit":            {},
	"payment_exit":             {},
	"checkout_resume":          {},
	"payment_resume":           {},
	"page_view":                {},
	"page_exit":                {},
	"ui_click":                 {},
	"sponsor_ad_impression":    {},
	"scroll_depth":             {},
	"form_field_focus":         {},
	"form_field_blur":          {},
	"frontend_error":           {},
	"api_error":                {},
	"product_view":             {},
	"product_click":            {},
	"quick_buy_open":           {},
	"add_to_cart":              {},
	"add_to_cart_blocked":      {},
	"buy_now":                  {},
	"buy_now_blocked":          {},
	"cart_view":                {},
	"cart_remove":              {},
	"cart_restore":             {},
	"cart_quantity_change":     {},
	"checkout_view":            {},
	"checkout_mode_change":     {},
	"checkout_submit":          {},
	"checkout_blocked":         {},
	"coupon_entered":           {},
	"coupon_applied":           {},
	"coupon_rejected":          {},
	"order_created":            {},
	"order_create_failed":      {},
	"payment_view":             {},
	"payment_channel_selected": {},
	"payment_submit":           {},
	"payment_started":          {},
	"payment_create_failed":    {},
	"payment_link_opened":      {},
	"payment_link_copied":      {},
	"payment_status":           {},
	"payment_success":          {},
	"payment_failed":           {},
	"order_canceled":           {},
	"order_expired":            {},
}

var behaviorSensitivePropertyFragments = []string{
	"password",
	"passwd",
	"token",
	"secret",
	"authorization",
	"cookie",
	"card_secret",
	"manual_form_data",
	"manual_form_submission",
}

// BehaviorAnalyticsService 第一方用户行为采集与漏斗分析服务。
type BehaviorAnalyticsService struct {
	repo        repository.BehaviorEventRepository
	orderRepo   repository.OrderRepository
	productRepo repository.ProductRepository
	couponRepo  repository.CouponRepository
	userRepo    repository.UserRepository

	cleanupMu   sync.Mutex
	lastCleanup time.Time
}

// NewBehaviorAnalyticsService 创建行为分析服务。
func NewBehaviorAnalyticsService(
	repo repository.BehaviorEventRepository,
	orderRepo repository.OrderRepository,
	productRepo repository.ProductRepository,
	couponRepo repository.CouponRepository,
	userRepo repository.UserRepository,
) *BehaviorAnalyticsService {
	return &BehaviorAnalyticsService{
		repo:        repo,
		orderRepo:   orderRepo,
		productRepo: productRepo,
		couponRepo:  couponRepo,
		userRepo:    userRepo,
	}
}

// RecordBehaviorEventInput 单条前台行为事件输入。
type RecordBehaviorEventInput struct {
	EventID         string
	VisitorID       string
	SessionID       string
	GuestEmail      string
	EventName       string
	PagePath        string
	PageURL         string
	Referrer        string
	ElementKey      string
	ElementText     string
	ElementSelector string
	ProductID       uint
	SKUID           uint
	OrderNo         string
	PaymentID       uint
	CouponID        uint
	CouponCode      string
	Reason          string
	DurationMS      int64
	ScrollDepth     int
	Properties      models.JSON
	OccurredAt      *time.Time
}

// RecordBehaviorBatchInput 一批行为事件及服务端请求上下文。
type RecordBehaviorBatchInput struct {
	UserID    uint
	ClientIP  string
	UserAgent string
	Events    []RecordBehaviorEventInput
}

// BehaviorAnalyticsOverviewResponse 行为分析总览。
type BehaviorAnalyticsOverviewResponse struct {
	Range       string                   `json:"range"`
	From        string                   `json:"from"`
	To          string                   `json:"to"`
	Timezone    string                   `json:"timezone"`
	KPI         BehaviorAnalyticsKPI     `json:"kpi"`
	Funnel      []BehaviorFunnelStep     `json:"funnel"`
	Dropoffs    []BehaviorDropoffInsight `json:"dropoffs"`
	Coupon      BehaviorCouponOverview   `json:"coupon"`
	TopClicks   []BehaviorClickInsight   `json:"top_clicks"`
	TopProducts []BehaviorProductInsight `json:"top_products"`
}

// BehaviorAnalyticsKPI 核心用户行为指标。
type BehaviorAnalyticsKPI struct {
	Visitors              int64  `json:"visitors"`
	Sessions              int64  `json:"sessions"`
	PageViews             int64  `json:"page_views"`
	ProductViewSessions   int64  `json:"product_view_sessions"`
	CartActionSessions    int64  `json:"cart_action_sessions"`
	CheckoutSessions      int64  `json:"checkout_sessions"`
	CouponAppliedSessions int64  `json:"coupon_applied_sessions"`
	OrderSessions         int64  `json:"order_sessions"`
	PaidSessions          int64  `json:"paid_sessions"`
	InProgressSessions    int64  `json:"in_progress_sessions"`
	CheckoutToPaidRate    string `json:"checkout_to_paid_rate"`
}

// BehaviorFunnelStep 漏斗步骤。
type BehaviorFunnelStep struct {
	Key                 string `json:"key"`
	Sessions            int64  `json:"sessions"`
	ConversionFromPrior string `json:"conversion_from_prior"`
}

// BehaviorDropoffInsight 流失原因聚合。
type BehaviorDropoffInsight struct {
	Reason   string `json:"reason"`
	Sessions int64  `json:"sessions"`
	Share    string `json:"share"`
}

// BehaviorCouponOverview 优惠券使用与流失概览。
type BehaviorCouponOverview struct {
	EnteredSessions   int64                   `json:"entered_sessions"`
	AppliedSessions   int64                   `json:"applied_sessions"`
	RejectedSessions  int64                   `json:"rejected_sessions"`
	OrderedSessions   int64                   `json:"ordered_sessions"`
	PaidSessions      int64                   `json:"paid_sessions"`
	AbandonedSessions int64                   `json:"abandoned_sessions"`
	PendingSessions   int64                   `json:"pending_sessions"`
	Coupons           []BehaviorCouponInsight `json:"coupons"`
}

// BehaviorCouponInsight 单个优惠券转化表现。
type BehaviorCouponInsight struct {
	CouponID          uint   `json:"coupon_id"`
	CouponCode        string `json:"coupon_code"`
	AppliedSessions   int64  `json:"applied_sessions"`
	OrderedSessions   int64  `json:"ordered_sessions"`
	PaidSessions      int64  `json:"paid_sessions"`
	AbandonedSessions int64  `json:"abandoned_sessions"`
}

// BehaviorClickInsight 点击热点。
type BehaviorClickInsight struct {
	ElementKey      string `json:"element_key"`
	ElementText     string `json:"element_text"`
	ElementSelector string `json:"element_selector"`
	PagePath        string `json:"page_path"`
	Clicks          int64  `json:"clicks"`
	Sessions        int64  `json:"sessions"`
}

// BehaviorProductInsight 商品行为表现。
type BehaviorProductInsight struct {
	ProductID        uint        `json:"product_id"`
	Title            models.JSON `json:"title"`
	ViewSessions     int64       `json:"view_sessions"`
	IntentSessions   int64       `json:"intent_sessions"`
	CheckoutSessions int64       `json:"checkout_sessions"`
	OrderSessions    int64       `json:"order_sessions"`
	PaidSessions     int64       `json:"paid_sessions"`
}

// BehaviorSessionUser 会话关联的登录用户。
type BehaviorSessionUser struct {
	ID          uint   `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// BehaviorSessionSummary 单个会话摘要。
type BehaviorSessionSummary struct {
	SessionID      string               `json:"session_id"`
	VisitorID      string               `json:"visitor_id"`
	User           *BehaviorSessionUser `json:"user,omitempty"`
	GuestEmail     string               `json:"guest_email,omitempty"`
	ClientIP       string               `json:"client_ip,omitempty"`
	UserAgent      string               `json:"user_agent,omitempty"`
	StartedAt      time.Time            `json:"started_at"`
	LastAt         time.Time            `json:"last_at"`
	DurationMS     int64                `json:"duration_ms"`
	EventCount     int64                `json:"event_count"`
	LastPage       string               `json:"last_page"`
	MaxScrollDepth int                  `json:"max_scroll_depth"`
	Stage          string               `json:"stage"`
	DropoffReason  string               `json:"dropoff_reason,omitempty"`
	IsPaid         bool                 `json:"is_paid"`
	ProductIDs     []uint               `json:"product_ids"`
	CouponCodes    []string             `json:"coupon_codes"`
	OrderNos       []string             `json:"order_nos"`
	OrderStatuses  map[string]string    `json:"order_statuses"`
}

// BehaviorSessionEvent 管理端会话时间线事件。
type BehaviorSessionEvent struct {
	ID              uint        `json:"id"`
	EventName       string      `json:"event_name"`
	PagePath        string      `json:"page_path,omitempty"`
	PageURL         string      `json:"page_url,omitempty"`
	Referrer        string      `json:"referrer,omitempty"`
	ElementKey      string      `json:"element_key,omitempty"`
	ElementText     string      `json:"element_text,omitempty"`
	ElementSelector string      `json:"element_selector,omitempty"`
	ProductID       uint        `json:"product_id,omitempty"`
	SKUID           uint        `json:"sku_id,omitempty"`
	OrderNo         string      `json:"order_no,omitempty"`
	PaymentID       uint        `json:"payment_id,omitempty"`
	CouponID        uint        `json:"coupon_id,omitempty"`
	CouponCode      string      `json:"coupon_code,omitempty"`
	Reason          string      `json:"reason,omitempty"`
	DurationMS      int64       `json:"duration_ms,omitempty"`
	ScrollDepth     int         `json:"scroll_depth,omitempty"`
	Properties      models.JSON `json:"properties,omitempty"`
	OccurredAt      time.Time   `json:"occurred_at"`
}

// BehaviorSessionDetail 单个会话完整轨迹。
type BehaviorSessionDetail struct {
	Summary BehaviorSessionSummary `json:"summary"`
	Events  []BehaviorSessionEvent `json:"events"`
}

// RecordBatch 清洗并批量写入行为事件。返回实际接受的事件数量。
func (s *BehaviorAnalyticsService) RecordBatch(input RecordBehaviorBatchInput) (int, error) {
	if s == nil || s.repo == nil || len(input.Events) == 0 {
		return 0, nil
	}

	rows := input.Events
	if len(rows) > behaviorEventBatchMax {
		rows = rows[:behaviorEventBatchMax]
	}

	now := time.Now().UTC()
	clientIP := truncateBehaviorText(strings.TrimSpace(input.ClientIP), 64)
	userAgent := truncateBehaviorText(strings.TrimSpace(input.UserAgent), 1000)
	couponCache := make(map[string]uint)
	events := make([]models.BehaviorEvent, 0, len(rows))

	for _, row := range rows {
		eventName := normalizeBehaviorEventName(row.EventName)
		if eventName == "" {
			continue
		}
		if purchaseDiagnosticEvents[eventName] && (eventName != "quick_buy_open" || row.Properties["instrumentation_version"] == "journey_v1") {
			row.GuestEmail = ""
			row.CouponCode = ""
			row.ElementKey = ""
			row.ElementText = ""
			row.ElementSelector = ""
			row.PageURL = purchaseDiagnosticURL(row.PageURL, false)
			row.Referrer = purchaseDiagnosticURL(row.Referrer, true)
			if !purchaseDiagnosticReason.MatchString(row.Reason) {
				row.Reason = ""
			}
			row.Properties = sanitizePurchaseDiagnosticProperties(row.Properties)
		}
		sessionID := normalizeBehaviorIdentifier(row.SessionID)
		if sessionID == "" {
			continue
		}
		visitorID := normalizeBehaviorIdentifier(row.VisitorID)
		if visitorID == "" {
			visitorID = sessionID
		}
		eventID := normalizeBehaviorIdentifier(row.EventID)
		if eventID == "" {
			eventID = uuid.NewString()
		}

		occurredAt := now
		if row.OccurredAt != nil && !row.OccurredAt.IsZero() {
			candidate := row.OccurredAt.UTC()
			if candidate.After(now.Add(-48*time.Hour)) && candidate.Before(now.Add(10*time.Minute)) {
				occurredAt = candidate
			}
		}

		couponCode := truncateBehaviorText(strings.TrimSpace(row.CouponCode), 160)
		couponID := row.CouponID
		if couponCode != "" && couponID == 0 {
			if cached, ok := couponCache[couponCode]; ok {
				couponID = cached
			} else if s.couponRepo != nil {
				coupon, err := s.couponRepo.GetByCode(couponCode)
				if err == nil && coupon != nil {
					couponID = coupon.ID
				}
				couponCache[couponCode] = couponID
			}
		}

		guestEmail := truncateBehaviorText(strings.TrimSpace(row.GuestEmail), 320)
		if normalized, err := NormalizeEmail(guestEmail); err == nil {
			guestEmail = normalized
		}

		pageURL := sanitizeBehaviorURL(row.PageURL)
		pagePath := sanitizeBehaviorPath(row.PagePath)
		if pagePath == "" && pageURL != "" {
			if parsed, err := url.Parse(pageURL); err == nil {
				pagePath = sanitizeBehaviorPath(parsed.Path)
			}
		}

		durationMS := row.DurationMS
		if durationMS < 0 {
			durationMS = 0
		}
		if durationMS > int64((24*time.Hour)/time.Millisecond) {
			durationMS = int64((24 * time.Hour) / time.Millisecond)
		}
		scrollDepth := row.ScrollDepth
		if scrollDepth < 0 {
			scrollDepth = 0
		}
		if scrollDepth > 100 {
			scrollDepth = 100
		}

		events = append(events, models.BehaviorEvent{
			EventID:         eventID,
			VisitorID:       visitorID,
			SessionID:       sessionID,
			UserID:          input.UserID,
			GuestEmail:      guestEmail,
			ClientIP:        clientIP,
			UserAgent:       userAgent,
			EventName:       eventName,
			PagePath:        pagePath,
			PageURL:         pageURL,
			Referrer:        sanitizeBehaviorURL(row.Referrer),
			ElementKey:      truncateBehaviorText(strings.TrimSpace(row.ElementKey), 160),
			ElementText:     truncateBehaviorText(strings.TrimSpace(row.ElementText), 500),
			ElementSelector: truncateBehaviorText(strings.TrimSpace(row.ElementSelector), 500),
			ProductID:       row.ProductID,
			SKUID:           row.SKUID,
			OrderNo:         normalizeBehaviorOrderNo(row.OrderNo),
			PaymentID:       row.PaymentID,
			CouponID:        couponID,
			CouponCode:      couponCode,
			Reason:          truncateBehaviorText(strings.TrimSpace(row.Reason), 500),
			DurationMS:      durationMS,
			ScrollDepth:     scrollDepth,
			Properties:      sanitizeBehaviorProperties(row.Properties),
			OccurredAt:      occurredAt,
			CreatedAt:       now,
		})
	}

	if len(events) == 0 {
		return 0, nil
	}
	if err := s.repo.CreateBatch(events); err != nil {
		return 0, err
	}
	s.maybeCleanup(now)
	return len(events), nil
}

func (s *BehaviorAnalyticsService) maybeCleanup(now time.Time) {
	if s == nil || s.repo == nil {
		return
	}
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < behaviorCleanupInterval {
		return
	}
	if err := s.repo.DeleteBefore(now.Add(-behaviorEventRetention)); err == nil {
		s.lastCleanup = now
	}
}

func normalizeBehaviorEventName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, "-", "_")
	if _, ok := allowedBehaviorEventNames[name]; !ok {
		return ""
	}
	return name
}

func normalizeBehaviorIdentifier(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return ""
	}
	return value
}

func normalizeBehaviorOrderNo(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return value
}

func sanitizeBehaviorPath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
		value = parsed.Path
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return truncateBehaviorText(value, 300)
}

func sanitizeBehaviorURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return truncateBehaviorText(value, 1000)
	}
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(lower, "password") ||
			strings.Contains(lower, "token") ||
			strings.Contains(lower, "secret") ||
			strings.Contains(lower, "session_id") ||
			strings.Contains(lower, "payerid") ||
			strings.Contains(lower, "payer_id") ||
			lower == "code" {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return truncateBehaviorText(parsed.String(), 1000)
}

func truncateBehaviorText(value string, max int) string {
	if max <= 0 || value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func sanitizeBehaviorProperties(raw models.JSON) models.JSON {
	if len(raw) == 0 {
		return models.JSON{}
	}
	result := make(models.JSON)
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(result) >= 50 || behaviorPropertyKeySensitive(key) {
			continue
		}
		cleanKey := truncateBehaviorText(strings.TrimSpace(key), 100)
		if cleanKey == "" {
			continue
		}
		if value, ok := sanitizeBehaviorPropertyValue(raw[key], 0); ok {
			result[cleanKey] = value
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 16*1024 {
		return models.JSON{"truncated": true}
	}
	return result
}

func behaviorPropertyKeySensitive(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, fragment := range behaviorSensitivePropertyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func sanitizeBehaviorPropertyValue(value interface{}, depth int) (interface{}, bool) {
	if depth > 3 {
		return nil, false
	}
	switch typed := value.(type) {
	case nil:
		return nil, true
	case bool:
		return typed, true
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return typed, true
	case string:
		return truncateBehaviorText(typed, 1000), true
	case []interface{}:
		limit := len(typed)
		if limit > 30 {
			limit = 30
		}
		result := make([]interface{}, 0, limit)
		for _, item := range typed[:limit] {
			if clean, ok := sanitizeBehaviorPropertyValue(item, depth+1); ok {
				result = append(result, clean)
			}
		}
		return result, true
	case []string:
		limit := len(typed)
		if limit > 30 {
			limit = 30
		}
		result := make([]string, 0, limit)
		for _, item := range typed[:limit] {
			result = append(result, truncateBehaviorText(item, 1000))
		}
		return result, true
	case []uint:
		if len(typed) > 30 {
			typed = typed[:30]
		}
		return typed, true
	case map[string]interface{}:
		result := make(map[string]interface{})
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if len(result) >= 30 || behaviorPropertyKeySensitive(key) {
				continue
			}
			if clean, ok := sanitizeBehaviorPropertyValue(typed[key], depth+1); ok {
				result[truncateBehaviorText(key, 100)] = clean
			}
		}
		return result, true
	default:
		encoded, err := json.Marshal(typed)
		if err != nil || len(encoded) > 4096 {
			return nil, false
		}
		var decoded interface{}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return nil, false
		}
		return sanitizeBehaviorPropertyValue(decoded, depth+1)
	}
}
