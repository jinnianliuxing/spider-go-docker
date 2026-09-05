package reconciliation

import (
	"spider-go/internal/common"
	"spider-go/internal/modules/user"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Handler 对账HTTP处理器
type Handler struct {
	service  Service
	repo     Repository      // 用于管理员直接操作数据库（如 OPTIMIZE TABLE）
	userRepo user.Repository // 用于管理员获取已绑定用户列表
}

// NewHandler 创建对账处理器
func NewHandler(service Service, repo Repository) *Handler {
	return &Handler{
		service: service,
		repo:    repo,
	}
}

// SetUserRepo 设置用户仓库（用于管理员功能）
func (h *Handler) SetUserRepo(userRepo user.Repository) {
	h.userRepo = userRepo
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	sync := r.Group("/sync")
	{
		// 触发同步任务
		sync.POST("/trigger", h.TriggerSync)     // 触发同步任务
		sync.GET("/tasks", h.ListTasks)          // 获取任务列表
		sync.GET("/tasks/:taskId", h.GetTask)    // 获取任务详情
		sync.GET("/status", h.GetUserSyncStatus) // 获取用户同步状态
	}
}

// RegisterAdminRoutes 注册管理员路由
func (h *Handler) RegisterAdminRoutes(r *gin.RouterGroup) {
	sync := r.Group("/sync")
	{
		sync.POST("/all", h.AdminSyncAll)            // 管理员同步所有用户
		sync.GET("/tasks", h.ListTasks)              // 获取任务列表
		sync.GET("/tasks/:taskId", h.GetTask)        // 获取任务详情
		sync.POST("/optimize", h.OptimizeSyncTables) // 优化同步表（释放磁盘空间）
	}
}

// TriggerSync 触发同步任务
// @Summary 触发数据同步任务
// @Tags Sync
// @Accept json
// @Produce json
// @Param request body CreateTaskRequest true "同步任务请求"
// @Success 200 {object} SyncTask
// @Router /sync/trigger [post]
func (h *Handler) TriggerSync(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, "请求参数错误")
		return
	}

	// 参数校验
	if req.TaskType == "" {
		common.Error(c, common.CodeInvalidParams, "任务类型不能为空")
		return
	}

	// 验证任务类型
	validTypes := map[TaskType]bool{
		TaskTypeAll:          true,
		TaskTypeGrade:        true,
		TaskTypeRegularGrade: true,
		TaskTypeExam:         true,
		TaskTypeLevelExam:    true,
		TaskTypeCourse:       true,
	}
	if !validTypes[req.TaskType] {
		common.Error(c, common.CodeInvalidParams, "无效的任务类型")
		return
	}

	var task *SyncTask
	var err error

	// 如果指定了用户列表，则同步指定用户
	if len(req.UserIDs) > 0 {
		task, err = h.service.SyncUsers(c.Request.Context(), req.UserIDs, req.TaskType, TriggerTypeManual)
	} else {
		// 否则只同步当前用户
		task, err = h.service.SyncUser(c.Request.Context(), uid.(int), req.TaskType, TriggerTypeManual)
	}

	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "触发同步任务失败")
		}
		return
	}

	common.Success(c, task)
}

// GetTask 获取任务详情
// @Summary 获取同步任务详情
// @Tags Sync
// @Produce json
// @Param taskId path string true "任务ID"
// @Success 200 {object} TaskDetailResponse
// @Router /sync/tasks/{taskId} [get]
func (h *Handler) GetTask(c *gin.Context) {
	_, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	taskID := c.Param("taskId")
	if taskID == "" {
		common.Error(c, common.CodeInvalidParams, "任务ID不能为空")
		return
	}

	task, err := h.service.GetTask(c.Request.Context(), taskID)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取任务详情失败")
		}
		return
	}

	common.Success(c, task)
}

