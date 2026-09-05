package config

import (
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 配置处理器
type Handler struct {
	service Service
}

// NewHandler 创建配置处理器
func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

// GetCurrentTerm 获取当前学期（公开接口）
// @Summary 获取当前学期
// @Tags 配置管理
// @Produce json
// @Success 200 {object} common.Response{data=CurrentTermResponse}
// @Router /api/config/term [get]
func (h *Handler) GetCurrentTerm(c *gin.Context) {
	term, err := h.service.GetCurrentTerm(c.Request.Context())
	if err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	common.Success(c, CurrentTermResponse{
		Term: term,
	})
}

// SetCurrentTerm 设置当前学期（管理员接口）
// @Summary 设置当前学期
// @Tags 配置管理
// @Accept json
// @Produce json
// @Param request body SetCurrentTermRequest true "学期信息"
// @Success 200 {object} common.Response
// @Router /api/admin/config/term [post]
func (h *Handler) SetCurrentTerm(c *gin.Context) {
	var req SetCurrentTermRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "参数错误"))
		return
	}

	if err := h.service.SetCurrentTerm(c.Request.Context(), req.Term); err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	common.Success(c, nil)
}

// GetSemesterDates 获取学期日期（公开接口）
// @Summary 获取学期日期
// @Tags 配置管理
// @Produce json
// @Param term query string false "学期（格式：2024-2025-1）,不传则返回当前学期"
// @Success 200 {object} common.Response{data=SemesterDatesResponse}
// @Router /api/config/semester-dates [get]
func (h *Handler) GetSemesterDates(c *gin.Context) {
	term := c.Query("term")

	// 如果没有传入term参数，使用当前学期
	if term == "" {
		currentTerm, err := h.service.GetCurrentTerm(c.Request.Context())
		if err != nil {
			common.ErrorWithAppError(c, err.(*common.AppError))
			return
		}
		term = currentTerm
	}

	startDate, endDate, err := h.service.GetSemesterDates(c.Request.Context(), term)
	if err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	common.Success(c, SemesterDatesResponse{
		Term:      term,
		StartDate: startDate,
		EndDate:   endDate,
	})
}

// SetSemesterDates 设置学期日期（管理员接口）
// @Summary 设置学期日期
// @Tags 配置管理
// @Accept json
// @Produce json
// @Param request body SetSemesterDatesRequest true "学期日期信息"
// @Success 200 {object} common.Response
// @Router /api/admin/config/semester-dates [post]
func (h *Handler) SetSemesterDates(c *gin.Context) {
	var req SetSemesterDatesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorWithAppError(c, common.NewAppError(common.CodeInvalidParams, "参数错误"))
		return
	}

	if err := h.service.SetSemesterDates(c.Request.Context(), req.Term, req.StartDate, req.EndDate); err != nil {
		common.ErrorWithAppError(c, err.(*common.AppError))
		return
	}

	common.Success(c, nil)
}
