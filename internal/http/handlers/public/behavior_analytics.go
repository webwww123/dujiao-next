package public

import (
	"time"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

type behaviorEventRequest struct {
	EventID         string      `json:"event_id"`
	VisitorID       string      `json:"visitor_id" binding:"required"`
	SessionID       string      `json:"session_id" binding:"required"`
	GuestEmail      string      `json:"guest_email"`
	EventName       string      `json:"event_name" binding:"required"`
	PagePath        string      `json:"page_path"`
	PageURL         string      `json:"page_url"`
	Referrer        string      `json:"referrer"`
	ElementKey      string      `json:"element_key"`
	ElementText     string      `json:"element_text"`
	ElementSelector string      `json:"element_selector"`
	ProductID       uint        `json:"product_id"`
	SKUID           uint        `json:"sku_id"`
	OrderNo         string      `json:"order_no"`
	PaymentID       uint        `json:"payment_id"`
	CouponID        uint        `json:"coupon_id"`
	CouponCode      string      `json:"coupon_code"`
	Reason          string      `json:"reason"`
	DurationMS      int64       `json:"duration_ms"`
	ScrollDepth     int         `json:"scroll_depth"`
	Properties      models.JSON `json:"properties"`
	OccurredAt      *time.Time  `json:"occurred_at"`
}

type behaviorEventBatchRequest struct {
	Events []behaviorEventRequest `json:"events" binding:"required,min=1,max=50,dive"`
}

// TrackBehaviorEvents 接收前台静默行为事件。
func (h *Handler) TrackBehaviorEvents(c *gin.Context) {
	var req behaviorEventBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	rows := make([]service.RecordBehaviorEventInput, 0, len(req.Events))
	for _, event := range req.Events {
		rows = append(rows, service.RecordBehaviorEventInput{
			EventID:         event.EventID,
			VisitorID:       event.VisitorID,
			SessionID:       event.SessionID,
			GuestEmail:      event.GuestEmail,
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
			CouponCode:      event.CouponCode,
			Reason:          event.Reason,
			DurationMS:      event.DurationMS,
			ScrollDepth:     event.ScrollDepth,
			Properties:      event.Properties,
			OccurredAt:      event.OccurredAt,
		})
	}

	accepted, err := h.BehaviorAnalyticsService.RecordBatch(service.RecordBehaviorBatchInput{
		UserID:    c.GetUint("user_id"),
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Events:    rows,
	})
	if err != nil {
		// 埋点失败不得影响购买流程；记录服务端错误并返回可忽略的成功响应。
		shared.RequestLog(c).Errorw("behavior_event_record_failed", "error", err)
		response.Success(c, gin.H{"accepted": 0})
		return
	}
	response.Success(c, gin.H{"accepted": accepted})
}
