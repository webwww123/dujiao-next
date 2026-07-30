package repository

import (
	"time"

	"github.com/dujiao-next/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BehaviorEventRepository 用户行为事件数据访问接口。
type BehaviorEventRepository interface {
	CreateBatch(events []models.BehaviorEvent) error
	DeleteBefore(before time.Time) error
	ListRange(startAt, endAt time.Time) ([]models.BehaviorEvent, error)
	ListSessions(filter BehaviorSessionListFilter) ([]BehaviorSessionAggregate, int64, error)
	ListBySessionIDs(sessionIDs []string) ([]models.BehaviorEvent, error)
	ListBySessionID(sessionID string) ([]models.BehaviorEvent, error)
}

// BehaviorSessionListFilter 会话列表筛选。
type BehaviorSessionListFilter struct {
	StartAt  time.Time
	EndAt    time.Time
	Page     int
	PageSize int
}

// BehaviorSessionAggregate 会话分页所需的聚合字段。
type BehaviorSessionAggregate struct {
	SessionID  string `gorm:"column:session_id"`
	EventCount int64  `gorm:"column:event_count"`
}

// GormBehaviorEventRepository GORM 实现。
type GormBehaviorEventRepository struct {
	db *gorm.DB
}

// NewBehaviorEventRepository 创建行为事件仓库。
func NewBehaviorEventRepository(db *gorm.DB) *GormBehaviorEventRepository {
	return &GormBehaviorEventRepository{db: db}
}

// CreateBatch 批量写入事件；event_id 重复时忽略，避免页面卸载重试造成重复统计。
func (r *GormBehaviorEventRepository) CreateBatch(events []models.BehaviorEvent) error {
	if r == nil || r.db == nil || len(events) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_id"}},
		DoNothing: true,
	}).CreateInBatches(events, 50).Error
}

// DeleteBefore 删除超过保留期的历史事件。
func (r *GormBehaviorEventRepository) DeleteBefore(before time.Time) error {
	if r == nil || r.db == nil || before.IsZero() {
		return nil
	}
	return r.db.Where("occurred_at < ?", before).Delete(&models.BehaviorEvent{}).Error
}

// ListRange 获取时间范围内的事件。
func (r *GormBehaviorEventRepository) ListRange(startAt, endAt time.Time) ([]models.BehaviorEvent, error) {
	events := make([]models.BehaviorEvent, 0)
	if r == nil || r.db == nil {
		return events, nil
	}
	if err := r.db.
		Where("occurred_at >= ? AND occurred_at < ?", startAt, endAt).
		Order("occurred_at ASC, id ASC").
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// ListSessions 分页获取会话聚合。
func (r *GormBehaviorEventRepository) ListSessions(filter BehaviorSessionListFilter) ([]BehaviorSessionAggregate, int64, error) {
	rows := make([]BehaviorSessionAggregate, 0)
	if r == nil || r.db == nil {
		return rows, 0, nil
	}

	query := r.db.Model(&models.BehaviorEvent{}).
		Where("occurred_at >= ? AND occurred_at < ?", filter.StartAt, filter.EndAt)

	var total int64
	if err := query.Distinct("session_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	selectSQL := `
		session_id,
		COUNT(*) AS event_count
	`
	if err := query.
		Select(selectSQL).
		Group("session_id").
		Order("MAX(occurred_at) DESC").
		Limit(pageSize).
		Offset(offset).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// ListBySessionIDs 批量获取会话内事件。
func (r *GormBehaviorEventRepository) ListBySessionIDs(sessionIDs []string) ([]models.BehaviorEvent, error) {
	events := make([]models.BehaviorEvent, 0)
	if r == nil || r.db == nil || len(sessionIDs) == 0 {
		return events, nil
	}
	if err := r.db.
		Where("session_id IN ?", sessionIDs).
		Order("occurred_at ASC, id ASC").
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// ListBySessionID 获取单个会话的完整事件轨迹。
func (r *GormBehaviorEventRepository) ListBySessionID(sessionID string) ([]models.BehaviorEvent, error) {
	return r.ListBySessionIDs([]string{sessionID})
}
