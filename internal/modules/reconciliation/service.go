package reconciliation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"spider-go/internal/cache"
	"spider-go/internal/modules/course"
	"spider-go/internal/modules/exam"
	"spider-go/internal/modules/grade"
	"spider-go/internal/modules/ranking"
	"spider-go/internal/shared"
	"spider-go/pkg/errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Service 对账服务接口
type Service interface {
	// SyncUser 触发同步任务
	SyncUser(ctx context.Context, uid int, taskType TaskType, triggerType TriggerType) (*SyncTask, error)
	SyncUsers(ctx context.Context, uids []int, taskType TaskType, triggerType TriggerType) (*SyncTask, error)
	SyncAllUsers(ctx context.Context, taskType TaskType, triggerType TriggerType) (*SyncTask, error)

	// SyncAllBoundUsers 同步所有已绑定用户（管理员专用，带密码验证）
	SyncAllBoundUsers(ctx context.Context, boundUsers []BoundUserInfo, taskType TaskType, triggerType TriggerType) (*SyncTask, error)

	// TriggerGradeSync 异步触发成绩同步（用户查询成绩时调用）
	TriggerGradeSync(ctx context.Context, uid int)

	// GetTask 查询任务状态
	GetTask(ctx context.Context, taskID string) (*TaskDetailResponse, error)
	ListTasks(ctx context.Context, limit, offset int) ([]*SyncTask, int64, error)

	// GetUserSyncStatus 查询用户同步状态
	GetUserSyncStatus(ctx context.Context, uid int) (*UserSyncStatusResponse, error)
}

// service 服务实现
type service struct {
	repo           Repository
	gradeService   grade.Service
	examService    exam.Service
	courseService  course.Service
	configCache    cache.ConfigCache
	rankingService ranking.Service
	userQuery      shared.UserQuery // 用于清除绑定
}

// NewService 创建对账服务
func NewService(repo Repository, gradeService grade.Service, examService exam.Service, courseService course.Service, configCache cache.ConfigCache, rankingService ranking.Service) Service {
	return &service{
		repo:           repo,
		gradeService:   gradeService,
		examService:    examService,
		courseService:  courseService,
		rankingService: rankingService,
		configCache:    configCache,
	}
}

// SetUserQuery 设置用户查询接口（用于延迟注入，避免循环依赖）
func (s *service) SetUserQuery(userQuery shared.UserQuery) {
	s.userQuery = userQuery
}

// SyncUser 同步单个用户
func (s *service) SyncUser(ctx context.Context, uid int, taskType TaskType, triggerType TriggerType) (*SyncTask, error) {
	return s.SyncUsers(ctx, []int{uid}, taskType, triggerType)
}

// SyncUsers 同步多个用户
func (s *service) SyncUsers(ctx context.Context, uids []int, taskType TaskType, triggerType TriggerType) (*SyncTask, error) {
	// 创建同步任务
	task := &SyncTask{
		TaskID:      uuid.New().String(),
		TaskType:    taskType,
		TriggerType: triggerType,
		Status:      TaskStatusPending,
		TotalUsers:  len(uids),
	}

	if err := s.repo.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("创建同步任务失败: %w", err)
	}

	// 异步执行同步任务
	go s.executeSyncTask(context.Background(), task, uids)

	return task, nil
}

// SyncAllUsers 同步所有用户
func (s *service) SyncAllUsers(ctx context.Context, taskType TaskType, triggerType TriggerType) (*SyncTask, error) {
	// TODO: 从用户表获取所有已绑定用户的 uid 列表
	// 这里需要注入 UserRepository 或 UserService
	return nil, fmt.Errorf("SyncAllUsers not implemented yet")
}

// executeSyncTask 执行同步任务
func (s *service) executeSyncTask(ctx context.Context, task *SyncTask, uids []int) {
	// 更新任务状态为执行中
	now := time.Now()
	task.Status = TaskStatusProcessing
	task.StartTime = &now
	s.repo.UpdateTask(ctx, task)

	// 根据任务类型执行不同的同步逻辑
	var err error
	switch task.TaskType {
	case TaskTypeGrade:
		err = s.syncGrades(ctx, task, uids)
	case TaskTypeRegularGrade:
		err = s.syncRegularGrades(ctx, task, uids)
	case TaskTypeExam:
		err = s.syncExams(ctx, task, uids)
	case TaskTypeLevelExam:
		err = s.syncLevelExams(ctx, task, uids)
	case TaskTypeCourse:
		err = s.syncCourses(ctx, task, uids)
	case TaskTypeAll:
		err = s.syncAll(ctx, task, uids)
	default:
		err = fmt.Errorf("未知的任务类型: %s", task.TaskType)
	}

	// 更新任务完成状态
	endTime := time.Now()
	task.EndTime = &endTime

	if err != nil {
		task.Status = TaskStatusFailed
		task.ErrorMsg = err.Error()
	} else {
		task.Status = TaskStatusSuccess
	}

	s.repo.UpdateTask(ctx, task)
}

// syncGrades 同步成绩
func (s *service) syncGrades(ctx context.Context, task *SyncTask, uids []int) error {
	logs := make([]*SyncLog, 0)
	newVersion := int(time.Now().Unix())

	for _, uid := range uids {
		task.ProcessedUsers++

		// 从教务系统获取成绩（使用不触发同步的方法，避免递归）
		gradeData, _, err := s.gradeService.GetAllGradesForSync(ctx, uid)
		if err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "grade", "", err))

			// 检查是否是认证错误，如果是则清除绑定
			if s.isAuthenticationError(err) {
				log.Printf("[syncGrades] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
				if s.userQuery != nil {
					if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
						log.Printf("[syncGrades] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
					} else {
						log.Printf("[syncGrades] 已清除用户 %d 的教务系统绑定", uid)
					}
				}
			}
			continue
		}

		// 获取本地已有成绩
		localGrades, _ := s.repo.GetGradesByUid(ctx, uid)
		localGradeMap := make(map[string]*Grade)
		for _, g := range localGrades {
			key := fmt.Sprintf("%s-%s", g.Term, g.Code)
			localGradeMap[key] = g
		}

		// 对账并更新
		remoteGradeMap := make(map[string]bool)
		grades := make([]*Grade, 0)

		for _, remote := range gradeData {
			key := fmt.Sprintf("%s-%s", remote.Term, remote.Code)
			remoteGradeMap[key] = true

			local, exists := localGradeMap[key]

			newGrade := &Grade{
				Uid:            uid,
				SerialNo:       remote.SerialNo,
				Term:           remote.Term,
				Code:           remote.Code,
				Subject:        remote.Subject,
				Score:          remote.Score,
				Credit:         remote.Credit,
				Gpa:            remote.Gpa,
				Status:         remote.Status,
				Property:       remote.Property,
				CourseProperty: remote.CourseProperty,
				Flag:           remote.Flag,
				SyncVersion:    newVersion,
				LastSyncTaskID: task.TaskID,
			}
			now := time.Now()
			newGrade.LastSyncAt = &now

			if !exists {
				// 新增
				task.NewRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionInsert, nil, newGrade, true, ""))
			} else if s.gradeChanged(local, newGrade) {
				// 更新
				task.UpdatedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionUpdate, local, newGrade, true, ""))
			} else {
				// 未变化
				task.UnchangedRecords++
				newGrade.ID = local.ID
				logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionSkip, nil, nil, true, ""))
			}

			grades = append(grades, newGrade)
		}

		// 标记删除的记录
		for key, local := range localGradeMap {
			if !remoteGradeMap[key] {
				task.DeletedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionDelete, local, nil, true, ""))
				// 软删除
				s.repo.DeleteGradesByUidAndTerm(ctx, uid, local.Term)
			}
		}

		// 批量更新数据库
		if err := s.repo.BatchUpsertGrades(ctx, grades); err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "grade", "", err))
			continue
		}

		// 更新用户同步状态
		if err := s.repo.UpdateGradeSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
			// 记录错误但不中断
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "grade_sync_status", "", err))
		}

		task.SuccessUsers++
		// 更新GPA排名
		s.updateStudentGPARanking(ctx, uid, task.TaskID)
	}

	// 批量保存日志
	if len(logs) > 0 {
		s.repo.BatchCreateLogs(ctx, logs)
	}

	s.repo.UpdateTask(ctx, task)
	return nil
}

