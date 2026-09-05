package evaluation

// EvaluationInfo 教评信息
type EvaluationInfo struct {
	TaskId       string `json:"task_id"`       // 任务ID
	TaskName     string `json:"task_name"`     // 任务名称
	CourseName   string `json:"course_name"`   // 课程名称
	TeacherName  string `json:"teacher_name"`  // 教师名称
	Status       string `json:"status"`        // 状态（已评、未评）
	EvaluateType string `json:"evaluate_type"` // 评价类型
	BeginTime    string `json:"begin_time"`    // 开始时间
	EndTime      string `json:"end_time"`      // 结束时间
}

// EvaluationAPIResponse 教评API响应
type EvaluationAPIResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		List []struct {
			TaskId       string `json:"taskId"`
			TaskName     string `json:"taskName"`
			CourseName   string `json:"courseName"`
			TeacherName  string `json:"teacherName"`
			Status       int    `json:"status"` // 0-未评 1-已评
			EvaluateType string `json:"evaluateType"`
			BeginTime    string `json:"beginTime"`
			EndTime      string `json:"endTime"`
		} `json:"list"`
		Total int `json:"total"`
	} `json:"data"`
}

// ============ 新增数据结构 ============

// EvaluationTask 教评任务
type EvaluationTask struct {
	TaskId            int    `json:"taskid"`
	TaskName          string `json:"taskname"`
	StartTime         string `json:"starttime"`
	EndTime           string `json:"endtime"`
	CurrentStatus     string `json:"currentStatus"`     // 进行中/已结束
	SfpjwcStatus      string `json:"sfpjwc"`            // 已评课程数
	EvalCoursesNumber string `json:"evalcoursesnumber"` // 总课程数
	QzqmTask          string `json:"qzqmtask"`          // 期中/期末
	YearTerm          int    `json:"yearterm"`
	IndexId           string `json:"indexid"` // 评教指标体系ID
}

// EvaluationTaskResponse 教评任务响应
type EvaluationTaskResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		PageData []EvaluationTask `json:"pageData"`
	} `json:"data"`
}

// EvaluationCourse 待评教课程
type EvaluationCourse struct {
	Id            int    `json:"id"`
	CourseName    string `json:"coursename"`
	TeacherName   string `json:"teachername"`
	JobNumber     string `json:"jobnumber"`
	CourseCode    string `json:"coursecode"`
	ClassNo       string `json:"classno"`
	PjCourseType  string `json:"pjcoursetype"` // 理论课/实验课
	HasSubmit     int    `json:"hassubmit"`    // 0-未评 1-已评
	CourseOrgName string `json:"courseorgname"`
	CourseOrgCode string `json:"courseorgcode"`
	StudentId     string `json:"studentid"`
	StudentName   string `json:"studentname"`
	YearTerm      int    `json:"yearterm"`
}

// EvaluationCoursesResponse 待评教课程响应
type EvaluationCoursesResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		PageData []EvaluationCourse `json:"pageData"`
	} `json:"data"`
}

// EvaluationQuestion 评教题目
type EvaluationQuestion struct {
	IndexId        int     `json:"indexid"`
	Title          string  `json:"title"`
	Type           string  `json:"type"`           // 打分题/问答题
	Score          float64 `json:"score"`          // 满分
	FirstLevlIndex string  `json:"firstlevlindex"` // 一级指标
	Ordor          int     `json:"ordor"`          // 题目顺序
	IsEmptyed      string  `json:"isemptyed"`      // 是否必填: 是/否
	IsScored       string  `json:"isscored"`       // 是否打分: 是/否
}

// EvaluationQuestionsResponse 评教题目响应
type EvaluationQuestionsResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		PageData []EvaluationQuestion `json:"pageData"`
	} `json:"data"`
}

