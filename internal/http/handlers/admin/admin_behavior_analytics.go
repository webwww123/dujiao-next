package admin

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/service"

	"github.com/gin-gonic/gin"
)

// GetBehaviorAnalyticsOverview 获取用户行为漏斗总览。
func (h *Handler) GetBehaviorAnalyticsOverview(c *gin.Context) {
	input, err := parseDashboardQuery(c)
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	data, err := h.BehaviorAnalyticsService.GetOverview(input)
	if err != nil {
		if errors.Is(err, service.ErrDashboardRangeInvalid) {
			shared.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.dashboard_fetch_failed", err)
		return
	}
	response.Success(c, data)
}

// ListBehaviorAnalyticsSessions 获取用户行为会话摘要。
func (h *Handler) ListBehaviorAnalyticsSessions(c *gin.Context) {
	input, err := parseDashboardQuery(c)
	if err != nil {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", err)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	page, pageSize = shared.NormalizePagination(page, pageSize)

	rows, total, err := h.BehaviorAnalyticsService.ListSessions(input, page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrDashboardRangeInvalid) {
			shared.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.dashboard_fetch_failed", err)
		return
	}
	response.SuccessWithPage(c, rows, response.BuildPagination(page, pageSize, total))
}

// GetBehaviorAnalyticsSession 获取单个用户会话完整轨迹。
func (h *Handler) GetBehaviorAnalyticsSession(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("session_id"))
	data, err := h.BehaviorAnalyticsService.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, service.ErrBehaviorSessionNotFound) {
			shared.RespondError(c, response.CodeNotFound, "error.behavior_session_not_found", nil)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.dashboard_fetch_failed", err)
		return
	}
	response.Success(c, data)
}