// syncRegularGrades 同步平时分
func (s *service) syncRegularGrades(ctx context.Context, task *SyncTask, uids []int) error {
	logs := make([]*SyncLog, 0)
	newVersion := int(time.Now().Unix())

	for _, uid := range uids {
		task.ProcessedUsers++

		// 先获取用户的所有成绩,从成绩中提取 term 和 code（使用不触发同步的方法，避免递归）
		grades, _, err := s.gradeService.GetAllGradesForSync(ctx, uid)
		if err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "regular_grade", "", err))

			// 检查是否是认证错误，如果是则清除绑定
			if s.isAuthenticationError(err) {
				log.Printf("[syncRegularGrades] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
				if s.userQuery != nil {
					if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
						log.Printf("[syncRegularGrades] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
					} else {
						log.Printf("[syncRegularGrades] 已清除用户 %d 的教务系统绑定", uid)
					}
				}
			}
			continue
		}

		// 如果没有成绩,跳过
		if len(grades) == 0 {
			task.SkippedUsers++
			continue
		}

		// 获取本地已有平时分
		localRegularGrades, _ := s.repo.GetRegularGradesByUid(ctx, uid)
		localRegularGradeMap := make(map[string]*RegularGrade)
		for _, rg := range localRegularGrades {
			key := fmt.Sprintf("%s-%s", rg.Term, rg.Code)
			localRegularGradeMap[key] = rg
		}

		// 对账并更新
		remoteRegularGradeMap := make(map[string]bool)
		regularGrades := make([]*RegularGrade, 0)

		// 遍历所有成绩,尝试获取平时分
		for _, grade := range grades {
			// 尝试获取平时分
			regularGrade, err := s.gradeService.GetRegularGrades(ctx, uid, grade.Term, grade.Code)
			if err != nil {
				// 如果获取失败(可能是没有平时分或链接未缓存),跳过该科目
				continue
			}

			// 检查平时分是否为空(没有实际数据)
			if regularGrade.FinalExamScore == "" && regularGrade.RegularScore == "" && regularGrade.FinalScore == "" {
				// 平时分为空,不保存
				continue
			}

			key := fmt.Sprintf("%s-%s", grade.Term, grade.Code)
			remoteRegularGradeMap[key] = true

			local, exists := localRegularGradeMap[key]

			newRegularGrade := &RegularGrade{
				Uid:            uid,
				Term:           grade.Term,
				Code:           grade.Code,
				Subject:        grade.Subject,
				FinalExamScore: regularGrade.FinalExamScore,
				FinalExamRatio: regularGrade.FinalExamRatio,
				RegularScore:   regularGrade.RegularScore,
				RegularRatio:   regularGrade.RegularRatio,
				FinalScore:     regularGrade.FinalScore,
				SyncVersion:    newVersion,
				LastSyncTaskID: task.TaskID,
			}
			now := time.Now()
			newRegularGrade.LastSyncAt = &now

			if !exists {
				// 新增
				task.NewRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionInsert, nil, newRegularGrade, true, ""))
			} else if s.regularGradeChanged(local, newRegularGrade) {
				// 更新
				task.UpdatedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionUpdate, local, newRegularGrade, true, ""))
			} else {
				// 未变化
				task.UnchangedRecords++
				newRegularGrade.ID = local.ID
				logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionSkip, nil, nil, true, ""))
			}

			regularGrades = append(regularGrades, newRegularGrade)
		}

		// 如果没有获取到任何平时分,跳过该用户
		if len(regularGrades) == 0 {
			task.SkippedUsers++
			continue
		}

		// 标记删除的记录(远程已不存在的平时分)
		for key, local := range localRegularGradeMap {
			if !remoteRegularGradeMap[key] {
				task.DeletedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionDelete, local, nil, true, ""))
				// 软删除
				s.repo.DeleteGradesByUidAndTerm(ctx, uid, local.Term)
			}
		}

		// 批量更新数据库
		if err := s.repo.BatchUpsertRegularGrades(ctx, regularGrades); err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "regular_grade", "", err))
			continue
		}

		// 更新用户同步状态
		if err := s.repo.UpdateRegularGradeSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
			// 记录错误但不中断
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "regular_grade_sync_status", "", err))
		}

		task.SuccessUsers++
	}

	// 批量保存日志
	if len(logs) > 0 {
		s.repo.BatchCreateLogs(ctx, logs)
	}

	s.repo.UpdateTask(ctx, task)
	return nil
}

// syncExams 同步考试安排
func (s *service) syncExams(ctx context.Context, task *SyncTask, uids []int) error {
	logs := make([]*SyncLog, 0)
	newVersion := int(time.Now().Unix())

	// 获取最近3个学期
	terms, err := s.configCache.GetPreviousTerms(ctx, 3)
	if err != nil {
		return fmt.Errorf("获取学期列表失败: %w", err)
	}

	for _, uid := range uids {
		task.ProcessedUsers++

		// 获取本地已有考试
		localExams, _ := s.repo.GetExamsByUid(ctx, uid)
		localExamMap := make(map[string]*Exam)
		for _, e := range localExams {
			key := fmt.Sprintf("%s-%s-%s", e.Term, e.ClassName, e.Time)
			localExamMap[key] = e
		}

		// 对账并更新
		remoteExamMap := make(map[string]bool)
		exams := make([]*Exam, 0)

		// 循环获取多个学期的考试数据
		for _, term := range terms {
			// 从教务系统获取该学期考试（使用 ForSync 方法避免递归）
			examData, err := s.examService.GetAllExamsForSync(ctx, uid, term)
			if err != nil {
				// 检查是否是认证错误
				if s.isAuthenticationError(err) {
					log.Printf("[syncExams] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
					if s.userQuery != nil {
						if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
							log.Printf("[syncExams] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
						} else {
							log.Printf("[syncExams] 已清除用户 %d 的教务系统绑定", uid)
						}
					}
					task.FailedUsers++
					logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam", fmt.Sprintf("term:%s auth_error", term), err))
					break // 认证错误时跳出学期循环
				}
				// 如果某个学期获取失败（非认证错误），记录但不中断整个同步
				logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam", fmt.Sprintf("term:%s", term), err))
				continue
			}

			for _, remote := range examData {
				key := fmt.Sprintf("%s-%s-%s", term, remote.ClassName, remote.Time)
				remoteExamMap[key] = true

				local, exists := localExamMap[key]

				newExam := &Exam{
					Uid:            uid,
					Term:           term,
					SerialNo:       remote.SerialNo,
					ClassNo:        remote.ClassNo,
					ClassName:      remote.ClassName,
					Time:           remote.Time,
					Place:          remote.Place,
					Execution:      remote.Execution,
					SyncVersion:    newVersion,
					LastSyncTaskID: task.TaskID,
				}
				now := time.Now()
				newExam.LastSyncAt = &now

				if !exists {
					// 新增
					task.NewRecords++
					logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionInsert, nil, newExam, true, ""))
				} else if s.examChanged(local, newExam) {
					// 更新
					task.UpdatedRecords++
					logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionUpdate, local, newExam, true, ""))
				} else {
					// 未变化
					task.UnchangedRecords++
					newExam.ID = local.ID
					logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionSkip, nil, nil, true, ""))
				}

				exams = append(exams, newExam)
			}
		}

		// 标记删除的记录
		for key, local := range localExamMap {
			if !remoteExamMap[key] {
				task.DeletedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionDelete, local, nil, true, ""))
				// 这里应该软删除，但目前没有DeleteExamsByUid方法
			}
		}

		// 批量更新数据库
		if err := s.repo.BatchUpsertExams(ctx, exams); err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam", "", err))
			continue
		}

		// 更新用户同步状态
		if err := s.repo.UpdateExamSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
			// 记录错误但不中断
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam_sync_status", "", err))
		}

		task.SuccessUsers++
	}

	// 批量保存日志
	if len(logs) > 0 {
		s.repo.BatchCreateLogs(ctx, logs)
	}

	s.repo.UpdateTask(ctx, task)
	return nil
}

