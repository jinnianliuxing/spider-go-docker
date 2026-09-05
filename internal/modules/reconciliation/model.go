package reconciliation

import "time"

// TaskType 同步任务类型
type TaskType string

const (
	TaskTypeAll          TaskType = "all"           // 全量同步
	TaskTypeGrade        TaskType = "grade"         // 成绩同步
	TaskTypeRegularGrade TaskType = "regular_grade" // 平时分同步
	TaskTypeExam         TaskType = "exam"          // 考试同步
	TaskTypeLevelExam    TaskType = "level_exam"    // 等级考试同步
	TaskTypeCourse       TaskType = "course"        // 课表同步
)

// TriggerType 触发类型
type TriggerType string

const (
	TriggerTypeManual    TriggerType = "manual"    // 手动触发
	TriggerTypeScheduled TriggerType = "scheduled" // 定时触发
	TriggerTypeAuto      TriggerType = "auto"      // 自动触发（首次绑定等）
)

// TaskStatus 任务状态
type TaskStatus int

const (
	TaskStatusPending    TaskStatus = 0 // 待执行
	TaskStatusProcessing TaskStatus = 1 // 执行中
	TaskStatusSuccess    TaskStatus = 2 // 成功
	TaskStatusFailed     TaskStatus = 3 // 失败
)

// SyncAction 同步操作
type SyncAction string

const (
	SyncActionInsert SyncAction = "insert" // 新增
	SyncActionUpdate SyncAction = "update" // 更新
	SyncActionDelete SyncAction = "delete" // 删除
	SyncActionSkip   SyncAction = "skip"   // 跳过
)

