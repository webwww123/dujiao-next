package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/repository"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRiskIPBlacklisted        = errors.New("risk: ip blacklisted")
	ErrRiskEmailBlacklisted     = errors.New("risk: email blacklisted")
	ErrRiskTooManyPendingOrders = errors.New("risk: too many pending orders")
	ErrRiskOrderRateLimited     = errors.New("risk: order rate limited")
)

// RiskRateLimitedError 携带 Retry-After 秒数的频率限制错误
type RiskRateLimitedError struct {
	RetryAfter int64
}

func (e *RiskRateLimitedError) Error() string {
	return ErrRiskOrderRateLimited.Error()
}

func (e *RiskRateLimitedError) Is(target error) bool {
	return target == ErrRiskOrderRateLimited
}

// GetRetryAfter 从错误中提取 RetryAfter 秒数，不存在则返回 0
func GetRetryAfter(err error) int64 {
	var rle *RiskRateLimitedError
	if errors.As(err, &rle) {
		return rle.RetryAfter
	}
	return 0
}

// RiskCheckInput 风控检查输入
type RiskCheckInput struct {
	UserID      uint
	Email       string
	GuestEmail  string
	ClientIP    string
	IsGuest     bool
	SkipIPCheck bool // 跳过 IP 维度检查（渠道/Bot 订单，因 ClientIP 为服务器 IP 会误杀）
}

// parsedIPBlacklist 缓存解析后的 IP 黑名单
type parsedIPBlacklist struct {
	exactIPs map[string]struct{}
	cidrs    []*net.IPNet
	hash     string
}

// OrderRiskControlService 订单风控服务
type OrderRiskControlService struct {
	settingService *SettingService
	orderRepo      repository.OrderRepository

	mu              sync.RWMutex
	cachedBlacklist *parsedIPBlacklist
}

// NewOrderRiskControlService 创建风控服务
func NewOrderRiskControlService(settingService *SettingService, orderRepo repository.OrderRepository) *OrderRiskControlService {
	return &OrderRiskControlService{
		settingService: settingService,
		orderRepo:      orderRepo,
	}
}

// CheckOrderAllowed 检查是否允许下单
func (s *OrderRiskControlService) CheckOrderAllowed(input RiskCheckInput) error {
	if s == nil || s.settingService == nil {
		return nil
	}

	cfg, err := s.settingService.GetOrderRiskControlConfig()
	if err != nil {
		logger.Warnw("risk_control_get_config_error", "error", err)
		return nil // 读取配置失败时放行，不阻塞正常业务
	}

	if !cfg.Enabled {
		return nil
	}

	// 1. IP 黑名单检查（跳过 IP 检查时不执行）
	if !input.SkipIPCheck && input.ClientIP != "" && len(cfg.IPBlacklist) > 0 {
		if s.isIPInBlacklist(input.ClientIP, cfg.IPBlacklist) {
			return ErrRiskIPBlacklisted
		}
	}

	// 2. 邮箱黑名单检查（游客和登录用户都执行）
	email := strings.TrimSpace(input.Email)
	if email == "" {
		email = strings.TrimSpace(input.GuestEmail)
	}
	if email != "" && len(cfg.EmailBlacklist) > 0 {
		normalizedEmail := strings.ToLower(email)
		for _, blocked := range cfg.EmailBlacklist {
			if normalizedEmail == blocked {
				return ErrRiskEmailBlacklisted
			}
		}
	}

	// 3. 并发待支付订单数检查
	if err := s.checkPendingOrderLimits(input, cfg); err != nil {
		return err
	}

	// 4. 下单频率检查
	if cfg.OrderRateLimit.Enabled {
		if err := s.checkOrderRateLimit(input, cfg.OrderRateLimit); err != nil {
			return err
		}
	}

	return nil
}

// checkPendingOrderLimits 检查并发待支付订单上限
func (s *OrderRiskControlService) checkPendingOrderLimits(input RiskCheckInput, cfg OrderRiskControlConfig) error {
	// 用户维度
	if input.UserID > 0 && cfg.MaxPendingOrdersPerUser > 0 {
		count, err := s.orderRepo.CountPendingByUserID(input.UserID)
		if err != nil {
			logger.Warnw("risk_control_count_pending_by_user_error", "user_id", input.UserID, "error", err)
		} else if count >= int64(cfg.MaxPendingOrdersPerUser) {
			return ErrRiskTooManyPendingOrders
		}
	}

	// IP 维度（跳过 IP 检查时不执行）
	if !input.SkipIPCheck && input.ClientIP != "" && cfg.MaxPendingOrdersPerIP > 0 {
		count, err := s.orderRepo.CountPendingByClientIP(input.ClientIP)
		if err != nil {
			logger.Warnw("risk_control_count_pending_by_ip_error", "ip", input.ClientIP, "error", err)
		} else if count >= int64(cfg.MaxPendingOrdersPerIP) {
			return ErrRiskTooManyPendingOrders
		}
	}

	// 游客邮箱维度
	if input.IsGuest && input.GuestEmail != "" && cfg.MaxPendingOrdersPerGuestEmail > 0 {
		count, err := s.orderRepo.CountPendingByGuestEmail(input.GuestEmail)
		if err != nil {
			logger.Warnw("risk_control_count_pending_by_email_error", "email", input.GuestEmail, "error", err)
		} else if count >= int64(cfg.MaxPendingOrdersPerGuestEmail) {
			return ErrRiskTooManyPendingOrders
		}
	}

	return nil
}