// syncLevelExams 同步等级考试
func (s *service) syncLevelExams(ctx context.Context, task *SyncTask, uids []int) error {
	logs := make([]*SyncLog, 0)
	newVersion := int(time.Now().Unix())

	for _, uid := range uids {
		task.ProcessedUsers++

		// 从教务系统获取等级考试成绩（使用 ForSync 方法避免递归）
		levelExamData, err := s.gradeService.GetLevelGradesForSync(ctx, uid)
		if err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "level_exam", "", err))

			// 检查是否是认证错误，如果是则清除绑定
			if s.isAuthenticationError(err) {
				log.Printf("[syncLevelExams] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
				if s.userQuery != nil {
					if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
						log.Printf("[syncLevelExams] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
					} else {
						log.Printf("[syncLevelExams] 已清除用户 %d 的教务系统绑定", uid)
					}
				}
			}
			continue
		}

		// 获取本地已有等级考试成绩
		localLevelExams, _ := s.repo.GetLevelExamsByUid(ctx, uid)
		localLevelExamMap := make(map[string]*LevelExam)
		for _, le := range localLevelExams {
			key := fmt.Sprintf("%s-%s", le.CourseName, le.Time)
			localLevelExamMap[key] = le
		}

		// 对账并更新
		remoteLevelExamMap := make(map[string]bool)
		levelExams := make([]*LevelExam, 0)

		for _, remote := range levelExamData {
			key := fmt.Sprintf("%s-%s", remote.CourseName, remote.Time)
			remoteLevelExamMap[key] = true

			local, exists := localLevelExamMap[key]

			newLevelExam := &LevelExam{
				Uid:            uid,
				No:             remote.No,
				CourseName:     remote.CourseName,
				LevGrade:       remote.LevGrade,
				Time:           remote.Time,
				SyncVersion:    newVersion,
				LastSyncTaskID: task.TaskID,
			}
			now := time.Now()
			newLevelExam.LastSyncAt = &now

			if !exists {
				// 新增
				task.NewRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionInsert, nil, newLevelExam, true, ""))
			} else if s.levelExamChanged(local, newLevelExam) {
				// 更新
				task.UpdatedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionUpdate, local, newLevelExam, true, ""))
			} else {
				// 未变化
				task.UnchangedRecords++
				newLevelExam.ID = local.ID
				logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionSkip, nil, nil, true, ""))
			}

			levelExams = append(levelExams, newLevelExam)
		}

		// 标记删除的记录
		for key, local := range localLevelExamMap {
			if !remoteLevelExamMap[key] {
				task.DeletedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionDelete, local, nil, true, ""))
				// 这里应该软删除，但目前没有DeleteLevelExamsByUid方法
				// 后续可以添加 s.repo.DeleteLevelExam(ctx, local.ID)
			}
		}

		// 批量更新数据库
		if err := s.repo.BatchUpsertLevelExams(ctx, levelExams); err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "level_exam", "", err))
			continue
		}

		// 更新用户同步状态
		if err := s.repo.UpdateLevelExamSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
			// 记录错误但不中断
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "level_exam_sync_status", "", err))
		}

		task.SuccessUsers++
	}

	// 批量保存日志
	if len(logs) > 0 {
		s.repo.BatchCreateLogs(ctx, logs)
	}

	s.repo.UpdateTask(ctx, task)
	return nil
}

// syncCourses 同步课表
func (s *service) syncCourses(ctx context.Context, task *SyncTask, uids []int) error {
	logs := make([]*SyncLog, 0)
	newVersion := int(time.Now().Unix())

	// 获取当前学期
	currentTerm, err := s.configCache.GetCurrentTerm(ctx)
	if err != nil {
		return fmt.Errorf("获取当前学期失败: %w", err)
	}

	// 获取全部20周的课表
	for _, uid := range uids {
		task.ProcessedUsers++

		// 获取本地已有课表
		localCourses, _ := s.repo.GetCoursesByUid(ctx, uid, "", 0)
		localCourseMap := make(map[string]*Course)
		for _, c := range localCourses {
			key := fmt.Sprintf("%s-%d-%d-%d-%s", c.Term, c.Week, c.Weekday, c.StartPeriod, c.Name)
			localCourseMap[key] = c
		}

		// 对账并更新
		remoteCourseMap := make(map[string]bool)
		courses := make([]*Course, 0)

		// 循环获取20周的课表
		authFailed := false
		for week := 1; week <= 20; week++ {
			// 从教务系统获取该周课表（使用 ForSync 方法避免递归）
			schedule, err := s.courseService.GetCourseTableByWeekForSync(ctx, uid, week, currentTerm)
			if err != nil {
				// 检查是否是认证错误
				if s.isAuthenticationError(err) {
					log.Printf("[syncCourses] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
					if s.userQuery != nil {
						if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
							log.Printf("[syncCourses] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
						} else {
							log.Printf("[syncCourses] 已清除用户 %d 的教务系统绑定", uid)
						}
					}
					task.FailedUsers++
					logs = append(logs, s.createErrorLog(task.TaskID, uid, "course", fmt.Sprintf("term:%s-week:%d auth_error", currentTerm, week), err))
					authFailed = true
					break // 认证错误时跳出周循环
				}
				// 如果某周获取失败（非认证错误），记录但不中断整个同步
				logs = append(logs, s.createErrorLog(task.TaskID, uid, "course", fmt.Sprintf("term:%s-week:%d", currentTerm, week), err))
				continue
			}

			// 遍历每天的课程
			for _, daySchedule := range schedule.Days {
				for _, remote := range daySchedule.Courses {
					key := fmt.Sprintf("%s-%d-%d-%d-%s", currentTerm, week, remote.Weekday, remote.StartPeriod, remote.Name)
					remoteCourseMap[key] = true

					local, exists := localCourseMap[key]

					newCourse := &Course{
						Uid:            uid,
						Term:           currentTerm,
						Week:           week,
						Weekday:        remote.Weekday,
						Name:           remote.Name,
						Teacher:        remote.Teacher,
						Classroom:      remote.Classroom,
						StartPeriod:    remote.StartPeriod,
						EndPeriod:      remote.EndPeriod,
						SyncVersion:    newVersion,
						LastSyncTaskID: task.TaskID,
					}
					now := time.Now()
					newCourse.LastSyncAt = &now

					if !exists {
						// 新增
						task.NewRecords++
						logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionInsert, nil, newCourse, true, ""))
					} else if s.courseChanged(local, newCourse) {
						// 更新
						task.UpdatedRecords++
						logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionUpdate, local, newCourse, true, ""))
					} else {
						// 未变化
						task.UnchangedRecords++
						newCourse.ID = local.ID
						logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionSkip, nil, nil, true, ""))
					}

					courses = append(courses, newCourse)
				}
			}
		}

		// 如果认证失败，跳过数据库更新
		if authFailed {
			continue
		}

		// 标记删除的记录(只删除当前学期的旧数据)
		for key, local := range localCourseMap {
			if local.Term == currentTerm && !remoteCourseMap[key] {
				task.DeletedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionDelete, local, nil, true, ""))
				// 这里应该软删除，但目前没有DeleteCoursesByUid方法
			}
		}

		// 批量更新数据库
		if err := s.repo.BatchUpsertCourses(ctx, courses); err != nil {
			task.FailedUsers++
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "course", "", err))
			continue
		}

		// 更新用户同步状态
		if err := s.repo.UpdateCourseSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
			// 记录错误但不中断
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "course_sync_status", "", err))
		}

		task.SuccessUsers++
	}

	// 批量保存日志
	if len(logs) > 0 {
		s.repo.BatchCreateLogs(ctx, logs)
	}

	s.repo.UpdateTask(ctx, task)
	return nil
}