// SyncTask 数据同步任务
type SyncTask struct {
	ID               int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID           string      `gorm:"type:varchar(64);uniqueIndex;not null" json:"task_id"`
	TaskType         TaskType    `gorm:"type:varchar(32);not null;index" json:"task_type"`
	TriggerType      TriggerType `gorm:"type:varchar(32);not null" json:"trigger_type"`
	Status           TaskStatus  `gorm:"type:tinyint;not null;default:0;index" json:"status"`
	TotalUsers       int         `gorm:"type:int;default:0" json:"total_users"`
	ProcessedUsers   int         `gorm:"type:int;default:0" json:"processed_users"`
	SuccessUsers     int         `gorm:"type:int;default:0" json:"success_users"`
	FailedUsers      int         `gorm:"type:int;default:0" json:"failed_users"`
	SkippedUsers     int         `gorm:"type:int;default:0" json:"skipped_users"`
	NewRecords       int         `gorm:"type:int;default:0" json:"new_records"`
	UpdatedRecords   int         `gorm:"type:int;default:0" json:"updated_records"`
	DeletedRecords   int         `gorm:"type:int;default:0" json:"deleted_records"`
	UnchangedRecords int         `gorm:"type:int;default:0" json:"unchanged_records"`
	StartTime        *time.Time  `json:"start_time"`
	EndTime          *time.Time  `json:"end_time"`
	ErrorMsg         string      `gorm:"type:text" json:"error_msg,omitempty"`
	CreatedAt        time.Time   `gorm:"index" json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

func (*SyncTask) TableName() string {
	return "sync_tasks"
}

// SyncLog 同步日志
type SyncLog struct {
	ID         int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID     string     `gorm:"type:varchar(64);not null;index" json:"task_id"`
	Uid        int        `gorm:"type:int;not null;index" json:"uid"`
	DataType   string     `gorm:"type:varchar(32);not null;index" json:"data_type"`
	Action     SyncAction `gorm:"type:varchar(20);not null" json:"action"`
	RecordKey  string     `gorm:"type:varchar(255)" json:"record_key"`
	BeforeData string     `gorm:"type:json" json:"before_data,omitempty"`
	AfterData  string     `gorm:"type:json" json:"after_data,omitempty"`
	Status     bool       `gorm:"type:tinyint;not null" json:"status"`
	ErrorMsg   string     `gorm:"type:text" json:"error_msg,omitempty"`
	CreatedAt  time.Time  `gorm:"index" json:"created_at"`
}

func (*SyncLog) TableName() string {
	return "sync_logs"
}

// Grade 成绩表
type Grade struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid            int        `gorm:"type:int;not null;uniqueIndex:uk_uid_term_code;index" json:"uid"`
	SerialNo       string     `gorm:"type:varchar(50)" json:"serial_no"`
	Term           string     `gorm:"type:varchar(20);not null;uniqueIndex:uk_uid_term_code;index" json:"term"`
	Code           string     `gorm:"type:varchar(50);not null;uniqueIndex:uk_uid_term_code" json:"code"`
	Subject        string     `gorm:"type:varchar(255);not null" json:"subject"`
	Score          string     `gorm:"type:varchar(20)" json:"score"`
	Credit         float64    `gorm:"type:decimal(5,2)" json:"credit"`
	Gpa            float64    `gorm:"type:decimal(5,3)" json:"gpa"`
	Status         int        `gorm:"type:tinyint" json:"status"`
	Property       string     `gorm:"type:varchar(20)" json:"property"`
	CourseProperty string     `gorm:"type:varchar(50)" json:"course_property"`
	Flag           string     `gorm:"type:varchar(20)" json:"flag"`
	SyncVersion    int        `gorm:"type:int;default:1;index" json:"sync_version"`
	LastSyncAt     *time.Time `gorm:"index" json:"last_sync_at"`
	LastSyncTaskID string     `gorm:"type:varchar(64)" json:"last_sync_task_id"`
	IsDeleted      bool       `gorm:"type:tinyint;default:0" json:"is_deleted"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (*Grade) TableName() string {
	return "grades"
}

// RegularGrade 平时分表
type RegularGrade struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid            int        `gorm:"type:int;not null;uniqueIndex:uk_uid_term_code;index" json:"uid"`
	Term           string     `gorm:"type:varchar(20);not null;uniqueIndex:uk_uid_term_code;index" json:"term"`
	Code           string     `gorm:"type:varchar(50);not null;uniqueIndex:uk_uid_term_code" json:"code"`
	Subject        string     `gorm:"type:varchar(255)" json:"subject"`
	FinalExamScore string     `gorm:"type:varchar(20)" json:"final_exam_score"`
	FinalExamRatio string     `gorm:"type:varchar(20)" json:"final_exam_ratio"`
	RegularScore   string     `gorm:"type:varchar(20)" json:"regular_score"`
	RegularRatio   string     `gorm:"type:varchar(20)" json:"regular_ratio"`
	FinalScore     string     `gorm:"type:varchar(20)" json:"final_score"`
	SyncVersion    int        `gorm:"type:int;default:1" json:"sync_version"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastSyncTaskID string     `gorm:"type:varchar(64)" json:"last_sync_task_id"`
	IsDeleted      bool       `gorm:"type:tinyint;default:0" json:"is_deleted"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (*RegularGrade) TableName() string {
	return "regular_grades"
}

// Exam 考试安排表
type Exam struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid            int        `gorm:"type:int;not null;uniqueIndex:uk_uid_term_course;index" json:"uid"`
	Term           string     `gorm:"type:varchar(20);not null;uniqueIndex:uk_uid_term_course;index" json:"term"`
	SerialNo       string     `gorm:"type:varchar(50)" json:"serial_no"`
	ClassNo        string     `gorm:"type:varchar(50)" json:"class_no"`
	ClassName      string     `gorm:"type:varchar(255);not null;uniqueIndex:uk_uid_term_course" json:"class_name"`
	Time           string     `gorm:"type:varchar(100)" json:"time"`
	Place          string     `gorm:"type:varchar(255)" json:"place"`
	Execution      string     `gorm:"type:varchar(50)" json:"execution"`
	SyncVersion    int        `gorm:"type:int;default:1" json:"sync_version"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastSyncTaskID string     `gorm:"type:varchar(64)" json:"last_sync_task_id"`
	IsDeleted      bool       `gorm:"type:tinyint;default:0" json:"is_deleted"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (*Exam) TableName() string {
	return "exams"
}

// LevelExam 等级考试表
type LevelExam struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid            int        `gorm:"type:int;not null;uniqueIndex:uk_uid_course_time;index" json:"uid"`
	No             string     `gorm:"type:varchar(50)" json:"no"`
	CourseName     string     `gorm:"type:varchar(255);not null;uniqueIndex:uk_uid_course_time" json:"CourseName"`
	LevGrade       string     `gorm:"type:varchar(50)" json:"LevelGrade"`
	Time           string     `gorm:"type:varchar(100);uniqueIndex:uk_uid_course_time" json:"Time"`
	SyncVersion    int        `gorm:"type:int;default:1" json:"sync_version"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastSyncTaskID string     `gorm:"type:varchar(64)" json:"last_sync_task_id"`
	IsDeleted      bool       `gorm:"type:tinyint;default:0" json:"is_deleted"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (*LevelExam) TableName() string {
	return "level_exams"
}

// Course 课表
type Course struct {
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid            int        `gorm:"type:int;not null;index:idx_uid_term_week" json:"uid"`
	Term           string     `gorm:"type:varchar(20);not null;index:idx_uid_term_week" json:"term"`
	Week           int        `gorm:"type:int;not null;index:idx_uid_term_week" json:"week"`
	Name           string     `gorm:"type:varchar(255);not null" json:"name"`
	Teacher        string     `gorm:"type:varchar(100)" json:"teacher"`
	Classroom      string     `gorm:"type:varchar(255)" json:"classroom"`
	Weekday        int        `gorm:"type:tinyint" json:"weekday"`
	StartPeriod    int        `gorm:"type:tinyint" json:"start_period"`
	EndPeriod      int        `gorm:"type:tinyint" json:"end_period"`
	WeekRange      string     `gorm:"type:varchar(100)" json:"week_range"`
	SyncVersion    int        `gorm:"type:int;default:1" json:"sync_version"`
	LastSyncAt     *time.Time `json:"last_sync_at"`
	LastSyncTaskID string     `gorm:"type:varchar(64)" json:"last_sync_task_id"`
	IsDeleted      bool       `gorm:"type:tinyint;default:0" json:"is_deleted"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (*Course) TableName() string {
	return "courses"
}

// UserSyncStatus 用户同步状态表
type UserSyncStatus struct {
	ID                         int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Uid                        int        `gorm:"type:int;uniqueIndex;not null" json:"uid"`
	GradeLastSyncAt            *time.Time `json:"grade_last_sync_at"`
	GradeLastSyncTaskID        string     `gorm:"type:varchar(64)" json:"grade_last_sync_task_id"`
	GradeSyncVersion           int        `gorm:"type:int;default:0" json:"grade_sync_version"`
	RegularGradeLastSyncAt     *time.Time `json:"regular_grade_last_sync_at"`
	RegularGradeLastSyncTaskID string     `gorm:"type:varchar(64)" json:"regular_grade_last_sync_task_id"`
	RegularGradeSyncVersion    int        `gorm:"type:int;default:0" json:"regular_grade_sync_version"`
	ExamLastSyncAt             *time.Time `json:"exam_last_sync_at"`
	ExamLastSyncTaskID         string     `gorm:"type:varchar(64)" json:"exam_last_sync_task_id"`
	ExamSyncVersion            int        `gorm:"type:int;default:0" json:"exam_sync_version"`
	LevelExamLastSyncAt        *time.Time `json:"level_exam_last_sync_at"`
	LevelExamLastSyncTaskID    string     `gorm:"type:varchar(64)" json:"level_exam_last_sync_task_id"`
	LevelExamSyncVersion       int        `gorm:"type:int;default:0" json:"level_exam_sync_version"`
	CourseLastSyncAt           *time.Time `json:"course_last_sync_at"`
	CourseLastSyncTaskID       string     `gorm:"type:varchar(64)" json:"course_last_sync_task_id"`
	CourseSyncVersion          int        `gorm:"type:int;default:0" json:"course_sync_version"`
	CreatedAt                  time.Time  `json:"created_at"`
	UpdatedAt                  time.Time  `json:"updated_at"`
}

func (*UserSyncStatus) TableName() string {
	return "user_sync_status"
}

// CreateTaskRequest 创建同步任务请求
type CreateTaskRequest struct {
	TaskType TaskType `json:"task_type" binding:"required"`
	UserIDs  []int    `json:"user_ids"` // 可选，为空则同步所有用户
}

// TaskDetailResponse 任务详情响应
type TaskDetailResponse struct {
	Task *SyncTask  `json:"task"`
	Logs []*SyncLog `json:"logs,omitempty"`
}

// UserSyncStatusResponse 用户同步状态响应
type UserSyncStatusResponse struct {
	Uid                int                 `json:"uid"`
	GradeStatus        *DataTypeSyncStatus `json:"grade_status"`
	RegularGradeStatus *DataTypeSyncStatus `json:"regular_grade_status"`
	ExamStatus         *DataTypeSyncStatus `json:"exam_status"`
	LevelExamStatus    *DataTypeSyncStatus `json:"level_exam_status"`
	CourseStatus       *DataTypeSyncStatus `json:"course_status"`
}

// DataTypeSyncStatus 单个数据类型的同步状态
type DataTypeSyncStatus struct {
	LastSyncAt  *time.Time `json:"last_sync_at"`
	LastTaskID  string     `json:"last_task_id"`
	SyncVersion int        `json:"sync_version"`
	RecordCount int64      `json:"record_count"` // 记录总数
}

// BoundUserInfo 已绑定用户信息（用于管理员批量同步）
type BoundUserInfo struct {
	Uid  int    `json:"uid"`
	Sid  string `json:"sid"`
	Spwd string `json:"spwd"`
}

// AdminSyncAllRequest 管理员同步所有用户请求
type AdminSyncAllRequest struct {
	TaskType TaskType `json:"task_type" binding:"required"`
}