// EvaluationSubmitRequest 提交评教请求
type EvaluationSubmitRequest struct {
	TaskId         int                `json:"taskid"`
	ClassNo        string             `json:"classno"`
	CourseCode     string             `json:"coursecode"`
	CourseName     string             `json:"coursename"`
	JobNumber      string             `json:"jobnumber"`
	StudentId      string             `json:"studentid"`
	StudentName    string             `json:"studentname"`
	TeacherName    string             `json:"teachername"`
	YearTerm       int                `json:"yearterm"`
	TotalScore     int                `json:"totalscore"`
	PjCourseType   string             `json:"pjcoursetype"`
	CourseOrgCode  string             `json:"courseorgcode"`
	CourseOrgName  string             `json:"courseorgname"`
	EvaluateResult []EvaluationAnswer `json:"evaluateResult"`
	CommitTime     string             `json:"commit_time"`
}

// EvaluationAnswer 评教答案
type EvaluationAnswer struct {
	IndexOrder int    `json:"index_order"` // 题目顺序
	Sfbt       string `json:"sfbt"`        // 是否必填
	Yjzb       string `json:"yjzb"`        // 一级指标
	IndexType  string `json:"index_type"`  // 题目类型
	IndexScore string `json:"index_score"` // 得分(打分题)
	IndexTitle string `json:"index_title"` // 答案文本
	IndexId    int    `json:"indexid"`     // 题目ID
}

// EvaluationSubmitResponse 提交评教响应
type EvaluationSubmitResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// AutoEvaluationResult 自动评教结果
type AutoEvaluationResult struct {
	TotalCourses     int      `json:"total_courses"`     // 总课程数
	EvaluatedCourses int      `json:"evaluated_courses"` // 已评课程数
	SuccessCourses   int      `json:"success_courses"`   // 成功评教课程数
	FailedCourses    int      `json:"failed_courses"`    // 失败课程数
	SkippedCourses   int      `json:"skipped_courses"`   // 跳过课程数(已评)
	SuccessList      []string `json:"success_list"`      // 成功课程名称列表
	FailedList       []string `json:"failed_list"`       // 失败课程名称列表
	SkippedList      []string `json:"skipped_list"`      // 跳过课程名称列表(之前已评)
	Message          string   `json:"message"`           // 总体结果消息
}

// EvaluationStatus 评教状态
type EvaluationStatus struct {
	TotalTasks         int                `json:"total_tasks"`         // 总任务数
	OngoingTasks       int                `json:"ongoing_tasks"`       // 进行中的任务数
	TotalCourses       int                `json:"total_courses"`       // 总课程数
	EvaluatedCourses   int                `json:"evaluated_courses"`   // 已评课程数
	UnevaluatedCourses int                `json:"unevaluated_courses"` // 未评课程数
	EvaluatedList      []CourseInfo       `json:"evaluated_list"`      // 已评课程列表
	UnevaluatedList    []CourseInfo       `json:"unevaluated_list"`    // 未评课程列表
	TaskDetails        []TaskStatusDetail `json:"task_details"`        // 各任务详情
}

// CourseInfo 课程信息(用于状态显示)
type CourseInfo struct {
	TaskId       int    `json:"task_id"`      // 任务ID
	TaskName     string `json:"task_name"`    // 任务名称
	CourseName   string `json:"course_name"`  // 课程名称
	TeacherName  string `json:"teacher_name"` // 教师名称
	PjCourseType string `json:"pjcoursetype"` // 课程类型
	HasSubmit    int    `json:"has_submit"`   // 是否已评(0-未评 1-已评)
}

// TaskStatusDetail 任务状态详情
type TaskStatusDetail struct {
	TaskId             int    `json:"task_id"`             // 任务ID
	TaskName           string `json:"task_name"`           // 任务名称
	CurrentStatus      string `json:"current_status"`      // 当前状态
	TotalCourses       int    `json:"total_courses"`       // 该任务总课程数
	EvaluatedCourses   int    `json:"evaluated_courses"`   // 该任务已评课程数
	UnevaluatedCourses int    `json:"unevaluated_courses"` // 该任务未评课程数
}