// syncAll 全量同步（按用户逐个同步所有数据类型）
func (s *service) syncAll(ctx context.Context, task *SyncTask, uids []int) error {
	logs := make([]*SyncLog, 0)
	newVersion := int(time.Now().Unix())

	// 获取当前学期和最近3个学期
	currentTerm, err := s.configCache.GetCurrentTerm(ctx)
	if err != nil {
		return fmt.Errorf("获取当前学期失败: %w", err)
	}

	terms, err := s.configCache.GetPreviousTerms(ctx, 3)
	if err != nil {
		return fmt.Errorf("获取学期列表失败: %w", err)
	}

	// 按用户逐个同步
	for _, uid := range uids {
		task.ProcessedUsers++

		// 同步该用户的所有数据类型
		userLogs, authFailed := s.syncUserAllData(ctx, task, uid, newVersion, currentTerm, terms)
		logs = append(logs, userLogs...)

		if authFailed {
			task.FailedUsers++
		} else {
			task.SuccessUsers++
		}
	}

	// 批量保存日志
	if len(logs) > 0 {
		s.repo.BatchCreateLogs(ctx, logs)
	}

	s.repo.UpdateTask(ctx, task)
	return nil
}

// syncUserAllData 同步单个用户的所有数据类型
// 返回日志列表和是否认证失败
func (s *service) syncUserAllData(ctx context.Context, task *SyncTask, uid int, newVersion int, currentTerm string, terms []string) ([]*SyncLog, bool) {
	logs := make([]*SyncLog, 0)
	syncedItems := make([]string, 0)

	// 获取用户信息（用于日志）- 优先从学籍卡片获取真实姓名
	var userName, userSid string
	if userInfo, err := s.gradeService.GetUserGradeMajorClass(ctx, uid); err == nil && userInfo.Name != "" {
		userName = userInfo.Name
	}
	// 获取学号
	if s.userQuery != nil {
		if userInfo, err := s.userQuery.GetUserByUid(ctx, uid); err == nil {
			userSid = userInfo.Sid
		}
	}

	// 1. 同步成绩
	gradeLogs, authFailed := s.syncUserGrades(ctx, task, uid, newVersion)
	logs = append(logs, gradeLogs...)
	if authFailed {
		log.Printf("[syncUserAllData] 同步失败 uid=%d 姓名=%s 学号=%s 原因=认证失败", uid, userName, userSid)
		return logs, true
	}
	syncedItems = append(syncedItems, "成绩")

	// 2. 同步平时分
	regularGradeLogs, authFailed := s.syncUserRegularGrades(ctx, task, uid, newVersion)
	logs = append(logs, regularGradeLogs...)
	if authFailed {
		log.Printf("[syncUserAllData] 同步失败 uid=%d 姓名=%s 学号=%s 原因=认证失败", uid, userName, userSid)
		return logs, true
	}
	syncedItems = append(syncedItems, "平时分")

	// 3. 同步考试安排
	examLogs, authFailed := s.syncUserExams(ctx, task, uid, newVersion, terms)
	logs = append(logs, examLogs...)
	if authFailed {
		log.Printf("[syncUserAllData] 同步失败 uid=%d 姓名=%s 学号=%s 原因=认证失败", uid, userName, userSid)
		return logs, true
	}
	syncedItems = append(syncedItems, "考试安排")

	// 4. 同步等级考试
	levelExamLogs, authFailed := s.syncUserLevelExams(ctx, task, uid, newVersion)
	logs = append(logs, levelExamLogs...)
	if authFailed {
		log.Printf("[syncUserAllData] 同步失败 uid=%d 姓名=%s 学号=%s 原因=认证失败", uid, userName, userSid)
		return logs, true
	}
	syncedItems = append(syncedItems, "等级考试")

	// 5. 同步课表
	courseLogs, authFailed := s.syncUserCourses(ctx, task, uid, newVersion, currentTerm)
	logs = append(logs, courseLogs...)
	if authFailed {
		log.Printf("[syncUserAllData] 同步失败 uid=%d 姓名=%s 学号=%s 原因=认证失败", uid, userName, userSid)
		return logs, true
	}
	syncedItems = append(syncedItems, "课表")

	// 打印同步完成日志
	log.Printf("[syncUserAllData] 同步完成 uid=%d 姓名=%s 学号=%s 同步项目=[%s]",
		uid, userName, userSid, strings.Join(syncedItems, ", "))

	return logs, false
}

// syncUserGrades 同步单个用户的成绩
func (s *service) syncUserGrades(ctx context.Context, task *SyncTask, uid int, newVersion int) ([]*SyncLog, bool) {
	logs := make([]*SyncLog, 0)

	// 从教务系统获取成绩
	gradeData, _, err := s.gradeService.GetAllGradesForSync(ctx, uid)
	if err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "grade", "", err))

		if s.isAuthenticationError(err) {
			log.Printf("[syncUserGrades] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
			if s.userQuery != nil {
				if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
					log.Printf("[syncUserGrades] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
				} else {
					log.Printf("[syncUserGrades] 已清除用户 %d 的教务系统绑定", uid)
				}
			}
			return logs, true
		}
		return logs, false
	}

	// 获取本地已有成绩
	localGrades, _ := s.repo.GetGradesByUid(ctx, uid)
	localGradeMap := make(map[string]*Grade)
	for _, g := range localGrades {
		key := fmt.Sprintf("%s-%s", g.Term, g.Code)
		localGradeMap[key] = g
	}

	// 对账并更新
	remoteGradeMap := make(map[string]bool)
	grades := make([]*Grade, 0)

	for _, remote := range gradeData {
		key := fmt.Sprintf("%s-%s", remote.Term, remote.Code)
		remoteGradeMap[key] = true

		local, exists := localGradeMap[key]

		newGrade := &Grade{
			Uid:            uid,
			SerialNo:       remote.SerialNo,
			Term:           remote.Term,
			Code:           remote.Code,
			Subject:        remote.Subject,
			Score:          remote.Score,
			Credit:         remote.Credit,
			Gpa:            remote.Gpa,
			Status:         remote.Status,
			Property:       remote.Property,
			CourseProperty: remote.CourseProperty,
			Flag:           remote.Flag,
			SyncVersion:    newVersion,
			LastSyncTaskID: task.TaskID,
		}
		now := time.Now()
		newGrade.LastSyncAt = &now

		if !exists {
			task.NewRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionInsert, nil, newGrade, true, ""))
		} else if s.gradeChanged(local, newGrade) {
			task.UpdatedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionUpdate, local, newGrade, true, ""))
		} else {
			task.UnchangedRecords++
			newGrade.ID = local.ID
		}

		grades = append(grades, newGrade)
	}

	// 标记删除的记录
	for key, local := range localGradeMap {
		if !remoteGradeMap[key] {
			task.DeletedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "grade", key, SyncActionDelete, local, nil, true, ""))
			s.repo.DeleteGradesByUidAndTerm(ctx, uid, local.Term)
		}
	}

	// 批量更新数据库
	if err := s.repo.BatchUpsertGrades(ctx, grades); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "grade", "", err))
		return logs, false
	}

	// 更新用户同步状态
	if err := s.repo.UpdateGradeSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "grade_sync_status", "", err))
	}

	// 更新GPA排名
	s.updateStudentGPARanking(ctx, uid, task.TaskID)

	return logs, false
}

