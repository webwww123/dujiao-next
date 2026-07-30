package repository

import (
	"errors"
	"strings"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/models"

	"gorm.io/gorm"
)

// OrderRepository 订单数据访问接口
type OrderRepository interface {
	Create(order *models.Order, items []models.OrderItem) error
	GetByID(id uint) (*models.Order, error)
	GetByIDs(ids []uint) ([]models.Order, error)
	GetByOrderNos(orderNos []string) ([]models.Order, error)
	ResolveReceiverEmailByOrderID(orderID uint) (string, error)
	GetByIDAndUser(id uint, userID uint) (*models.Order, error)
	GetByOrderNoAndUser(orderNo string, userID uint) (*models.Order, error)
	GetAnyByOrderNoAndUser(orderNo string, userID uint) (*models.Order, error)
	GetByIDAndGuest(id uint, email, password string) (*models.Order, error)
	GetByOrderNoAndGuest(orderNo, email, password string) (*models.Order, error)
	GetAnyByOrderNoAndGuest(orderNo, email, password string) (*models.Order, error)
	ListChildren(parentID uint) ([]models.Order, error)
	ListByUser(filter OrderListFilter) ([]models.Order, int64, error)
	ListByGuest(email, password string, page, pageSize int) ([]models.Order, int64, error)
	ListAdmin(filter OrderListFilter) ([]models.Order, int64, error)
	UpdateStatus(id uint, status string, updates map[string]interface{}) error
	CountOrderItemsByProduct(productID uint) (int64, error)
	CountPendingByUserID(userID uint) (int64, error)
	CountPendingByClientIP(clientIP string) (int64, error)
	CountPendingByGuestEmail(email string) (int64, error)
	Transaction(fn func(tx *gorm.DB) error) error
	WithTx(tx *gorm.DB) *GormOrderRepository
}

// GormOrderRepository GORM 实现
type GormOrderRepository struct {
	BaseRepository
}

// NewOrderRepository 创建订单仓库
func NewOrderRepository(db *gorm.DB) *GormOrderRepository {
	return &GormOrderRepository{BaseRepository: BaseRepository{db: db}}
}

// WithTx 绑定事务
func (r *GormOrderRepository) WithTx(tx *gorm.DB) *GormOrderRepository {
	if tx == nil {
		return r
	}
	return &GormOrderRepository{BaseRepository: BaseRepository{db: tx}}
}

func (r *GormOrderRepository) withChildren(query *gorm.DB) *gorm.DB {
	return query.Preload("Children").Preload("Children.Items").Preload("Children.Fulfillment")
}

