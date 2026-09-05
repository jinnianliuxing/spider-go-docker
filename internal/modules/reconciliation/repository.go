package reconciliation

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 对账数据仓储接口
type Repository interface {
	// 任务相关
	CreateTask(ctx context.Context, task *SyncTask) error
	GetTaskByID(ctx context.Context, taskID string) (*SyncTask, error)
	UpdateTask(ctx context.Context, task *SyncTask) error
	ListTasks(ctx context.Context, limit, offset int) ([]*SyncTask, int64, error)

	// 日志相关
	CreateLog(ctx context.Context, log *SyncLog) error
	BatchCreateLogs(ctx context.Context, logs []*SyncLog) error
	GetLogsByTaskID(ctx context.Context, taskID string, limit, offset int) ([]*SyncLog, error)

	// 成绩相关
	UpsertGrade(ctx context.Context, grade *Grade) error
	BatchUpsertGrades(ctx context.Context, grades []*Grade) error
	GetGradesByUid(ctx context.Context, uid int) ([]*Grade, error)
	GetGradesByUidAndTerm(ctx context.Context, uid int, term string) ([]*Grade, error)
	DeleteGradesByUidAndTerm(ctx context.Context, uid int, term string) error

	// 平时分相关
	UpsertRegularGrade(ctx context.Context, rg *RegularGrade) error
	BatchUpsertRegularGrades(ctx context.Context, rgs []*RegularGrade) error
	GetRegularGradesByUid(ctx context.Context, uid int) ([]*RegularGrade, error)
	GetRegularGradeByUidTermCode(ctx context.Context, uid int, term, code string) (*RegularGrade, error)

	// 考试相关
	UpsertExam(ctx context.Context, exam *Exam) error
	BatchUpsertExams(ctx context.Context, exams []*Exam) error
	GetExamsByUid(ctx context.Context, uid int) ([]*Exam, error)
	GetExamsByUidAndTerm(ctx context.Context, uid int, term string) ([]*Exam, error)

	// 等级考试相关
	UpsertLevelExam(ctx context.Context, exam *LevelExam) error
	BatchUpsertLevelExams(ctx context.Context, exams []*LevelExam) error
	GetLevelExamsByUid(ctx context.Context, uid int) ([]*LevelExam, error)

	// 课表相关
	UpsertCourse(ctx context.Context, course *Course) error
	BatchUpsertCourses(ctx context.Context, courses []*Course) error
	GetCoursesByUid(ctx context.Context, uid int, term string, week int) ([]*Course, error)

	// 用户同步状态相关
	GetUserSyncStatus(ctx context.Context, uid int) (*UserSyncStatus, error)
	UpsertUserSyncStatus(ctx context.Context, status *UserSyncStatus) error
	UpdateGradeSyncStatus(ctx context.Context, uid int, taskID string, version int) error
	UpdateRegularGradeSyncStatus(ctx context.Context, uid int, taskID string, version int) error
	UpdateExamSyncStatus(ctx context.Context, uid int, taskID string, version int) error
	UpdateLevelExamSyncStatus(ctx context.Context, uid int, taskID string, version int) error
	UpdateCourseSyncStatus(ctx context.Context, uid int, taskID string, version int) error

	// 清理相关
	DeleteOldSyncTasks(ctx context.Context, before time.Time, batchSize int) (int64, error)
	DeleteOldSyncLogs(ctx context.Context, before time.Time, batchSize int) (int64, error)

	// 优化表
	OptimizeSyncTables(ctx context.Context) error
}

// repository 实现
type repository struct {
	db *gorm.DB
}

// NewRepository 创建仓储实例
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// ============= 任务相关 =============

func (r *repository) CreateTask(ctx context.Context, task *SyncTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *repository) GetTaskByID(ctx context.Context, taskID string) (*SyncTask, error) {
	var task SyncTask
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *repository) UpdateTask(ctx context.Context, task *SyncTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *repository) ListTasks(ctx context.Context, limit, offset int) ([]*SyncTask, int64, error) {
	var tasks []*SyncTask
	var total int64

	err := r.db.WithContext(ctx).Model(&SyncTask{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = r.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&tasks).Error

	return tasks, total, err
}

// ============= 日志相关 =============

func (r *repository) CreateLog(ctx context.Context, log *SyncLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

func (r *repository) BatchCreateLogs(ctx context.Context, logs []*SyncLog) error {
	if len(logs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(logs, 100).Error
}

func (r *repository) GetLogsByTaskID(ctx context.Context, taskID string, limit, offset int) ([]*SyncLog, error) {
	var logs []*SyncLog
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error
	return logs, err
}

// ============= 成绩相关 =============

func (r *repository) UpsertGrade(ctx context.Context, grade *Grade) error {
	return r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND code = ?", grade.Uid, grade.Term, grade.Code).
		Assign(grade).
		FirstOrCreate(grade).Error
}

func (r *repository) BatchUpsertGrades(ctx context.Context, grades []*Grade) error {
	if len(grades) == 0 {
		return nil
	}

	// 使用 MySQL ON DUPLICATE KEY UPDATE 批量 upsert
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "uid"}, {Name: "term"}, {Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"serial_no", "subject", "score", "credit", "gpa", "status",
			"property", "course_property", "flag", "sync_version", "last_sync_at", "last_sync_task_id",
			"is_deleted", "updated_at",
		}),
	}).Create(grades).Error
}