// syncUserRegularGrades 同步单个用户的平时分
func (s *service) syncUserRegularGrades(ctx context.Context, task *SyncTask, uid int, newVersion int) ([]*SyncLog, bool) {
	logs := make([]*SyncLog, 0)

	// 先获取用户的所有成绩
	grades, _, err := s.gradeService.GetAllGradesForSync(ctx, uid)
	if err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "regular_grade", "", err))

		if s.isAuthenticationError(err) {
			log.Printf("[syncUserRegularGrades] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
			if s.userQuery != nil {
				if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
					log.Printf("[syncUserRegularGrades] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
				} else {
					log.Printf("[syncUserRegularGrades] 已清除用户 %d 的教务系统绑定", uid)
				}
			}
			return logs, true
		}
		return logs, false
	}

	if len(grades) == 0 {
		return logs, false
	}

	// 获取本地已有平时分
	localRegularGrades, _ := s.repo.GetRegularGradesByUid(ctx, uid)
	localRegularGradeMap := make(map[string]*RegularGrade)
	for _, rg := range localRegularGrades {
		key := fmt.Sprintf("%s-%s", rg.Term, rg.Code)
		localRegularGradeMap[key] = rg
	}

	// 对账并更新
	remoteRegularGradeMap := make(map[string]bool)
	regularGrades := make([]*RegularGrade, 0)

	for _, grade := range grades {
		regularGrade, err := s.gradeService.GetRegularGrades(ctx, uid, grade.Term, grade.Code)
		if err != nil {
			continue
		}

		if regularGrade.FinalExamScore == "" && regularGrade.RegularScore == "" && regularGrade.FinalScore == "" {
			continue
		}

		key := fmt.Sprintf("%s-%s", grade.Term, grade.Code)
		remoteRegularGradeMap[key] = true

		local, exists := localRegularGradeMap[key]

		newRegularGrade := &RegularGrade{
			Uid:            uid,
			Term:           grade.Term,
			Code:           grade.Code,
			Subject:        grade.Subject,
			FinalExamScore: regularGrade.FinalExamScore,
			FinalExamRatio: regularGrade.FinalExamRatio,
			RegularScore:   regularGrade.RegularScore,
			RegularRatio:   regularGrade.RegularRatio,
			FinalScore:     regularGrade.FinalScore,
			SyncVersion:    newVersion,
			LastSyncTaskID: task.TaskID,
		}
		now := time.Now()
		newRegularGrade.LastSyncAt = &now

		if !exists {
			task.NewRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionInsert, nil, newRegularGrade, true, ""))
		} else if s.regularGradeChanged(local, newRegularGrade) {
			task.UpdatedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionUpdate, local, newRegularGrade, true, ""))
		} else {
			task.UnchangedRecords++
			newRegularGrade.ID = local.ID
		}

		regularGrades = append(regularGrades, newRegularGrade)
	}

	if len(regularGrades) == 0 {
		return logs, false
	}

	// 标记删除的记录
	for key, local := range localRegularGradeMap {
		if !remoteRegularGradeMap[key] {
			task.DeletedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "regular_grade", key, SyncActionDelete, local, nil, true, ""))
			s.repo.DeleteGradesByUidAndTerm(ctx, uid, local.Term)
		}
	}

	// 批量更新数据库
	if err := s.repo.BatchUpsertRegularGrades(ctx, regularGrades); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "regular_grade", "", err))
		return logs, false
	}

	// 更新用户同步状态
	if err := s.repo.UpdateRegularGradeSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "regular_grade_sync_status", "", err))
	}

	return logs, false
}

// syncUserExams 同步单个用户的考试安排
func (s *service) syncUserExams(ctx context.Context, task *SyncTask, uid int, newVersion int, terms []string) ([]*SyncLog, bool) {
	logs := make([]*SyncLog, 0)

	// 获取本地已有考试
	localExams, _ := s.repo.GetExamsByUid(ctx, uid)
	localExamMap := make(map[string]*Exam)
	for _, e := range localExams {
		key := fmt.Sprintf("%s-%s-%s", e.Term, e.ClassName, e.Time)
		localExamMap[key] = e
	}

	remoteExamMap := make(map[string]bool)
	exams := make([]*Exam, 0)

	for _, term := range terms {
		examData, err := s.examService.GetAllExamsForSync(ctx, uid, term)
		if err != nil {
			if s.isAuthenticationError(err) {
				log.Printf("[syncUserExams] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
				if s.userQuery != nil {
					if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
						log.Printf("[syncUserExams] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
					} else {
						log.Printf("[syncUserExams] 已清除用户 %d 的教务系统绑定", uid)
					}
				}
				logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam", fmt.Sprintf("term:%s auth_error", term), err))
				return logs, true
			}
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam", fmt.Sprintf("term:%s", term), err))
			continue
		}

		for _, remote := range examData {
			key := fmt.Sprintf("%s-%s-%s", term, remote.ClassName, remote.Time)
			remoteExamMap[key] = true

			local, exists := localExamMap[key]

			newExam := &Exam{
				Uid:            uid,
				Term:           term,
				SerialNo:       remote.SerialNo,
				ClassNo:        remote.ClassNo,
				ClassName:      remote.ClassName,
				Time:           remote.Time,
				Place:          remote.Place,
				Execution:      remote.Execution,
				SyncVersion:    newVersion,
				LastSyncTaskID: task.TaskID,
			}
			now := time.Now()
			newExam.LastSyncAt = &now

			if !exists {
				task.NewRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionInsert, nil, newExam, true, ""))
			} else if s.examChanged(local, newExam) {
				task.UpdatedRecords++
				logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionUpdate, local, newExam, true, ""))
			} else {
				task.UnchangedRecords++
				newExam.ID = local.ID
			}

			exams = append(exams, newExam)
		}
	}

	// 标记删除的记录
	for key, local := range localExamMap {
		if !remoteExamMap[key] {
			task.DeletedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "exam", key, SyncActionDelete, local, nil, true, ""))
		}
	}

	// 批量更新数据库
	if err := s.repo.BatchUpsertExams(ctx, exams); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam", "", err))
		return logs, false
	}

	// 更新用户同步状态
	if err := s.repo.UpdateExamSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "exam_sync_status", "", err))
	}

	return logs, false
}

