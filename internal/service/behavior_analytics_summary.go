package service

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
)

// ErrBehaviorSessionNotFound 行为会话不存在。
var ErrBehaviorSessionNotFound = errors.New("behavior session not found")

type behaviorSessionState struct {
	SessionID      string
	VisitorID      string
	UserID         uint
	GuestEmail     string
	ClientIP       string
	UserAgent      string
	StartedAt      time.Time
	LastAt         time.Time
	EventCount     int64
	LastPage       string
	DurationMS     int64
	MaxScrollDepth int

	EventNames       map[string]struct{}
	LastEventAt      map[string]time.Time
	ProductIDs       map[uint]struct{}
	OrderProductIDs  map[uint]struct{}
	CouponIDs        map[uint]struct{}
	AppliedCouponIDs map[uint]struct{}
	CouponCodes      map[string]struct{}
	OrderNos         map[string]struct{}

	LatestFailureName   string
	LatestFailureReason string
	LatestFailureAt     time.Time
}

type behaviorProductAggregate struct {
	ViewSessions     map[string]struct{}
	IntentSessions   map[string]struct{}
	CheckoutSessions map[string]struct{}
	OrderSessions    map[string]struct{}
	PaidSessions     map[string]struct{}
}

type behaviorCouponAggregate struct {
	AppliedSessions map[string]struct{}
	OrderedSessions map[string]struct{}
	PaidSessions    map[string]struct{}
	Abandoned       map[string]struct{}
}

type behaviorClickAggregate struct {
	ElementKey      string
	ElementText     string
	ElementSelector string
	PagePath        string
	Clicks          int64
	Sessions        map[string]struct{}
}

func newBehaviorSessionState(sessionID string) *behaviorSessionState {
	return &behaviorSessionState{
		SessionID:        sessionID,
		EventNames:       make(map[string]struct{}),
		LastEventAt:      make(map[string]time.Time),
		ProductIDs:       make(map[uint]struct{}),
		OrderProductIDs:  make(map[uint]struct{}),
		CouponIDs:        make(map[uint]struct{}),
		AppliedCouponIDs: make(map[uint]struct{}),
		CouponCodes:      make(map[string]struct{}),
		OrderNos:         make(map[string]struct{}),
	}
}

func (state *behaviorSessionState) apply(event models.BehaviorEvent) {
	if state == nil {
		return
	}
	if state.SessionID == "" {
		state.SessionID = event.SessionID
	}
	if event.VisitorID != "" {
		state.VisitorID = event.VisitorID
	}
	if event.UserID > 0 {
		state.UserID = event.UserID
	}
	if event.GuestEmail != "" {
		state.GuestEmail = event.GuestEmail
	}
	if event.ClientIP != "" {
		state.ClientIP = event.ClientIP
	}
	if event.UserAgent != "" {
		state.UserAgent = event.UserAgent
	}
	if state.StartedAt.IsZero() || event.OccurredAt.Before(state.StartedAt) {
		state.StartedAt = event.OccurredAt
	}
	if state.LastAt.IsZero() || event.OccurredAt.After(state.LastAt) {
		state.LastAt = event.OccurredAt
	}
	state.EventCount++
	if event.PagePath != "" {
		state.LastPage = event.PagePath
	}
	if event.DurationMS > 0 {
		state.DurationMS += event.DurationMS
	}
	if event.ScrollDepth > state.MaxScrollDepth {
		state.MaxScrollDepth = event.ScrollDepth
	}

	state.EventNames[event.EventName] = struct{}{}
	state.LastEventAt[event.EventName] = event.OccurredAt
	for _, productID := range behaviorEventProductIDs(event) {
		state.ProductIDs[productID] = struct{}{}
		if event.EventName == "order_created" {
			state.OrderProductIDs[productID] = struct{}{}
		}
	}
	if event.CouponID > 0 {
		state.CouponIDs[event.CouponID] = struct{}{}
		if event.EventName == "coupon_applied" {
			state.AppliedCouponIDs[event.CouponID] = struct{}{}
		}
	}
	if code := strings.TrimSpace(event.CouponCode); code != "" {
		state.CouponCodes[code] = struct{}{}
	}
	if event.OrderNo != "" {
		state.OrderNos[event.OrderNo] = struct{}{}
	}

	switch event.EventName {
	case "coupon_rejected", "checkout_blocked", "order_create_failed", "payment_create_failed", "payment_failed":
		if state.LatestFailureAt.IsZero() || !event.OccurredAt.Before(state.LatestFailureAt) {
			state.LatestFailureName = event.EventName
			state.LatestFailureReason = strings.TrimSpace(event.Reason)
			state.LatestFailureAt = event.OccurredAt
		}
	}
}