func (r *repository) GetGradesByUid(ctx context.Context, uid int) ([]*Grade, error) {
	var grades []*Grade
	err := r.db.WithContext(ctx).
		Where("uid = ? AND is_deleted = ?", uid, false).
		Order("term DESC, code ASC").
		Find(&grades).Error
	return grades, err
}

func (r *repository) GetGradesByUidAndTerm(ctx context.Context, uid int, term string) ([]*Grade, error) {
	var grades []*Grade
	err := r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND is_deleted = ?", uid, term, false).
		Order("code ASC").
		Find(&grades).Error
	return grades, err
}

func (r *repository) DeleteGradesByUidAndTerm(ctx context.Context, uid int, term string) error {
	return r.db.WithContext(ctx).
		Model(&Grade{}).
		Where("uid = ? AND term = ?", uid, term).
		Update("is_deleted", true).Error
}

// ============= 平时分相关 =============

func (r *repository) UpsertRegularGrade(ctx context.Context, rg *RegularGrade) error {
	return r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND code = ?", rg.Uid, rg.Term, rg.Code).
		Assign(rg).
		FirstOrCreate(rg).Error
}

func (r *repository) BatchUpsertRegularGrades(ctx context.Context, rgs []*RegularGrade) error {
	if len(rgs) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, rg := range rgs {
			if err := tx.Where("uid = ? AND term = ? AND code = ?", rg.Uid, rg.Term, rg.Code).
				Assign(rg).
				FirstOrCreate(rg).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *repository) GetRegularGradesByUid(ctx context.Context, uid int) ([]*RegularGrade, error) {
	var rgs []*RegularGrade
	err := r.db.WithContext(ctx).
		Where("uid = ? AND is_deleted = ?", uid, false).
		Order("term DESC, code ASC").
		Find(&rgs).Error
	return rgs, err
}

func (r *repository) GetRegularGradeByUidTermCode(ctx context.Context, uid int, term, code string) (*RegularGrade, error) {
	var rg RegularGrade
	err := r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND code = ? AND is_deleted = ?", uid, term, code, false).
		First(&rg).Error
	if err != nil {
		return nil, err
	}
	return &rg, nil
}

// ============= 考试相关 =============

func (r *repository) UpsertExam(ctx context.Context, exam *Exam) error {
	return r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND class_name = ?", exam.Uid, exam.Term, exam.ClassName).
		Assign(exam).
		FirstOrCreate(exam).Error
}

func (r *repository) BatchUpsertExams(ctx context.Context, exams []*Exam) error {
	if len(exams) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, exam := range exams {
			if err := tx.Where("uid = ? AND term = ? AND class_name = ?", exam.Uid, exam.Term, exam.ClassName).
				Assign(exam).
				FirstOrCreate(exam).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *repository) GetExamsByUid(ctx context.Context, uid int) ([]*Exam, error) {
	var exams []*Exam
	err := r.db.WithContext(ctx).
		Where("uid = ? AND is_deleted = ?", uid, false).
		Order("term DESC, time ASC").
		Find(&exams).Error
	return exams, err
}

func (r *repository) GetExamsByUidAndTerm(ctx context.Context, uid int, term string) ([]*Exam, error) {
	var exams []*Exam
	err := r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND is_deleted = ?", uid, term, false).
		Order("time ASC").
		Find(&exams).Error
	return exams, err
}

// ============= 等级考试相关 =============

func (r *repository) UpsertLevelExam(ctx context.Context, exam *LevelExam) error {
	return r.db.WithContext(ctx).
		Where("uid = ? AND course_name = ? AND time = ?", exam.Uid, exam.CourseName, exam.Time).
		Assign(exam).
		FirstOrCreate(exam).Error
}

func (r *repository) BatchUpsertLevelExams(ctx context.Context, exams []*LevelExam) error {
	if len(exams) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, exam := range exams {
			if err := tx.Where("uid = ? AND course_name = ? AND time = ?", exam.Uid, exam.CourseName, exam.Time).
				Assign(exam).
				FirstOrCreate(exam).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *repository) GetLevelExamsByUid(ctx context.Context, uid int) ([]*LevelExam, error) {
	var exams []*LevelExam
	err := r.db.WithContext(ctx).
		Where("uid = ? AND is_deleted = ?", uid, false).
		Order("time DESC").
		Find(&exams).Error
	return exams, err
}

// ============= 课表相关 =============

func (r *repository) UpsertCourse(ctx context.Context, course *Course) error {
	// 课表使用组合条件判断唯一性
	return r.db.WithContext(ctx).
		Where("uid = ? AND term = ? AND week = ? AND name = ? AND weekday = ? AND start_period = ?",
			course.Uid, course.Term, course.Week, course.Name, course.Weekday, course.StartPeriod).
		Assign(course).
		FirstOrCreate(course).Error
}

func (r *repository) BatchUpsertCourses(ctx context.Context, courses []*Course) error {
	if len(courses) == 0 {
		return nil
	}

	// 按 uid 分组处理（同一用户的课表一起处理）
	uidCourses := make(map[int][]*Course)
	for _, course := range courses {
		uidCourses[course.Uid] = append(uidCourses[course.Uid], course)
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for uid, userCourses := range uidCourses {
			if len(userCourses) == 0 {
				continue
			}

			// 获取该用户该学期的所有现有课表
			term := userCourses[0].Term
			var existingCourses []*Course
			if err := tx.Where("uid = ? AND term = ?", uid, term).Find(&existingCourses).Error; err != nil {
				return err
			}

			// 建立已存在课表的映射 (key: uid-term-week-name-weekday-startPeriod)
			existingMap := make(map[string]*Course)
			for _, c := range existingCourses {
				key := r.courseKey(c)
				existingMap[key] = c
			}

			// 分离需要插入和更新的记录
			var toInsert []*Course
			var toUpdate []*Course

			for _, course := range userCourses {
				key := r.courseKey(course)
				if existing, ok := existingMap[key]; ok {
					// 已存在，需要更新，保留原有的 ID 和 CreatedAt
					course.ID = existing.ID
					course.CreatedAt = existing.CreatedAt
					toUpdate = append(toUpdate, course)
				} else {
					// 不存在，需要插入
					toInsert = append(toInsert, course)
				}
			}

			// 批量插入新记录
			if len(toInsert) > 0 {
				if err := tx.CreateInBatches(toInsert, 100).Error; err != nil {
					return err
				}
			}

			// 批量更新已有记录
			for _, course := range toUpdate {
				if err := tx.Save(course).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// courseKey 生成课表唯一标识
func (r *repository) courseKey(c *Course) string {
	return fmt.Sprintf("%d-%s-%d-%s-%d-%d", c.Uid, c.Term, c.Week, c.Name, c.Weekday, c.StartPeriod)
}

func (r *repository) GetCoursesByUid(ctx context.Context, uid int, term string, week int) ([]*Course, error) {
	var courses []*Course
	query := r.db.WithContext(ctx).Where("uid = ? AND is_deleted = ?", uid, false)

	if term != "" {
		query = query.Where("term = ?", term)
	}
	if week > 0 {
		query = query.Where("week = ?", week)
	}

	err := query.Order("weekday ASC, start_period ASC").Find(&courses).Error
	return courses, err
}

// ============= 用户同步状态相关 =============

func (r *repository) GetUserSyncStatus(ctx context.Context, uid int) (*UserSyncStatus, error) {
	var status UserSyncStatus
	err := r.db.WithContext(ctx).Where("uid = ?", uid).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		// 如果不存在，创建一个新的
		status = UserSyncStatus{Uid: uid}
		if err := r.db.WithContext(ctx).Create(&status).Error; err != nil {
			return nil, err
		}
		return &status, nil
	}
	if err != nil {
		return nil, err
	}
	return &status, nil
}

func (r *repository) UpsertUserSyncStatus(ctx context.Context, status *UserSyncStatus) error {
	return r.db.WithContext(ctx).
		Where("uid = ?", status.Uid).
		Assign(status).
		FirstOrCreate(status).Error
}

func (r *repository) UpdateGradeSyncStatus(ctx context.Context, uid int, taskID string, version int) error {
	now := time.Now()

	// 先确保记录存在
	var status UserSyncStatus
	err := r.db.WithContext(ctx).Where("uid = ?", uid).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		// 记录不存在，创建新记录
		status = UserSyncStatus{
			Uid:                 uid,
			GradeLastSyncAt:     &now,
			GradeLastSyncTaskID: taskID,
			GradeSyncVersion:    version,
		}
		return r.db.WithContext(ctx).Create(&status).Error
	}
	if err != nil {
		return err
	}

	// 记录存在，更新字段
	return r.db.WithContext(ctx).Model(&UserSyncStatus{}).
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"grade_last_sync_at":      now,
			"grade_last_sync_task_id": taskID,
			"grade_sync_version":      version,
		}).Error
}

func (r *repository) UpdateRegularGradeSyncStatus(ctx context.Context, uid int, taskID string, version int) error {
	now := time.Now()

	var status UserSyncStatus
	err := r.db.WithContext(ctx).Where("uid = ?", uid).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		status = UserSyncStatus{
			Uid:                        uid,
			RegularGradeLastSyncAt:     &now,
			RegularGradeLastSyncTaskID: taskID,
			RegularGradeSyncVersion:    version,
		}
		return r.db.WithContext(ctx).Create(&status).Error
	}
	if err != nil {
		return err
	}

	return r.db.WithContext(ctx).Model(&UserSyncStatus{}).
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"regular_grade_last_sync_at":      now,
			"regular_grade_last_sync_task_id": taskID,
			"regular_grade_sync_version":      version,
		}).Error
}