// syncUserLevelExams 同步单个用户的等级考试
func (s *service) syncUserLevelExams(ctx context.Context, task *SyncTask, uid int, newVersion int) ([]*SyncLog, bool) {
	logs := make([]*SyncLog, 0)

	levelExamData, err := s.gradeService.GetLevelGradesForSync(ctx, uid)
	if err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "level_exam", "", err))

		if s.isAuthenticationError(err) {
			log.Printf("[syncUserLevelExams] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
			if s.userQuery != nil {
				if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
					log.Printf("[syncUserLevelExams] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
				} else {
					log.Printf("[syncUserLevelExams] 已清除用户 %d 的教务系统绑定", uid)
				}
			}
			return logs, true
		}
		return logs, false
	}

	// 获取本地已有等级考试成绩
	localLevelExams, _ := s.repo.GetLevelExamsByUid(ctx, uid)
	localLevelExamMap := make(map[string]*LevelExam)
	for _, le := range localLevelExams {
		key := fmt.Sprintf("%s-%s", le.CourseName, le.Time)
		localLevelExamMap[key] = le
	}

	remoteLevelExamMap := make(map[string]bool)
	levelExams := make([]*LevelExam, 0)

	for _, remote := range levelExamData {
		key := fmt.Sprintf("%s-%s", remote.CourseName, remote.Time)
		remoteLevelExamMap[key] = true

		local, exists := localLevelExamMap[key]

		newLevelExam := &LevelExam{
			Uid:            uid,
			No:             remote.No,
			CourseName:     remote.CourseName,
			LevGrade:       remote.LevGrade,
			Time:           remote.Time,
			SyncVersion:    newVersion,
			LastSyncTaskID: task.TaskID,
		}
		now := time.Now()
		newLevelExam.LastSyncAt = &now

		if !exists {
			task.NewRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionInsert, nil, newLevelExam, true, ""))
		} else if s.levelExamChanged(local, newLevelExam) {
			task.UpdatedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionUpdate, local, newLevelExam, true, ""))
		} else {
			task.UnchangedRecords++
			newLevelExam.ID = local.ID
		}

		levelExams = append(levelExams, newLevelExam)
	}

	// 标记删除的记录
	for key, local := range localLevelExamMap {
		if !remoteLevelExamMap[key] {
			task.DeletedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "level_exam", key, SyncActionDelete, local, nil, true, ""))
		}
	}

	// 批量更新数据库
	if err := s.repo.BatchUpsertLevelExams(ctx, levelExams); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "level_exam", "", err))
		return logs, false
	}

	// 更新用户同步状态
	if err := s.repo.UpdateLevelExamSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "level_exam_sync_status", "", err))
	}

	return logs, false
}

// syncUserCourses 同步单个用户的课表
func (s *service) syncUserCourses(ctx context.Context, task *SyncTask, uid int, newVersion int, currentTerm string) ([]*SyncLog, bool) {
	logs := make([]*SyncLog, 0)

	// 获取本地已有课表
	localCourses, _ := s.repo.GetCoursesByUid(ctx, uid, "", 0)
	localCourseMap := make(map[string]*Course)
	for _, c := range localCourses {
		key := fmt.Sprintf("%s-%d-%d-%d-%s", c.Term, c.Week, c.Weekday, c.StartPeriod, c.Name)
		localCourseMap[key] = c
	}

	remoteCourseMap := make(map[string]bool)
	courses := make([]*Course, 0)

	for week := 1; week <= 20; week++ {
		schedule, err := s.courseService.GetCourseTableByWeekForSync(ctx, uid, week, currentTerm)
		if err != nil {
			if s.isAuthenticationError(err) {
				log.Printf("[syncUserCourses] 用户 %d 登录失败，清除绑定信息: %v", uid, err)
				if s.userQuery != nil {
					if clearErr := s.userQuery.ClearJwcBinding(ctx, uid); clearErr != nil {
						log.Printf("[syncUserCourses] 清除用户 %d 绑定信息失败: %v", uid, clearErr)
					} else {
						log.Printf("[syncUserCourses] 已清除用户 %d 的教务系统绑定", uid)
					}
				}
				logs = append(logs, s.createErrorLog(task.TaskID, uid, "course", fmt.Sprintf("term:%s-week:%d auth_error", currentTerm, week), err))
				return logs, true
			}
			logs = append(logs, s.createErrorLog(task.TaskID, uid, "course", fmt.Sprintf("term:%s-week:%d", currentTerm, week), err))
			continue
		}

		for _, daySchedule := range schedule.Days {
			for _, remote := range daySchedule.Courses {
				key := fmt.Sprintf("%s-%d-%d-%d-%s", currentTerm, week, remote.Weekday, remote.StartPeriod, remote.Name)
				remoteCourseMap[key] = true

				local, exists := localCourseMap[key]

				newCourse := &Course{
					Uid:            uid,
					Term:           currentTerm,
					Week:           week,
					Weekday:        remote.Weekday,
					Name:           remote.Name,
					Teacher:        remote.Teacher,
					Classroom:      remote.Classroom,
					StartPeriod:    remote.StartPeriod,
					EndPeriod:      remote.EndPeriod,
					SyncVersion:    newVersion,
					LastSyncTaskID: task.TaskID,
				}
				now := time.Now()
				newCourse.LastSyncAt = &now

				if !exists {
					task.NewRecords++
					logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionInsert, nil, newCourse, true, ""))
				} else if s.courseChanged(local, newCourse) {
					task.UpdatedRecords++
					logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionUpdate, local, newCourse, true, ""))
				} else {
					task.UnchangedRecords++
					newCourse.ID = local.ID
				}

				courses = append(courses, newCourse)
			}
		}
	}

	// 标记删除的记录
	for key, local := range localCourseMap {
		if local.Term == currentTerm && !remoteCourseMap[key] {
			task.DeletedRecords++
			logs = append(logs, s.createLog(task.TaskID, uid, "course", key, SyncActionDelete, local, nil, true, ""))
		}
	}

	// 批量更新数据库
	if err := s.repo.BatchUpsertCourses(ctx, courses); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "course", "", err))
		return logs, false
	}

	// 更新用户同步状态
	if err := s.repo.UpdateCourseSyncStatus(ctx, uid, task.TaskID, newVersion); err != nil {
		logs = append(logs, s.createErrorLog(task.TaskID, uid, "course_sync_status", "", err))
	}

	return logs, false
}

// GetTask 获取任务详情
func (s *service) GetTask(ctx context.Context, taskID string) (*TaskDetailResponse, error) {
	task, err := s.repo.GetTaskByID(ctx, taskID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errors.NewAppError(errors.CodeNotFound, "任务不存在")
		}
		return nil, fmt.Errorf("获取任务失败: %w", err)
	}

	// 获取日志（分页，默认最近100条）
	logs, err := s.repo.GetLogsByTaskID(ctx, taskID, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("获取任务日志失败: %w", err)
	}

	return &TaskDetailResponse{
		Task: task,
		Logs: logs,
	}, nil
}

// ListTasks 获取任务列表
func (s *service) ListTasks(ctx context.Context, limit, offset int) ([]*SyncTask, int64, error) {
	return s.repo.ListTasks(ctx, limit, offset)
}