// Create 创建订单与订单项
func (r *GormOrderRepository) Create(order *models.Order, items []models.OrderItem) error {
	if err := r.db.Create(order).Error; err != nil {
		return err
	}
	for i := range items {
		items[i].OrderID = order.ID
	}
	if len(items) > 0 {
		if err := r.db.Create(&items).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetByID 根据 ID 获取订单
func (r *GormOrderRepository) GetByID(id uint) (*models.Order, error) {
	var order models.Order
	query := r.withChildren(r.db.Preload("Items").Preload("Fulfillment"))
	if err := query.First(&order, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

// GetByIDs 根据 ID 列表批量获取订单
func (r *GormOrderRepository) GetByIDs(ids []uint) ([]models.Order, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var orders []models.Order
	if err := r.db.Where("id IN ?", ids).Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

// GetByOrderNos 根据订单号批量获取父订单，用于行为分析关联最终支付状态。
func (r *GormOrderRepository) GetByOrderNos(orderNos []string) ([]models.Order, error) {
	if len(orderNos) == 0 {
		return []models.Order{}, nil
	}
	var orders []models.Order
	if err := r.db.
		Where("parent_id IS NULL AND order_no IN ?", orderNos).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

// ResolveReceiverEmailByOrderID 根据订单 ID 解析状态通知的收件邮箱。
func (r *GormOrderRepository) ResolveReceiverEmailByOrderID(orderID uint) (string, error) {
	if orderID == 0 {
		return "", nil
	}

	var orderRow struct {
		UserID     uint
		GuestEmail string
	}
	if err := r.db.Model(&models.Order{}).
		Select("user_id", "guest_email").
		Where("id = ?", orderID).
		Take(&orderRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if orderRow.UserID == 0 {
		return strings.TrimSpace(orderRow.GuestEmail), nil
	}

	var userRow struct {
		Email string
	}
	if err := r.db.Model(&models.User{}).
		Select("email").
		Where("id = ?", orderRow.UserID).
		Take(&userRow).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(userRow.Email), nil
}

// GetByIDAndUser 获取用户订单详情
func (r *GormOrderRepository) GetByIDAndUser(id uint, userID uint) (*models.Order, error) {
	var order models.Order
	query := r.withChildren(r.db.Preload("Items").Preload("Fulfillment"))
	if err := query.Where("id = ? AND user_id = ? AND parent_id IS NULL", id, userID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}
func (r *GormOrderRepository) GetByOrderNoAndUser(orderNo string, userID uint) (*models.Order, error) {
	var order models.Order
	query := r.withChildren(r.db.Preload("Items").Preload("Fulfillment"))
	if err := query.Where("order_no = ? AND user_id = ? AND parent_id IS NULL", orderNo, userID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

// GetAnyByOrderNoAndUser 按订单号查找订单（不限父/子），用于交付下载等场景
func (r *GormOrderRepository) GetAnyByOrderNoAndUser(orderNo string, userID uint) (*models.Order, error) {
	var order models.Order
	query := r.db.Preload("Items").Preload("Fulfillment").Preload("Children", func(db *gorm.DB) *gorm.DB {
		return db.Preload("Items").Preload("Fulfillment")
	})
	if err := query.Where("order_no = ? AND user_id = ?", orderNo, userID).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

// GetAnyByOrderNoAndGuest 按订单号查找游客订单（不限父/子），用于交付下载等场景
func (r *GormOrderRepository) GetAnyByOrderNoAndGuest(orderNo, email, password string) (*models.Order, error) {
	var order models.Order
	query := r.db.Preload("Items").Preload("Fulfillment").Preload("Children", func(db *gorm.DB) *gorm.DB {
		return db.Preload("Items").Preload("Fulfillment")
	})
	if err := query.Where("order_no = ? AND user_id = 0 AND guest_email = ? AND guest_password = ?", orderNo, email, password).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

// GetByIDAndGuest 获取游客订单详情
func (r *GormOrderRepository) GetByIDAndGuest(id uint, email, password string) (*models.Order, error) {
	var order models.Order
	query := r.withChildren(r.db.Preload("Items").Preload("Fulfillment"))
	if err := query.
		Where("id = ? AND user_id = 0 AND guest_email = ? AND guest_password = ? AND parent_id IS NULL", id, email, password).
		First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

// GetByOrderNoAndGuest 获取游客订单详情（按订单号）
func (r *GormOrderRepository) GetByOrderNoAndGuest(orderNo, email, password string) (*models.Order, error) {
	var order models.Order
	query := r.withChildren(r.db.Preload("Items").Preload("Fulfillment"))
	if err := query.
		Where("order_no = ? AND user_id = 0 AND guest_email = ? AND guest_password = ? AND parent_id IS NULL", orderNo, email, password).
		First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &order, nil
}

// ListChildren 获取子订单列表
func (r *GormOrderRepository) ListChildren(parentID uint) ([]models.Order, error) {
	var orders []models.Order
	if parentID == 0 {
		return orders, nil
	}
	if err := r.db.Preload("Items").Preload("Fulfillment").
		Where("parent_id = ?", parentID).
		Order("id asc").
		Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

// ListAdmin 管理端订单列表
func (r *GormOrderRepository) ListAdmin(filter OrderListFilter) ([]models.Order, int64, error) {
	var orders []models.Order
	query := r.db.Model(&models.Order{}).Where("parent_id IS NULL")

	if filter.UserID != 0 {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if keyword := strings.TrimSpace(filter.UserKeyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(
			"user_id IN ("+
				"SELECT users.id FROM users "+
				"WHERE users.deleted_at IS NULL AND ("+
				"users.email LIKE ? OR "+
				"users.display_name LIKE ? OR "+
				"EXISTS ("+
				"SELECT 1 FROM user_oauth_identities "+
				"WHERE user_oauth_identities.user_id = users.id AND ("+
				"user_oauth_identities.provider LIKE ? OR "+
				"user_oauth_identities.provider_user_id LIKE ? OR "+
				"user_oauth_identities.username LIKE ?"+
				")"+
				")"+
				")"+
				")",
			like, like, like, like, like,
		)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.OrderNo != "" {
		query = query.Where("order_no = ?", filter.OrderNo)
	}
	if filter.GuestEmail != "" {
		query = query.Where("guest_email = ?", filter.GuestEmail)
	}
	if keyword := strings.TrimSpace(filter.ProductKeyword); keyword != "" {
		like := "%" + keyword + "%"
		cond1, argCount1 := buildLocalizedLikeCondition(r.db, nil, []string{"oi.title_json"})
		cond2, argCount2 := buildLocalizedLikeCondition(r.db, nil, []string{"oi2.title_json"})
		if cond1 != "" {
			args1 := repeatLikeArgs(like, argCount1)
			args2 := repeatLikeArgs(like, argCount2)
			query = query.Where(
				"id IN (SELECT DISTINCT oi.order_id FROM order_items oi WHERE oi.order_id IN (SELECT o2.id FROM orders o2 WHERE o2.parent_id IS NULL) AND ("+cond1+")) "+
					"OR id IN (SELECT DISTINCT o3.parent_id FROM orders o3 WHERE o3.parent_id IS NOT NULL AND o3.id IN (SELECT DISTINCT oi2.order_id FROM order_items oi2 WHERE "+cond2+"))",
				append(args1, args2...)...,
			)
		}
	}
	if filter.CreatedFrom != nil {
		query = query.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where("created_at <= ?", *filter.CreatedTo)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	dataQuery := applyPagination(query.Session(&gorm.Session{}), filter.Page, filter.PageSize)
	dataQuery = r.withChildren(dataQuery.Preload("Items").Preload("Fulfillment"))

	orderClause := resolveAdminOrderSort(filter.SortBy, filter.SortOrder)
	if err := dataQuery.Order(orderClause).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// resolveAdminOrderSort 解析排序参数，返回安全的 ORDER BY 子句。
func resolveAdminOrderSort(sortBy, sortOrder string) string {
	allowedColumns := map[string]bool{
		"created_at":   true,
		"updated_at":   true,
		"total_amount": true,
	}
	direction := "desc"
	if strings.ToLower(strings.TrimSpace(sortOrder)) == "asc" {
		direction = "asc"
	}
	col := strings.ToLower(strings.TrimSpace(sortBy))
	if !allowedColumns[col] {
		return "id " + direction
	}
	return col + " " + direction
}

// UpdateStatus 更新订单状态
func (r *GormOrderRepository) UpdateStatus(id uint, status string, updates map[string]interface{}) error {
	if updates == nil {
		updates = map[string]interface{}{}
	}
	updates["status"] = status
	return r.db.Model(&models.Order{}).Where("id = ?", id).Updates(updates).Error
}

// ListByUser 获取用户订单列表
func (r *GormOrderRepository) ListByUser(filter OrderListFilter) ([]models.Order, int64, error) {
	var orders []models.Order
	query := r.db.Model(&models.Order{}).Where("user_id = ? AND parent_id IS NULL", filter.UserID)

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.OrderNo != "" {
		query = query.Where("order_no LIKE ?", "%"+filter.OrderNo+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = applyPagination(query, filter.Page, filter.PageSize)

	query = r.withChildren(query.Preload("Items").Preload("Fulfillment"))
	if err := query.Order("id desc").Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// ListByGuest 获取游客订单列表
func (r *GormOrderRepository) ListByGuest(email, password string, page, pageSize int) ([]models.Order, int64, error) {
	var total int64
	if err := r.db.Model(&models.Order{}).
		Where("user_id = 0 AND guest_email = ? AND guest_password = ? AND parent_id IS NULL", email, password).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var orders []models.Order
	query := r.withChildren(r.db.Preload("Items").Preload("Fulfillment"))
	if err := query.
		Where("user_id = 0 AND guest_email = ? AND guest_password = ? AND parent_id IS NULL", email, password).
		Order("id desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	return orders, total, nil
}

// CountPendingByUserID 统计用户待支付的父订单数量
func (r *GormOrderRepository) CountPendingByUserID(userID uint) (int64, error) {
	if userID == 0 {
		return 0, nil
	}
	var count int64
	if err := r.db.Model(&models.Order{}).
		Where("user_id = ? AND status = ? AND parent_id IS NULL", userID, constants.OrderStatusPendingPayment).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountPendingByClientIP 统计某 IP 待支付的父订单数量
func (r *GormOrderRepository) CountPendingByClientIP(clientIP string) (int64, error) {
	if clientIP == "" {
		return 0, nil
	}
	var count int64
	if err := r.db.Model(&models.Order{}).
		Where("client_ip = ? AND status = ? AND parent_id IS NULL", clientIP, constants.OrderStatusPendingPayment).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountPendingByGuestEmail 统计游客邮箱待支付的父订单数量
func (r *GormOrderRepository) CountPendingByGuestEmail(email string) (int64, error) {
	if email == "" {
		return 0, nil
	}
	var count int64
	if err := r.db.Model(&models.Order{}).
		Where("guest_email = ? AND status = ? AND parent_id IS NULL", email, constants.OrderStatusPendingPayment).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountOrderItemsByProduct 统计商品关联的订单项数量
func (r *GormOrderRepository) CountOrderItemsByProduct(productID uint) (int64, error) {
	if productID == 0 {
		return 0, errors.New("invalid product id")
	}
	var count int64
	if err := r.db.Model(&models.OrderItem{}).Where("product_id = ?", productID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