func (r *repository) UpdateExamSyncStatus(ctx context.Context, uid int, taskID string, version int) error {
	now := time.Now()

	var status UserSyncStatus
	err := r.db.WithContext(ctx).Where("uid = ?", uid).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		status = UserSyncStatus{
			Uid:                uid,
			ExamLastSyncAt:     &now,
			ExamLastSyncTaskID: taskID,
			ExamSyncVersion:    version,
		}
		return r.db.WithContext(ctx).Create(&status).Error
	}
	if err != nil {
		return err
	}

	return r.db.WithContext(ctx).Model(&UserSyncStatus{}).
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"exam_last_sync_at":      now,
			"exam_last_sync_task_id": taskID,
			"exam_sync_version":      version,
		}).Error
}

func (r *repository) UpdateLevelExamSyncStatus(ctx context.Context, uid int, taskID string, version int) error {
	now := time.Now()

	var status UserSyncStatus
	err := r.db.WithContext(ctx).Where("uid = ?", uid).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		status = UserSyncStatus{
			Uid:                     uid,
			LevelExamLastSyncAt:     &now,
			LevelExamLastSyncTaskID: taskID,
			LevelExamSyncVersion:    version,
		}
		return r.db.WithContext(ctx).Create(&status).Error
	}
	if err != nil {
		return err
	}

	return r.db.WithContext(ctx).Model(&UserSyncStatus{}).
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"level_exam_last_sync_at":      now,
			"level_exam_last_sync_task_id": taskID,
			"level_exam_sync_version":      version,
		}).Error
}