// orderRateLimitScript Redis Lua 脚本：固定窗口计数，返回 {current, ttl}
var orderRateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("EXPIRE", KEYS[1], ARGV[1])
end
if tonumber(ARGV[2]) > 0 and tonumber(ARGV[3]) > 0 and current == tonumber(ARGV[2]) + 1 then
	redis.call("EXPIRE", KEYS[1], ARGV[3])
end
local ttl = redis.call("TTL", KEYS[1])
return {current, ttl}
`)

// checkOrderRateLimit 检查下单频率
func (s *OrderRiskControlService) checkOrderRateLimit(input RiskCheckInput, rl OrderRateLimitConfig) error {
	client := cache.Client()
	if client == nil {
		return nil // Redis 不可用时放行
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// IP 维度频率限制（跳过 IP 检查时不执行）
	if !input.SkipIPCheck && input.ClientIP != "" {
		if err := s.checkSingleRateLimit(ctx, client,
			fmt.Sprintf("dj:risk:order_rate:ip:%s", input.ClientIP), rl); err != nil {
			return err
		}
	}

	// 用户维度频率限制（登录用户额外检查）
	if input.UserID > 0 {
		if err := s.checkSingleRateLimit(ctx, client,
			fmt.Sprintf("dj:risk:order_rate:user:%d", input.UserID), rl); err != nil {
			return err
		}
	}

	return nil
}

// checkSingleRateLimit 执行单个维度的频率限制检查
func (s *OrderRiskControlService) checkSingleRateLimit(ctx context.Context, client *redis.Client, key string, rl OrderRateLimitConfig) error {
	result, err := orderRateLimitScript.Run(ctx, client, []string{key},
		rl.WindowSeconds, rl.MaxRequests, rl.BlockSeconds,
	).Result()
	if err != nil {
		logger.Warnw("risk_control_rate_limit_script_error", "key", key, "error", err)
		return nil // 脚本执行失败时放行
	}

	values, ok := result.([]interface{})
	if !ok || len(values) < 2 {
		return nil
	}

	current, _ := values[0].(int64)
	ttl, _ := values[1].(int64)

	if current > int64(rl.MaxRequests) {
		if ttl < 0 {
			ttl = 0
		}
		return &RiskRateLimitedError{RetryAfter: ttl}
	}
	return nil
}

// getOrBuildBlacklist 获取或重建缓存的 IP 黑名单
func (s *OrderRiskControlService) getOrBuildBlacklist(blacklist []string) *parsedIPBlacklist {
	hash := hashBlacklist(blacklist)

	s.mu.RLock()
	if s.cachedBlacklist != nil && s.cachedBlacklist.hash == hash {
		cached := s.cachedBlacklist
		s.mu.RUnlock()
		return cached
	}
	s.mu.RUnlock()

	// 重建
	parsed := &parsedIPBlacklist{
		exactIPs: make(map[string]struct{}, len(blacklist)),
		hash:     hash,
	}
	for _, entry := range blacklist {
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err == nil {
				parsed.cidrs = append(parsed.cidrs, cidr)
			}
		} else {
			parsed.exactIPs[entry] = struct{}{}
		}
	}

	s.mu.Lock()
	if s.cachedBlacklist == nil || s.cachedBlacklist.hash != hash {
		s.cachedBlacklist = parsed
	}
	s.mu.Unlock()

	return parsed
}

// hashBlacklist 计算黑名单列表的哈希用于缓存失效判断
func hashBlacklist(list []string) string {
	h := sha256.New()
	for _, s := range list {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// isIPInBlacklist 检查 IP 是否在黑名单中（支持 CIDR，使用缓存）
func (s *OrderRiskControlService) isIPInBlacklist(clientIP string, blacklist []string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	parsed := s.getOrBuildBlacklist(blacklist)

	// 精确 IP 匹配（O(1) 哈希查找）
	if _, ok := parsed.exactIPs[clientIP]; ok {
		return true
	}

	// CIDR 匹配
	for _, cidr := range parsed.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}
