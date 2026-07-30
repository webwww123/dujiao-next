package models

import "time"

// BehaviorEvent 前台用户行为事件。
//
// 保存漏斗诊断与单次会话还原所需的结构化字段。密码、Token、支付凭证、
// 卡密及手工交付表单原文不进入本表。
type BehaviorEvent struct {
	ID              uint      `gorm:"primarykey" json:"id"`
	EventID         string    `gorm:"size:64;uniqueIndex;not null" json:"event_id"`
	VisitorID       string    `gorm:"size:64;index;not null" json:"visitor_id"`
	SessionID       string    `gorm:"size:64;index;not null" json:"session_id"`
	UserID          uint      `gorm:"index;not null;default:0" json:"user_id,omitempty"`
	GuestEmail      string    `gorm:"size:320;index" json:"guest_email,omitempty"`
	ClientIP        string    `gorm:"size:64;index" json:"client_ip,omitempty"`
	UserAgent       string    `gorm:"size:1000" json:"user_agent,omitempty"`
	EventName       string    `gorm:"size:64;index;not null" json:"event_name"`
	PagePath        string    `gorm:"size:300;index" json:"page_path,omitempty"`
	PageURL         string    `gorm:"size:1000" json:"page_url,omitempty"`
	Referrer        string    `gorm:"size:1000" json:"referrer,omitempty"`
	ElementKey      string    `gorm:"size:160;index" json:"element_key,omitempty"`
	ElementText     string    `gorm:"size:500" json:"element_text,omitempty"`
	ElementSelector string    `gorm:"size:500" json:"element_selector,omitempty"`
	ProductID       uint      `gorm:"index;not null;default:0" json:"product_id,omitempty"`
	SKUID           uint      `gorm:"index;not null;default:0" json:"sku_id,omitempty"`
	OrderNo         string    `gorm:"size:64;index" json:"order_no,omitempty"`
	PaymentID       uint      `gorm:"index;not null;default:0" json:"payment_id,omitempty"`
	CouponID        uint      `gorm:"index;not null;default:0" json:"coupon_id,omitempty"`
	CouponCode      string    `gorm:"size:160;index" json:"coupon_code,omitempty"`
	Reason          string    `gorm:"size:500;index" json:"reason,omitempty"`
	DurationMS      int64     `gorm:"not null;default:0" json:"duration_ms,omitempty"`
	ScrollDepth     int       `gorm:"not null;default:0" json:"scroll_depth,omitempty"`
	Properties      JSON      `gorm:"type:json" json:"properties,omitempty"`
	OccurredAt      time.Time `gorm:"index;not null" json:"occurred_at"`
	CreatedAt       time.Time `gorm:"index;not null" json:"created_at"`
}

// TableName 指定表名。
func (BehaviorEvent) TableName() string {
	return "behavior_events"
}