func (r *repository) UpdateCourseSyncStatus(ctx context.Context, uid int, taskID string, version int) error {
	now := time.Now()

	var status UserSyncStatus
	err := r.db.WithContext(ctx).Where("uid = ?", uid).First(&status).Error
	if err == gorm.ErrRecordNotFound {
		status = UserSyncStatus{
			Uid:                  uid,
			CourseLastSyncAt:     &now,
			CourseLastSyncTaskID: taskID,
			CourseSyncVersion:    version,
		}
		return r.db.WithContext(ctx).Create(&status).Error
	}
	if err != nil {
		return err
	}

	return r.db.WithContext(ctx).Model(&UserSyncStatus{}).
		Where("uid = ?", uid).
		Updates(map[string]interface{}{
			"course_last_sync_at":      now,
			"course_last_sync_task_id": taskID,
			"course_sync_version":      version,
		}).Error
}

// DeleteOldSyncTasks 删除指定时间之前的已完成同步任务，返回删除数量
func (r *repository) DeleteOldSyncTasks(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(
		"DELETE FROM sync_tasks WHERE created_at < ? AND status IN (2, 3) LIMIT ?",
		before, batchSize,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// DeleteOldSyncLogs 删除指定时间之前的同步日志（仅删除关联任务已完成或已删除的），返回删除数量
func (r *repository) DeleteOldSyncLogs(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(
		"DELETE FROM sync_logs WHERE created_at < ? AND (task_id NOT IN (SELECT task_id FROM sync_tasks WHERE status IN (0, 1))) LIMIT ?",
		before, batchSize,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// OptimizeSyncTables 对 sync_tasks 和 sync_logs 表执行 OPTIMIZE TABLE
func (r *repository) OptimizeSyncTables(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec("OPTIMIZE TABLE sync_tasks, sync_logs").Error
}