// GetUserSyncStatus 获取用户同步状态
func (s *service) GetUserSyncStatus(ctx context.Context, uid int) (*UserSyncStatusResponse, error) {
	status, err := s.repo.GetUserSyncStatus(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("获取用户同步状态失败: %w", err)
	}

	// 构建响应
	resp := &UserSyncStatusResponse{
		Uid: uid,
		GradeStatus: &DataTypeSyncStatus{
			LastSyncAt:  status.GradeLastSyncAt,
			LastTaskID:  status.GradeLastSyncTaskID,
			SyncVersion: status.GradeSyncVersion,
		},
		RegularGradeStatus: &DataTypeSyncStatus{
			LastSyncAt:  status.RegularGradeLastSyncAt,
			LastTaskID:  status.RegularGradeLastSyncTaskID,
			SyncVersion: status.RegularGradeSyncVersion,
		},
		ExamStatus: &DataTypeSyncStatus{
			LastSyncAt:  status.ExamLastSyncAt,
			LastTaskID:  status.ExamLastSyncTaskID,
			SyncVersion: status.ExamSyncVersion,
		},
		LevelExamStatus: &DataTypeSyncStatus{
			LastSyncAt:  status.LevelExamLastSyncAt,
			LastTaskID:  status.LevelExamLastSyncTaskID,
			SyncVersion: status.LevelExamSyncVersion,
		},
		CourseStatus: &DataTypeSyncStatus{
			LastSyncAt:  status.CourseLastSyncAt,
			LastTaskID:  status.CourseLastSyncTaskID,
			SyncVersion: status.CourseSyncVersion,
		},
	}

	// 获取记录数量
	grades, _ := s.repo.GetGradesByUid(ctx, uid)
	resp.GradeStatus.RecordCount = int64(len(grades))

	regularGrades, _ := s.repo.GetRegularGradesByUid(ctx, uid)
	resp.RegularGradeStatus.RecordCount = int64(len(regularGrades))

	exams, _ := s.repo.GetExamsByUid(ctx, uid)
	resp.ExamStatus.RecordCount = int64(len(exams))

	levelExams, _ := s.repo.GetLevelExamsByUid(ctx, uid)
	resp.LevelExamStatus.RecordCount = int64(len(levelExams))

	courses, _ := s.repo.GetCoursesByUid(ctx, uid, "", 0)
	resp.CourseStatus.RecordCount = int64(len(courses))

	return resp, nil
}

// === 辅助方法 ===

// gradeChanged 检查成绩是否发生变化
func (s *service) gradeChanged(local *Grade, remote *Grade) bool {
	return local.Score != remote.Score ||
		local.Credit != remote.Credit ||
		local.Gpa != remote.Gpa ||
		local.Status != remote.Status ||
		local.Property != remote.Property ||
		local.CourseProperty != remote.CourseProperty ||
		local.Flag != remote.Flag ||
		local.Subject != remote.Subject
}

// regularGradeChanged 检查平时分是否发生变化
func (s *service) regularGradeChanged(local *RegularGrade, remote *RegularGrade) bool {
	return local.FinalExamScore != remote.FinalExamScore ||
		local.FinalExamRatio != remote.FinalExamRatio ||
		local.RegularScore != remote.RegularScore ||
		local.RegularRatio != remote.RegularRatio ||
		local.FinalScore != remote.FinalScore
}

// levelExamChanged 检查等级考试成绩是否发生变化
func (s *service) levelExamChanged(local *LevelExam, remote *LevelExam) bool {
	return local.LevGrade != remote.LevGrade ||
		local.No != remote.No
}

// examChanged 检查考试安排是否发生变化
func (s *service) examChanged(local *Exam, remote *Exam) bool {
	return local.Time != remote.Time ||
		local.Place != remote.Place ||
		local.Execution != remote.Execution ||
		local.SerialNo != remote.SerialNo
}

// courseChanged 检查课表是否发生变化
func (s *service) courseChanged(local *Course, remote *Course) bool {
	return local.Teacher != remote.Teacher ||
		local.Classroom != remote.Classroom ||
		local.StartPeriod != remote.StartPeriod ||
		local.EndPeriod != remote.EndPeriod ||
		local.Weekday != remote.Weekday
}

// createLog 创建同步日志
func (s *service) createLog(
	taskID string,
	uid int,
	dataType string,
	recordKey string,
	action SyncAction,
	before interface{},
	after interface{},
	status bool,
	errorMsg string,
) *SyncLog {
	log := &SyncLog{
		TaskID:    taskID,
		Uid:       uid,
		DataType:  dataType,
		Action:    action,
		RecordKey: recordKey,
		Status:    status,
		ErrorMsg:  errorMsg,
		CreatedAt: time.Now(),
	}

	if before != nil {
		if bytes, err := json.Marshal(before); err == nil {
			log.BeforeData = string(bytes)
		} else {
			log.BeforeData = "null"
		}
	} else {
		log.BeforeData = "null"
	}

	if after != nil {
		if bytes, err := json.Marshal(after); err == nil {
			log.AfterData = string(bytes)
		} else {
			log.AfterData = "null"
		}
	} else {
		log.AfterData = "null"
	}

	return log
}

// createErrorLog 创建错误日志
func (s *service) createErrorLog(taskID string, uid int, dataType string, recordKey string, err error) *SyncLog {
	return s.createLog(taskID, uid, dataType, recordKey, SyncActionSkip, nil, nil, false, err.Error())
}

// updateStudentGPARanking 更新学生GPA排名（累计 + 各学期 + 各学年）
func (s *service) updateStudentGPARanking(ctx context.Context, uid int, taskID string) {
	// 获取学生详细信息（学院、专业、年级、班级、姓名）
	userInfo, err := s.gradeService.GetUserGradeMajorClass(ctx, uid)
	if err != nil {
		return
	}

	// 获取所有成绩（使用不触发同步的方法，避免递归）
	grades, overallGPA, err := s.gradeService.GetAllGradesForSync(ctx, uid)
	if err != nil || overallGPA == nil || len(grades) == 0 {
		return
	}

	// 获取用户学号（从用户表获取）
	var sid string
	if s.userQuery != nil {
		if user, err := s.userQuery.GetUserByUid(ctx, uid); err == nil {
			sid = user.Sid
		}
	}

	// 构建基础信息（包含姓名）
	baseInfo := &ranking.StudentGPAData{
		Uid:     uid,
		Sid:     sid,
		Name:    userInfo.Name, // 从学籍卡片获取的姓名
		College: userInfo.Collage,
		Major:   userInfo.Major,
		Grade:   userInfo.Grade,
		Class:   userInfo.Class,
	}

	// 1. 保存累计GPA
	s.saveGPAData(ctx, baseInfo, grades, overallGPA, ranking.StatisticsTypeCumulative, "all")

	// 2. 按学期分组并保存各学期GPA
	termGrades := make(map[string][]grade.Grade)
	yearGrades := make(map[string][]grade.Grade)

	for _, g := range grades {
		if g.Term == "" {
			continue
		}
		termGrades[g.Term] = append(termGrades[g.Term], g)

		// 提取学年（如 2024-2025-1 -> 2024-2025）
		if len(g.Term) >= 9 {
			year := g.Term[:9]
			yearGrades[year] = append(yearGrades[year], g)
		}
	}

	// 保存各学期GPA
	for term, termGradeList := range termGrades {
		termGPA := s.calculateGPAFromGrades(termGradeList)
		s.saveGPAData(ctx, baseInfo, termGradeList, termGPA, ranking.StatisticsTypeSemester, term)
	}

	// 3. 保存各学年GPA
	for year, yearGradeList := range yearGrades {
		yearGPA := s.calculateGPAFromGrades(yearGradeList)
		s.saveGPAData(ctx, baseInfo, yearGradeList, yearGPA, "year", year)
	}
}

// saveGPAData 保存GPA数据
func (s *service) saveGPAData(ctx context.Context, baseInfo *ranking.StudentGPAData, grades []grade.Grade, gpa *grade.GPA, statisticsType, statisticsTerm string) {
	if gpa == nil {
		return
	}

	// 计算学分和课程数
	var totalCredit float64
	completedCourses := 0
	for _, g := range grades {
		if g.Status == 0 && g.Flag != "缓考" {
			totalCredit += g.Credit
			completedCourses++
		}
	}

	gpaData := &ranking.StudentGPAData{
		Uid:              baseInfo.Uid,
		Sid:              baseInfo.Sid,
		Name:             baseInfo.Name,
		College:          baseInfo.College,
		Major:            baseInfo.Major,
		Grade:            baseInfo.Grade,
		Class:            baseInfo.Class,
		GPA:              gpa.AverageGPA,
		AvgScore:         gpa.AverageScore,
		TotalCredit:      totalCredit,
		CompletedCourses: completedCourses,
	}

	s.rankingService.UpdateStudentGPA(ctx, gpaData, statisticsType, statisticsTerm)
}