// ListTasks 获取任务列表
// @Summary 获取同步任务列表
// @Tags Sync
// @Produce json
// @Param limit query int false "每页数量" default(20)
// @Param offset query int false "偏移量" default(0)
// @Success 200 {object} map[string]interface{}
// @Router /sync/tasks [get]
func (h *Handler) ListTasks(c *gin.Context) {
	_, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	// 获取分页参数
	limit := 20
	offset := 0

	if l, ok := c.GetQuery("limit"); ok {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}

	if o, ok := c.GetQuery("offset"); ok {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	tasks, total, err := h.service.ListTasks(c.Request.Context(), limit, offset)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取任务列表失败")
		}
		return
	}

	common.Success(c, gin.H{
		"tasks":  tasks,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetUserSyncStatus 获取用户同步状态
// @Summary 获取用户数据同步状态
// @Tags Sync
// @Produce json
// @Success 200 {object} UserSyncStatusResponse
// @Router /sync/status [get]
func (h *Handler) GetUserSyncStatus(c *gin.Context) {
	uid, ok := c.Get("uid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "未授权")
		return
	}

	status, err := h.service.GetUserSyncStatus(c.Request.Context(), uid.(int))
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "获取同步状态失败")
		}
		return
	}

	common.Success(c, status)
}

// AdminSyncAll 管理员同步所有已绑定用户
// @Summary 管理员同步所有已绑定用户
// @Tags Admin Sync
// @Accept json
// @Produce json
// @Param request body AdminSyncAllRequest true "同步请求"
// @Success 200 {object} SyncTask
// @Router /admin/sync/all [post]
func (h *Handler) AdminSyncAll(c *gin.Context) {
	// 验证管理员权限（通过 aid 判断）
	_, ok := c.Get("aid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "需要管理员权限")
		return
	}

	// 检查 userRepo 是否已设置
	if h.userRepo == nil {
		common.Error(c, common.CodeInternalError, "服务未正确配置")
		return
	}

	var req AdminSyncAllRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Error(c, common.CodeInvalidParams, "请求参数错误")
		return
	}

	// 验证任务类型
	validTypes := map[TaskType]bool{
		TaskTypeAll:          true,
		TaskTypeGrade:        true,
		TaskTypeRegularGrade: true,
		TaskTypeExam:         true,
		TaskTypeLevelExam:    true,
		TaskTypeCourse:       true,
	}
	if !validTypes[req.TaskType] {
		common.Error(c, common.CodeInvalidParams, "无效的任务类型")
		return
	}

	// 获取所有已绑定用户
	users, err := h.userRepo.FindAllBoundUsers(c.Request.Context())
	if err != nil {
		common.Error(c, common.CodeInternalError, "获取用户列表失败")
		return
	}

	if len(users) == 0 {
		common.Error(c, common.CodeInvalidParams, "没有已绑定的用户")
		return
	}

	// 转换为 BoundUserInfo
	boundUsers := make([]BoundUserInfo, len(users))
	for i, u := range users {
		boundUsers[i] = BoundUserInfo{
			Uid:  u.Uid,
			Sid:  u.Sid,
			Spwd: u.Spwd,
		}
	}

	// 触发同步任务
	task, err := h.service.SyncAllBoundUsers(c.Request.Context(), boundUsers, req.TaskType, TriggerTypeManual)
	if err != nil {
		if appErr, ok := err.(*common.AppError); ok {
			common.ErrorWithAppError(c, appErr)
		} else {
			common.Error(c, common.CodeInternalError, "触发同步任务失败: "+err.Error())
		}
		return
	}

	common.Success(c, gin.H{
		"task":        task,
		"total_users": len(boundUsers),
		"message":     "同步任务已启动",
	})
}

// OptimizeSyncTables 管理员手动执行 OPTIMIZE TABLE
// @Summary 优化 sync_tasks 和 sync_logs 表（释放磁盘空间）
// @Tags Admin Sync
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /admin/sync/optimize [post]
func (h *Handler) OptimizeSyncTables(c *gin.Context) {
	// 验证管理员权限
	_, ok := c.Get("aid")
	if !ok {
		common.Error(c, common.CodeUnauthorized, "需要管理员权限")
		return
	}

	if err := h.repo.OptimizeSyncTables(c.Request.Context()); err != nil {
		common.Error(c, common.CodeInternalError, "OPTIMIZE TABLE 执行失败: "+err.Error())
		return
	}

	common.Success(c, gin.H{
		"message": "OPTIMIZE TABLE 执行成功",
	})
}
