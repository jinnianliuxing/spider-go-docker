package statistics

import (
	"spider-go/internal/common"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler 统计处理器
type Handler struct {
	service Service
}

// NewHandler 创建统计处理器
func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

// GetTodayDAU 获取今日DAU
// @Summary 获取今日DAU
// @Tags 统计
// @Produce JSON
// @Success 200 {object} common.Response{data=DAUResponse}
// @Router /api/admin/statistics/dau [get]
func (h *Handler) GetTodayDAU(c *gin.Context) {
	count, err := h.service.GetTodayDAU(c.Request.Context())
	if err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	today := time.Now().Format("2006-01-02")
	common.Success(c, DAUResponse{
		Date:  today,
		Count: count,
	})
}

// GetDAURange 获取DAU范围
// @Summary 获取指定日期范围的DAU
// @Tags 统计
// @Produce json
// @Param start_date query string true "开始日期（格式：2024-01-01）"
// @Param end_date query string true "结束日期（格式：2024-01-31）"
// @Success 200 {object} common.Response{data=DAURangeResponse}
// @Router /api/admin/statistics/dau/range [get]
func (h *Handler) GetDAURange(c *gin.Context) {
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")

	if startDateStr == "" || endDateStr == "" {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "开始日期和结束日期不能为空"))
		return
	}

	// 解析日期
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "开始日期格式错误，应为：2024-01-01"))
		return
	}

	endDate, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "结束日期格式错误，应为：2024-01-31"))
		return
	}

	// 获取数据
	data, err := h.service.GetDAURange(c.Request.Context(), startDate, endDate)
	if err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	// 转换为响应格式
	dayData := make([]DAUDayResponse, 0, len(data))
	for date, count := range data {
		dayData = append(dayData, DAUDayResponse{
			Date:  date,
			Count: count,
		})
	}

	common.Success(c, DAURangeResponse{
		StartDate: startDateStr,
		EndDate:   endDateStr,
		Data:      dayData,
	})
}

// GetUserCount 获取用户总数
// @Summary 获取用户总数
// @Tags 统计
// @Produce json
// @Success 200 {object} common.Response{data=UserCountResponse}
// @Router /api/admin/statistics/user/count [get]
func (h *Handler) GetUserCount(c *gin.Context) {
	count, err := h.service.GetUserCount(c.Request.Context())
	if err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	common.Success(c, UserCountResponse{
		TotalCount: count,
	})
}

// GetNewUserCount 获取新增用户数
// @Summary 获取新增用户数
// @Tags 统计
// @Produce json
// @Param date query string false "指定日期（格式：2024-01-01）"
// @Param start_date query string false "开始日期（格式：2024-01-01）"
// @Param end_date query string false "结束日期（格式：2024-01-31）"
// @Success 200 {object} common.Response{data=NewUserCountResponse}
// @Router /api/admin/statistics/user/new [get]
func (h *Handler) GetNewUserCount(c *gin.Context) {
	dateStr := c.Query("date")
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")

	var date *time.Time
	var startDate *time.Time
	var endDate *time.Time

	// 解析单日日期
	if dateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "日期格式错误，应为：2024-01-01"))
			return
		}
		date = &parsedDate
	}

	// 解析日期范围
	if startDateStr != "" && endDateStr != "" {
		parsedStartDate, err := time.Parse("2006-01-02", startDateStr)
		if err != nil {
			common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "开始日期格式错误，应为：2024-01-01"))
			return
		}

		parsedEndDate, err := time.Parse("2006-01-02", endDateStr)
		if err != nil {
			common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "结束日期格式错误，应为：2024-01-31"))
			return
		}

		startDate = &parsedStartDate
		endDate = &parsedEndDate
	}

	// 获取新增用户数
	count, err := h.service.GetNewUserCount(c.Request.Context(), date, startDate, endDate)
	if err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	// 构造响应
	response := NewUserCountResponse{
		TotalCount: count,
	}

	if startDate != nil && endDate != nil {
		response.StartDate = startDateStr
		response.EndDate = endDateStr
	} else if date != nil {
		response.Date = dateStr
	} else {
		response.Date = time.Now().Format("2006-01-02")
	}

	common.Success(c, response)
}