// calculateGPAFromGrades 从成绩列表计算GPA（简化版，复用grade模块的计算逻辑）
func (s *service) calculateGPAFromGrades(grades []grade.Grade) *grade.GPA {
	if len(grades) == 0 {
		return nil
	}

	// 去重
	distinct := s.distinctGrades(grades)

	var (
		sumScore   float64
		sumGp      float64
		sumCredit  float64
		num2       int
		sumScore2  float64
		sumCredit2 float64
	)

	for _, g := range distinct {
		// 只算"课程属性 kcsx=必修"的课程（公选/任选/通识等课程属性不计，见 grade.IsAwardEligible）
		if !grade.IsAwardEligible(g) {
			continue
		}
		// 跳过缓考
		if g.Flag == "缓考" {
			continue
		}

		scoreText := g.Score

		// BasicPoint
		if g.Status == 0 {
			gradeD := s.mapGradeToScoreForBasic(scoreText)
			sumScore2 += gradeD * g.Credit
			sumCredit2 += g.Credit
		}

		// GPA & APF
		numericScore, isNum := s.parseNumeric(scoreText)

		if isNum && g.Status == 0 && numericScore >= 59.9 {
			sumScore += numericScore
			gp := s.getCourseGp(g, scoreText)
			sumGp += gp * g.Credit
			sumCredit += g.Credit
			num2++
		} else if g.Status == 0 && !isNum {
			gp := s.getCourseGp(g, scoreText)
			score := gp*10.0 + 50.0
			sumScore += score
			sumGp += gp * g.Credit
			sumCredit += g.Credit
			num2++
		} else if g.Status == 1 && isNum && numericScore >= 59.9 {
			sumScore += 60.0
			gp := s.getCourseGp(g, scoreText)
			sumGp += gp * 1.0
			sumCredit += g.Credit
			num2++
		} else if g.Status == 1 && !isNum && (scoreText == "及格" || scoreText == "合格") {
			gp := s.getCourseGp(g, scoreText)
			sumScore += 60.0
			sumGp += gp * 1.0
			sumCredit += g.Credit
			num2++
		} else if g.Status == 1 && !isNum && (scoreText == "不及格" || scoreText == "不合格") {
			sumCredit += g.Credit
			num2++
		} else if g.Status == 1 && isNum && numericScore <= 59.9 {
			sumCredit += g.Credit
			num2++
		} else {
			sumCredit += g.Credit
			num2++
			if isNum {
				sumScore += numericScore
			}
		}
	}

	var gpa, apf, basic float64
	if sumCredit != 0 {
		gpa = sumGp / sumCredit
	}
	if num2 != 0 {
		apf = sumScore / float64(num2)
	}
	if sumCredit2 != 0 {
		basic = sumScore2 / sumCredit2
	}

	return &grade.GPA{
		AverageGPA:   s.round3(gpa),
		AverageScore: s.round3(apf),
		BasicScore:   s.round3(basic),
	}
}

// distinctGrades 去重成绩
func (s *service) distinctGrades(grades []grade.Grade) []grade.Grade {
	m := make(map[string]grade.Grade)
	for _, g := range grades {
		key := g.SerialNo + "|" + g.Code + "|" + g.Term
		if existing, exists := m[key]; exists {
			if existing.Flag == "缓考" && g.Flag != "缓考" {
				m[key] = g
			}
		} else {
			m[key] = g
		}
	}
	res := make([]grade.Grade, 0, len(m))
	for _, g := range m {
		res = append(res, g)
	}
	return res
}

// mapGradeToScoreForBasic 映射成绩到基本分
func (s *service) mapGradeToScoreForBasic(scoreText string) float64 {
	switch scoreText {
	case "不及格", "不合格":
		return 50.0
	case "及格", "合格":
		return 60.0
	case "中":
		return 70.0
	case "良":
		return 80.0
	case "优":
		return 90.0
	default:
		if v, ok := s.parseNumeric(scoreText); ok {
			return v
		}
		return 0
	}
}

// parseNumeric 解析数字成绩
func (s *service) parseNumeric(str string) (float64, bool) {
	var v float64
	_, err := fmt.Sscanf(str, "%f", &v)
	return v, err == nil
}

// getCourseGp 获取课程绩点
func (s *service) getCourseGp(g grade.Grade, scoreText string) float64 {
	if g.Gpa > 0 {
		return g.Gpa
	}
	return s.handelGp(scoreText)
}

// handelGp 计算绩点
func (s *service) handelGp(scoreText string) float64 {
	switch scoreText {
	case "不及格", "不合格":
		return 0
	case "及格", "合格":
		return 1.0
	case "中":
		return 2.0
	case "良":
		return 3.0
	case "优":
		return 4.0
	}

	score, ok := s.parseNumeric(scoreText)
	if !ok {
		return 0
	}

	raw := (score - 50.0) / 10.0
	raw = s.round3(raw)
	if raw <= 0.1 {
		return 0
	}
	return raw
}

// round3 四舍五入保留3位小数
func (s *service) round3(v float64) float64 {
	return float64(int(v*1000+0.5)) / 1000
}

// SyncAllBoundUsers 同步所有已绑定用户（管理员专用）
func (s *service) SyncAllBoundUsers(ctx context.Context, boundUsers []BoundUserInfo, taskType TaskType, triggerType TriggerType) (*SyncTask, error) {
	if len(boundUsers) == 0 {
		return nil, fmt.Errorf("没有需要同步的用户")
	}

	// 提取 uid 列表
	uids := make([]int, len(boundUsers))
	for i, u := range boundUsers {
		uids[i] = u.Uid
	}

	// 创建同步任务
	task := &SyncTask{
		TaskID:      uuid.New().String(),
		TaskType:    taskType,
		TriggerType: triggerType,
		Status:      TaskStatusPending,
		TotalUsers:  len(uids),
	}

	if err := s.repo.CreateTask(ctx, task); err != nil {
		return nil, fmt.Errorf("创建同步任务失败: %w", err)
	}

	// 异步执行同步任务
	go s.executeSyncTask(context.Background(), task, uids)

	return task, nil
}

// TriggerGradeSync 异步触发成绩同步（用户查询成绩时调用，不阻塞）
func (s *service) TriggerGradeSync(ctx context.Context, uid int) {
	// 异步执行，不记录任务日志（轻量级同步）
	go func() {
		bgCtx := context.Background()
		// 直接调用同步方法，不创建完整的任务
		s.SyncUser(bgCtx, uid, TaskTypeGrade, TriggerTypeAuto)
	}()
}

// isAuthenticationError 判断是否是认证相关错误（登录失败等）
func (s *service) isAuthenticationError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否是 AppError
	if appErr, ok := err.(*errors.AppError); ok {
		switch appErr.Code {
		case errors.CodeJwcLoginFailed, // 登录失败
			errors.CodeJwcNotBound,  // 未绑定
			errors.CodeUnauthorized: // 未授权
			return true
		}
	}

	// 检查错误信息是否包含登录相关关键字
	errMsg := err.Error()
	authKeywords := []string{
		"登录失败",
		"密码错误",
		"账号被锁",
		"未绑定",
		"认证失败",
		"用户名或密码",
	}
	for _, keyword := range authKeywords {
		if strings.Contains(errMsg, keyword) {
			return true
		}
	}

	return false
}