func behaviorEventProductIDs(event models.BehaviorEvent) []uint {
	set := make(map[uint]struct{})
	if event.ProductID > 0 {
		set[event.ProductID] = struct{}{}
	}
	if event.Properties != nil {
		if raw, ok := event.Properties["product_ids"]; ok {
			for _, id := range behaviorUintSlice(raw) {
				if id > 0 {
					set[id] = struct{}{}
				}
			}
		}
	}
	result := make([]uint, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func behaviorUintSlice(raw interface{}) []uint {
	result := make([]uint, 0)
	switch typed := raw.(type) {
	case []uint:
		return append(result, typed...)
	case []int:
		for _, value := range typed {
			if value > 0 {
				result = append(result, uint(value))
			}
		}
	case []float64:
		for _, value := range typed {
			if value > 0 {
				result = append(result, uint(value))
			}
		}
	case []interface{}:
		for _, value := range typed {
			switch item := value.(type) {
			case float64:
				if item > 0 {
					result = append(result, uint(item))
				}
			case int:
				if item > 0 {
					result = append(result, uint(item))
				}
			case uint:
				if item > 0 {
					result = append(result, item)
				}
			}
		}
	}
	return result
}

func buildBehaviorStates(events []models.BehaviorEvent) map[string]*behaviorSessionState {
	states := make(map[string]*behaviorSessionState)
	for _, event := range events {
		state := states[event.SessionID]
		if state == nil {
			state = newBehaviorSessionState(event.SessionID)
			states[event.SessionID] = state
		}
		state.apply(event)
	}
	return states
}

func behaviorStateHas(state *behaviorSessionState, eventName string) bool {
	if state == nil {
		return false
	}
	_, ok := state.EventNames[eventName]
	return ok
}

func behaviorStateHasAny(state *behaviorSessionState, eventNames ...string) bool {
	for _, name := range eventNames {
		if behaviorStateHas(state, name) {
			return true
		}
	}
	return false
}

func behaviorOrderStatusPaid(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case constants.OrderStatusPaid,
		constants.OrderStatusFulfilling,
		constants.OrderStatusPartiallyDelivered,
		constants.OrderStatusDelivered,
		constants.OrderStatusCompleted,
		constants.OrderStatusPartiallyRefunded,
		constants.OrderStatusRefunded:
		return true
	default:
		return false
	}
}

func behaviorSessionPaid(state *behaviorSessionState, orderStatuses map[string]string) bool {
	if behaviorStateHas(state, "payment_success") {
		return true
	}
	for orderNo := range state.OrderNos {
		if behaviorOrderStatusPaid(orderStatuses[orderNo]) {
			return true
		}
	}
	return false
}

func behaviorSessionClassification(state *behaviorSessionState, orderStatuses map[string]string, now time.Time) (stage, reason string, paid, inProgress bool) {
	if state == nil {
		return "browsing", "browse_only", false, false
	}

	switch {
	case behaviorSessionPaid(state, orderStatuses):
		stage = "paid"
		paid = true
	case behaviorStateHasAny(state, "payment_started", "payment_submit", "payment_view"):
		stage = "payment"
	case behaviorStateHas(state, "order_created"):
		stage = "ordered"
	case behaviorStateHasAny(state, "checkout_view", "checkout_submit"):
		stage = "checkout"
	case behaviorStateHasAny(state, "add_to_cart", "buy_now", "cart_view"):
		stage = "cart"
	case behaviorStateHasAny(state, "product_view", "product_click", "quick_buy_open"):
		stage = "product"
	default:
		stage = "browsing"
	}
	if paid {
		return stage, "", true, false
	}

	if !state.LastAt.IsZero() && now.Sub(state.LastAt) < behaviorSessionIdleTimeout {
		return stage, "in_progress", false, true
	}

	if behaviorStateHas(state, "order_expired") {
		return stage, "order_expired", false, false
	}
	for orderNo := range state.OrderNos {
		if orderStatuses[orderNo] == constants.OrderStatusCanceled {
			return stage, "order_canceled_or_expired", false, false
		}
	}

	if behaviorFailureStillRelevant(state) {
		reason = state.LatestFailureName
		if detail := strings.TrimSpace(state.LatestFailureReason); detail != "" {
			reason += ":" + detail
		}
		return stage, reason, false, false
	}

	switch {
	case behaviorStateHasAny(state, "payment_started", "payment_submit", "payment_link_opened"):
		reason = "payment_not_completed"
	case behaviorStateHas(state, "order_created"):
		reason = "order_created_no_payment"
	case behaviorStateHas(state, "checkout_submit"):
		reason = "checkout_submit_no_order"
	case behaviorStateHas(state, "checkout_view"):
		reason = "checkout_no_submit"
	case behaviorStateHasAny(state, "add_to_cart", "buy_now", "cart_view"):
		reason = "cart_no_checkout"
	case behaviorStateHasAny(state, "product_view", "product_click", "quick_buy_open"):
		reason = "product_no_action"
	default:
		reason = "browse_only"
	}
	return stage, reason, false, false
}

func behaviorFailureStillRelevant(state *behaviorSessionState) bool {
	if state == nil || state.LatestFailureName == "" || state.LatestFailureAt.IsZero() {
		return false
	}
	after := func(eventName string) bool {
		at := state.LastEventAt[eventName]
		return !at.IsZero() && at.After(state.LatestFailureAt)
	}
	switch state.LatestFailureName {
	case "payment_create_failed", "payment_failed":
		return !after("payment_started") && !after("payment_success")
	case "order_create_failed":
		return !after("order_created")
	case "checkout_blocked":
		return !after("checkout_submit") && !after("order_created")
	case "coupon_rejected":
		return !after("coupon_applied") && !after("checkout_submit") && !after("order_created")
	default:
		return true
	}
}

// GetOverview 获取行为漏斗、流失原因、优惠券与点击热点总览。
func (s *BehaviorAnalyticsService) GetOverview(input DashboardQueryInput) (*BehaviorAnalyticsOverviewResponse, error) {
	window, err := resolveDashboardWindow(input, time.Now())
	if err != nil {
		return nil, err
	}
	response := &BehaviorAnalyticsOverviewResponse{
		Range:       window.rangeKey,
		From:        window.startAt.Format(time.RFC3339),
		To:          window.endAt.Add(-time.Second).Format(time.RFC3339),
		Timezone:    window.timezone,
		Funnel:      []BehaviorFunnelStep{},
		Dropoffs:    []BehaviorDropoffInsight{},
		TopClicks:   []BehaviorClickInsight{},
		TopProducts: []BehaviorProductInsight{},
		Coupon: BehaviorCouponOverview{
			Coupons: []BehaviorCouponInsight{},
		},
	}
	if s == nil || s.repo == nil {
		return response, nil
	}

	events, err := s.repo.ListRange(window.startAt, window.endAt)
	if err != nil {
		return nil, err
	}
	states := buildBehaviorStates(events)
	orderStatuses, err := s.loadBehaviorOrderStatuses(states)
	if err != nil {
		return nil, err
	}

	visitors := make(map[string]struct{})
	dropoffCounts := make(map[string]int64)
	clicks := make(map[string]*behaviorClickAggregate)
	products := make(map[uint]*behaviorProductAggregate)
	coupons := make(map[uint]*behaviorCouponAggregate)

	var pageViews int64
	for _, event := range events {
		if event.EventName == "page_view" {
			pageViews++
		}
		if event.EventName == "ui_click" {
			key := event.PagePath + "\x00" + event.ElementKey + "\x00" + event.ElementText + "\x00" + event.ElementSelector
			row := clicks[key]
			if row == nil {
				row = &behaviorClickAggregate{
					ElementKey:      event.ElementKey,
					ElementText:     event.ElementText,
					ElementSelector: event.ElementSelector,
					PagePath:        event.PagePath,
					Sessions:        make(map[string]struct{}),
				}
				clicks[key] = row
			}
			row.Clicks++
			row.Sessions[event.SessionID] = struct{}{}
		}

		for _, productID := range behaviorEventProductIDs(event) {
			row := products[productID]
			if row == nil {
				row = &behaviorProductAggregate{
					ViewSessions:     make(map[string]struct{}),
					IntentSessions:   make(map[string]struct{}),
					CheckoutSessions: make(map[string]struct{}),
					OrderSessions:    make(map[string]struct{}),
					PaidSessions:     make(map[string]struct{}),
				}
				products[productID] = row
			}
			switch event.EventName {
			case "product_view":
				row.ViewSessions[event.SessionID] = struct{}{}
			case "add_to_cart", "buy_now":
				row.IntentSessions[event.SessionID] = struct{}{}
			case "checkout_view", "checkout_submit":
				row.CheckoutSessions[event.SessionID] = struct{}{}
			case "order_created":
				row.OrderSessions[event.SessionID] = struct{}{}
			}
		}
	}

	var productViewSessions, cartActionSessions, checkoutSessions, couponAppliedSessions, orderSessions, paidSessions, inProgressSessions int64
	var couponEnteredSessions, couponRejectedSessions, couponOrderedSessions, couponPaidSessions, couponAbandonedSessions, couponPendingSessions int64
	for sessionID, state := range states {
		if state.VisitorID != "" {
			visitors[state.VisitorID] = struct{}{}
		}
		if behaviorStateHasAny(state, "product_view", "product_click", "quick_buy_open") {
			productViewSessions++
		}
		if behaviorStateHasAny(state, "add_to_cart", "buy_now") {
			cartActionSessions++
		}
		if behaviorStateHasAny(state, "checkout_view", "checkout_submit") {
			checkoutSessions++
		}
		if behaviorStateHasAny(state, "coupon_entered", "coupon_applied", "coupon_rejected") || len(state.CouponCodes) > 0 {
			couponEnteredSessions++
		}
		if behaviorStateHas(state, "coupon_applied") {
			couponAppliedSessions++
		}
		if behaviorStateHas(state, "coupon_rejected") {
			couponRejectedSessions++
		}
		if behaviorStateHas(state, "order_created") {
			orderSessions++
		}

		_, reason, paid, inProgress := behaviorSessionClassification(state, orderStatuses, time.Now())
		if paid {
			paidSessions++
		}
		if inProgress {
			inProgressSessions++
		}
		if reason != "" && reason != "in_progress" {
			dropoffCounts[reason]++
		}

		couponApplied := behaviorStateHas(state, "coupon_applied")
		if couponApplied && behaviorStateHas(state, "order_created") {
			couponOrderedSessions++
		}
		if couponApplied && paid {
			couponPaidSessions++
		}
		if couponApplied && inProgress {
			couponPendingSessions++
		}
		if couponApplied && !paid && !inProgress {
			couponAbandonedSessions++
		}

		for couponID := range state.AppliedCouponIDs {
			row := coupons[couponID]
			if row == nil {
				row = &behaviorCouponAggregate{
					AppliedSessions: make(map[string]struct{}),
					OrderedSessions: make(map[string]struct{}),
					PaidSessions:    make(map[string]struct{}),
					Abandoned:       make(map[string]struct{}),
				}
				coupons[couponID] = row
			}
			row.AppliedSessions[sessionID] = struct{}{}
			if behaviorStateHas(state, "order_created") {
				row.OrderedSessions[sessionID] = struct{}{}
			}
			if paid {
				row.PaidSessions[sessionID] = struct{}{}
			}
			if !paid && !inProgress {
				row.Abandoned[sessionID] = struct{}{}
			}
		}

		if paid {
			paidProductIDs := state.OrderProductIDs
			if len(paidProductIDs) == 0 {
				paidProductIDs = state.ProductIDs
			}
			for productID := range paidProductIDs {
				if row := products[productID]; row != nil {
					row.PaidSessions[sessionID] = struct{}{}
				}
			}
		}
	}

	response.KPI = BehaviorAnalyticsKPI{
		Visitors:              int64(len(visitors)),
		Sessions:              int64(len(states)),
		PageViews:             pageViews,
		ProductViewSessions:   productViewSessions,
		CartActionSessions:    cartActionSessions,
		CheckoutSessions:      checkoutSessions,
		CouponAppliedSessions: couponAppliedSessions,
		OrderSessions:         orderSessions,
		PaidSessions:          paidSessions,
		InProgressSessions:    inProgressSessions,
		CheckoutToPaidRate:    behaviorPercent(paidSessions, checkoutSessions),
	}
	response.Funnel = buildBehaviorFunnel(int64(len(states)), productViewSessions, cartActionSessions, checkoutSessions, orderSessions, paidSessions)
	response.Dropoffs = buildBehaviorDropoffs(dropoffCounts)
	response.Coupon = BehaviorCouponOverview{
		EnteredSessions:   couponEnteredSessions,
		AppliedSessions:   couponAppliedSessions,
		RejectedSessions:  couponRejectedSessions,
		OrderedSessions:   couponOrderedSessions,
		PaidSessions:      couponPaidSessions,
		AbandonedSessions: couponAbandonedSessions,
		PendingSessions:   couponPendingSessions,
		Coupons:           []BehaviorCouponInsight{},
	}

	couponCodes, err := s.loadBehaviorCouponCodes(mapKeysUintCoupon(coupons))
	if err != nil {
		return nil, err
	}
	for couponID, row := range coupons {
		response.Coupon.Coupons = append(response.Coupon.Coupons, BehaviorCouponInsight{
			CouponID:          couponID,
			CouponCode:        couponCodes[couponID],
			AppliedSessions:   int64(len(row.AppliedSessions)),
			OrderedSessions:   int64(len(row.OrderedSessions)),
			PaidSessions:      int64(len(row.PaidSessions)),
			AbandonedSessions: int64(len(row.Abandoned)),
		})
	}
	sort.Slice(response.Coupon.Coupons, func(i, j int) bool {
		if response.Coupon.Coupons[i].AppliedSessions == response.Coupon.Coupons[j].AppliedSessions {
			return response.Coupon.Coupons[i].CouponID < response.Coupon.Coupons[j].CouponID
		}
		return response.Coupon.Coupons[i].AppliedSessions > response.Coupon.Coupons[j].AppliedSessions
	})
	if len(response.Coupon.Coupons) > 20 {
		response.Coupon.Coupons = response.Coupon.Coupons[:20]
	}

	for _, row := range clicks {
		response.TopClicks = append(response.TopClicks, BehaviorClickInsight{
			ElementKey:      row.ElementKey,
			ElementText:     row.ElementText,
			ElementSelector: row.ElementSelector,
			PagePath:        row.PagePath,
			Clicks:          row.Clicks,
			Sessions:        int64(len(row.Sessions)),
		})
	}
	sort.Slice(response.TopClicks, func(i, j int) bool {
		if response.TopClicks[i].Clicks == response.TopClicks[j].Clicks {
			return response.TopClicks[i].ElementKey < response.TopClicks[j].ElementKey
		}
		return response.TopClicks[i].Clicks > response.TopClicks[j].Clicks
	})
	if len(response.TopClicks) > 20 {
		response.TopClicks = response.TopClicks[:20]
	}

	productTitles, err := s.loadBehaviorProductTitles(mapKeysUintProduct(products))
	if err != nil {
		return nil, err
	}
	for productID, row := range products {
		response.TopProducts = append(response.TopProducts, BehaviorProductInsight{
			ProductID:        productID,
			Title:            productTitles[productID],
			ViewSessions:     int64(len(row.ViewSessions)),
			IntentSessions:   int64(len(row.IntentSessions)),
			CheckoutSessions: int64(len(row.CheckoutSessions)),
			OrderSessions:    int64(len(row.OrderSessions)),
			PaidSessions:     int64(len(row.PaidSessions)),
		})
	}
	sort.Slice(response.TopProducts, func(i, j int) bool {
		if response.TopProducts[i].ViewSessions == response.TopProducts[j].ViewSessions {
			return response.TopProducts[i].ProductID < response.TopProducts[j].ProductID
		}
		return response.TopProducts[i].ViewSessions > response.TopProducts[j].ViewSessions
	})
	if len(response.TopProducts) > 20 {
		response.TopProducts = response.TopProducts[:20]
	}

	return response, nil
}

// ListSessions 分页获取会话摘要。
func (s *BehaviorAnalyticsService) ListSessions(input DashboardQueryInput, page, pageSize int) ([]BehaviorSessionSummary, int64, error) {
	window, err := resolveDashboardWindow(input, time.Now())
	if err != nil {
		return nil, 0, err
	}
	if s == nil || s.repo == nil {
		return []BehaviorSessionSummary{}, 0, nil
	}
	rows, total, err := s.repo.ListSessions(repository.BehaviorSessionListFilter{
		StartAt:  window.startAt,
		EndAt:    window.endAt,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return nil, 0, err
	}
	sessionIDs := make([]string, 0, len(rows))
	states := make(map[string]*behaviorSessionState, len(rows))
	for _, row := range rows {
		sessionIDs = append(sessionIDs, row.SessionID)
		state := newBehaviorSessionState(row.SessionID)
		states[row.SessionID] = state
	}
	events, err := s.repo.ListBySessionIDs(sessionIDs)
	if err != nil {
		return nil, 0, err
	}
	for _, event := range events {
		if state := states[event.SessionID]; state != nil {
			state.apply(event)
		}
	}
	orderStatuses, err := s.loadBehaviorOrderStatuses(states)
	if err != nil {
		return nil, 0, err
	}
	users, err := s.loadBehaviorUsers(states)
	if err != nil {
		return nil, 0, err
	}
	couponCodes, err := s.loadBehaviorCouponCodes(collectBehaviorCouponIDs(states))
	if err != nil {
		return nil, 0, err
	}

	result := make([]BehaviorSessionSummary, 0, len(rows))
	for _, sessionID := range sessionIDs {
		state := states[sessionID]
		result = append(result, buildBehaviorSessionSummary(state, orderStatuses, users, couponCodes, time.Now()))
	}
	return result, total, nil
}

// GetSession 获取单个会话完整时间线。
func (s *BehaviorAnalyticsService) GetSession(sessionID string) (*BehaviorSessionDetail, error) {
	normalized := normalizeBehaviorIdentifier(sessionID)
	if normalized == "" || s == nil || s.repo == nil {
		return nil, ErrBehaviorSessionNotFound
	}
	events, err := s.repo.ListBySessionID(normalized)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrBehaviorSessionNotFound
	}
	states := buildBehaviorStates(events)
	state := states[normalized]
	orderStatuses, err := s.loadBehaviorOrderStatuses(states)
	if err != nil {
		return nil, err
	}
	users, err := s.loadBehaviorUsers(states)
	if err != nil {
		return nil, err
	}
	couponCodes, err := s.loadBehaviorCouponCodes(collectBehaviorCouponIDs(states))
	if err != nil {
		return nil, err
	}

	timeline := make([]BehaviorSessionEvent, 0, len(events))
	for _, event := range events {
		code := event.CouponCode
		if code == "" && event.CouponID > 0 {
			code = couponCodes[event.CouponID]
		}
		timeline = append(timeline, BehaviorSessionEvent{
			ID:              event.ID,
			EventName:       event.EventName,
			PagePath:        event.PagePath,
			PageURL:         event.PageURL,
			Referrer:        event.Referrer,
			ElementKey:      event.ElementKey,
			ElementText:     event.ElementText,
			ElementSelector: event.ElementSelector,
			ProductID:       event.ProductID,
			SKUID:           event.SKUID,
			OrderNo:         event.OrderNo,
			PaymentID:       event.PaymentID,
			CouponID:        event.CouponID,
			CouponCode:      code,
			Reason:          event.Reason,
			DurationMS:      event.DurationMS,
			ScrollDepth:     event.ScrollDepth,
			Properties:      event.Properties,
			OccurredAt:      event.OccurredAt,
		})
	}

	return &BehaviorSessionDetail{
		Summary: buildBehaviorSessionSummary(state, orderStatuses, users, couponCodes, time.Now()),
		Events:  timeline,
	}, nil
}

func buildBehaviorFunnel(allSessions, productViews, intents, checkouts, orders, paid int64) []BehaviorFunnelStep {
	values := []struct {
		key   string
		value int64
	}{
		{key: "sessions", value: allSessions},
		{key: "product_view", value: productViews},
		{key: "purchase_intent", value: intents},
		{key: "checkout", value: checkouts},
		{key: "order_created", value: orders},
		{key: "paid", value: paid},
	}
	result := make([]BehaviorFunnelStep, 0, len(values))
	for index, item := range values {
		conversion := "100.00"
		if index > 0 {
			conversion = behaviorPercent(item.value, values[index-1].value)
		}
		result = append(result, BehaviorFunnelStep{
			Key:                 item.key,
			Sessions:            item.value,
			ConversionFromPrior: conversion,
		})
	}
	return result
}

func buildBehaviorDropoffs(counts map[string]int64) []BehaviorDropoffInsight {
	var total int64
	for _, count := range counts {
		total += count
	}
	result := make([]BehaviorDropoffInsight, 0, len(counts))
	for reason, count := range counts {
		result = append(result, BehaviorDropoffInsight{
			Reason:   reason,
			Sessions: count,
			Share:    behaviorPercent(count, total),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Sessions == result[j].Sessions {
			return result[i].Reason < result[j].Reason
		}
		return result[i].Sessions > result[j].Sessions
	})
	if len(result) > 20 {
		result = result[:20]
	}
	return result
}

func behaviorPercent(numerator, denominator int64) string {
	if denominator <= 0 {
		return "0.00"
	}
	return formatPercentValue(float64(numerator) / float64(denominator) * 100)
}

func buildBehaviorSessionSummary(
	state *behaviorSessionState,
	orderStatuses map[string]string,
	users map[uint]models.User,
	couponCodes map[uint]string,
	now time.Time,
) BehaviorSessionSummary {
	if state == nil {
		return BehaviorSessionSummary{ProductIDs: []uint{}, CouponCodes: []string{}, OrderNos: []string{}, OrderStatuses: map[string]string{}}
	}
	stage, reason, paid, _ := behaviorSessionClassification(state, orderStatuses, now)
	durationMS := state.DurationMS
	if !state.StartedAt.IsZero() && !state.LastAt.IsZero() {
		elapsed := state.LastAt.Sub(state.StartedAt).Milliseconds()
		if elapsed > durationMS {
			durationMS = elapsed
		}
	}

	productIDs := mapKeysUintSet(state.ProductIDs)
	orderNos := mapKeysStringSet(state.OrderNos)
	codesSet := make(map[string]struct{}, len(state.CouponCodes)+len(state.CouponIDs))
	for code := range state.CouponCodes {
		codesSet[code] = struct{}{}
	}
	for couponID := range state.CouponIDs {
		if code := strings.TrimSpace(couponCodes[couponID]); code != "" {
			codesSet[code] = struct{}{}
		}
	}
	sessionStatuses := make(map[string]string)
	for _, orderNo := range orderNos {
		if status := strings.TrimSpace(orderStatuses[orderNo]); status != "" {
			sessionStatuses[orderNo] = status
		}
	}

	var user *BehaviorSessionUser
	if state.UserID > 0 {
		if found, ok := users[state.UserID]; ok {
			user = &BehaviorSessionUser{ID: found.ID, Email: found.Email, DisplayName: found.DisplayName}
		} else {
			user = &BehaviorSessionUser{ID: state.UserID}
		}
	}

	return BehaviorSessionSummary{
		SessionID:      state.SessionID,
		VisitorID:      state.VisitorID,
		User:           user,
		GuestEmail:     state.GuestEmail,
		ClientIP:       state.ClientIP,
		UserAgent:      state.UserAgent,
		StartedAt:      state.StartedAt,
		LastAt:         state.LastAt,
		DurationMS:     durationMS,
		EventCount:     state.EventCount,
		LastPage:       state.LastPage,
		MaxScrollDepth: state.MaxScrollDepth,
		Stage:          stage,
		DropoffReason:  reason,
		IsPaid:         paid,
		ProductIDs:     productIDs,
		CouponCodes:    mapKeysStringSet(codesSet),
		OrderNos:       orderNos,
		OrderStatuses:  sessionStatuses,
	}
}

func (s *BehaviorAnalyticsService) loadBehaviorOrderStatuses(states map[string]*behaviorSessionState) (map[string]string, error) {
	result := make(map[string]string)
	if s == nil || s.orderRepo == nil {
		return result, nil
	}
	set := make(map[string]struct{})
	for _, state := range states {
		for orderNo := range state.OrderNos {
			set[orderNo] = struct{}{}
		}
	}
	orderNos := mapKeysStringSet(set)
	orders, err := s.orderRepo.GetByOrderNos(orderNos)
	if err != nil {
		return nil, err
	}
	for _, order := range orders {
		result[order.OrderNo] = order.Status
	}
	return result, nil
}

func (s *BehaviorAnalyticsService) loadBehaviorUsers(states map[string]*behaviorSessionState) (map[uint]models.User, error) {
	result := make(map[uint]models.User)
	if s == nil || s.userRepo == nil {
		return result, nil
	}
	set := make(map[uint]struct{})
	for _, state := range states {
		if state.UserID > 0 {
			set[state.UserID] = struct{}{}
		}
	}
	users, err := s.userRepo.ListByIDs(mapKeysUintSet(set))
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		result[user.ID] = user
	}
	return result, nil
}

func (s *BehaviorAnalyticsService) loadBehaviorCouponCodes(ids []uint) (map[uint]string, error) {
	result := make(map[uint]string)
	if s == nil || s.couponRepo == nil || len(ids) == 0 {
		return result, nil
	}
	coupons, err := s.couponRepo.ListByIDs(ids)
	if err != nil {
		return nil, err
	}
	for _, coupon := range coupons {
		result[coupon.ID] = coupon.Code
	}
	return result, nil
}

func (s *BehaviorAnalyticsService) loadBehaviorProductTitles(ids []uint) (map[uint]models.JSON, error) {
	result := make(map[uint]models.JSON)
	if s == nil || s.productRepo == nil || len(ids) == 0 {
		return result, nil
	}
	products, err := s.productRepo.ListByIDs(ids)
	if err != nil {
		return nil, err
	}
	for _, product := range products {
		result[product.ID] = product.TitleJSON
	}
	return result, nil
}

func collectBehaviorCouponIDs(states map[string]*behaviorSessionState) []uint {
	set := make(map[uint]struct{})
	for _, state := range states {
		for id := range state.CouponIDs {
			set[id] = struct{}{}
		}
	}
	return mapKeysUintSet(set)
}

func mapKeysUintCoupon(values map[uint]*behaviorCouponAggregate) []uint {
	set := make(map[uint]struct{}, len(values))
	for id := range values {
		set[id] = struct{}{}
	}
	return mapKeysUintSet(set)
}

func mapKeysUintProduct(values map[uint]*behaviorProductAggregate) []uint {
	set := make(map[uint]struct{}, len(values))
	for id := range values {
		set[id] = struct{}{}
	}
	return mapKeysUintSet(set)
}

func mapKeysUintSet(values map[uint]struct{}) []uint {
	result := make([]uint, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func mapKeysStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
