package ranking

import (
	"errors"
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handler 排名处理器
type Handler struct {
	service Service
}

// NewHandler 创建处理器实例
func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	// 用户端路由（需要认证）- 只能查看自己的排名
	r.GET("/ranking/my", h.GetMyRanking)
}

// @Summary 获取我的排名
// @Description 获取当前用户的GPA排名信息（只能查看自己在学院和专业的排名）
// @Tags Ranking
// @Accept json
// @Produce json
// @Param statistics_type query string false "统计类型: cumulative(累计), semester(学期), year(学年)"
// @Param statistics_term query string false "统计学期/学年(如2024-2025-1或2024-2025)"
// @Success 200 {object} MyRankingResponse
// @Router /ranking/my [get]
func (h *Handler) GetMyRanking(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeUnauthorized, "未授权"))
		return
	}

	var req GetRankingRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "参数错误"))
		return
	}

	// 默认值：累计排名
	if req.StatisticsType == "" {
		req.StatisticsType = StatisticsTypeCumulative
	}

	// 累计排名时，统计学期固定为 "all"
	if req.StatisticsType == StatisticsTypeCumulative {
		req.StatisticsTerm = "all"
	}

	// 验证学期/学年统计时必须提供 statistics_term
	if (req.StatisticsType == StatisticsTypeSemester || req.StatisticsType == "year") && req.StatisticsTerm == "" {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "学期/学年统计时必须提供 statistics_term"))
		return
	}

	resp, err := h.service.GetMyRanking(c.Request.Context(), uid.(int), req.StatisticsType, req.StatisticsTerm)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ErrorWithAppError(c, common.NewAppError(common.CodeNotFound, "暂无排名数据，请先查询成绩后再查看排名"))
			return
		}
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInternalError, "获取排名失败"))
		return
	}

	common.Success(c, resp)
}
