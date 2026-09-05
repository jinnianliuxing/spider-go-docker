package grade

import (
	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// Handler 成绩HTTP处理器
type Handler struct {
	service Service
}

// NewHandler 创建成绩处理器
func NewHandler(service Service) *Handler {
	return &Handler{
		service: service,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	grades := r.Group("/grades")
	{
		grades.GET("", h.GetGrades)                   // 获取成绩（可选term参数）
		grades.GET("/level", h.GetLevelGrades)        // 获取等级考试成绩
		grades.GET("/analysis", h.GetGradesAnalysis)  // 获取成绩分析
		grades.POST("/regular", h.GetRegularScore)    // 获取平时分
		grades.GET("/student-info", h.GetStudentInfo) // 获取学生信息（年级、学院、专业、班级）
	}
}

// GetGrades 获取成绩
// @Summary 获取成绩
// @Tags Grade
// @Produce JSON
// @Param term query string false "学期，格式：2024-2025-1"
// @Param year query string false "学年，格式：2024-2025"
// @Success 200 {object} GradesResponse
// @Router /grades [get]
func (h *Handler) GetGrades(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	// 从 query params 获取学期或学年参数
	term := c.Query("term")
	year := c.Query("year")

	var grades []Grade
	var gpa *GPA
	var err error

	// 优先级：year > term > all
	if year != "" {
		// 学年查询
		grades, gpa, err = h.service.GetGradesByYear(c.Request.Context(), uid.(int), year)
	} else if term != "" {
		// 学期查询
		grades, gpa, err = h.service.GetGradesByTerm(c.Request.Context(), uid.(int), term)
	} else {
		// 查询所有成绩
		grades, gpa, err = h.service.GetAllGrades(c.Request.Context(), uid.(int))
	}

	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取成绩失败")
		}
		return
	}

	common.Success(c, gin.H{
		"grades": grades,
		"gpa":    gpa,
	})
}

// GetLevelGrades 获取等级考试成绩
// @Summary 获取等级考试成绩
// @Tags Grade
// @Produce json
// @Success 200 {array} LevelGrade
// @Router /grades/level [get]
func (h *Handler) GetLevelGrades(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	grades, err := h.service.GetLevelGrades(c.Request.Context(), uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取等级考试成绩失败")
		}
		return
	}

	common.Success(c, grades)
}

// GetGradesAnalysis 获取成绩分析
// @Summary 获取最近三个学期的成绩分析
// @Tags Grade
// @Produce JSON
// @Success 200 {object} TermsGradesAnalysis
// @Router /grades/analysis [get]
func (h *Handler) GetGradesAnalysis(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	analysis, err := h.service.GetRecentTermsGrades(c.Request.Context(), uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取成绩分析失败")
		}
		return
	}

	common.Success(c, analysis)
}

// GetRegularScore 获取平时分
// @Summary 获取平时分
// @Tags Grade
// @Accept json
// @Produce json
// @Param request body GetRegularGradesRequest true "获取平时分请求"
// @Success 200 {object} RegularGrade
// @Router /grades/regular [post]
func (h *Handler) GetRegularScore(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	// 绑定 JSON 请求体
	var req GetRegularGradesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, "请求参数错误")
		return
	}

	// 参数校验
	if req.Term == "" {
		common.Error(c, common.CodeInvalidParams, "学期参数不能为空")
		return
	}

	if req.Code == "" {
		common.Error(c, common.CodeInvalidParams, "课程编号不能为空")
		return
	}

	// 调用 service 获取平时分
	regularGrade, err := h.service.GetRegularGrades(c.Request.Context(), uid.(int), req.Term, req.Code)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取平时分失败")
		}
		return
	}

	common.Success(c, regularGrade)
}

// GetStudentInfo 获取学生详细信息
// @Summary 获取学生详细信息（年级、学院、专业、班级）
// @Tags Grade
// @Produce json
// @Success 200 {object} UserDetailedInfo
// @Router /grades/student-info [get]
func (h *Handler) GetStudentInfo(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	info, err := h.service.GetUserGradeMajorClass(c.Request.Context(), uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取学生信息失败")
		}
		return
	}

	common.Success(c, info)
}
