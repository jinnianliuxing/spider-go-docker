package ranking

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 排名数据仓储接口
type Repository interface {
	// 基础操作
	UpsertGPA(ctx context.Context, gpa *StudentGPA) error
	BatchUpsertGPAs(ctx context.Context, gpas []*StudentGPA) error

	// 查询（排名实时计算）
	GetRankingByUid(ctx context.Context, uid int, statisticsType, statisticsTerm string) (*StudentRankingView, error)
	GetRankingList(ctx context.Context, req *RankingListRequest) ([]*StudentRankingView, int64, error)

	// 统计
	GetCollegeStats(ctx context.Context, college, statisticsType, statisticsTerm string) (*CollegeRankingStats, error)
	GetMajorStats(ctx context.Context, college, major, statisticsType, statisticsTerm string) (*MajorRankingStats, error)
	GetAllColleges(ctx context.Context) ([]string, error)
}

// repository 实现
type repository struct {
	db *gorm.DB
}

// NewRepository 创建仓储实例
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// UpsertGPA 插入或更新GPA数据（使用 ON DUPLICATE KEY UPDATE）
func (r *repository) UpsertGPA(ctx context.Context, gpa *StudentGPA) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "uid"}, {Name: "statistics_type"}, {Name: "statistics_term"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sid", "name", "college", "major", "grade", "class",
			"gpa", "avg_score", "total_credit", "completed_courses",
			"updated_at",
		}),
	}).Create(gpa).Error
}

// BatchUpsertGPAs 批量插入或更新GPA数据（使用 ON DUPLICATE KEY UPDATE）
func (r *repository) BatchUpsertGPAs(ctx context.Context, gpas []*StudentGPA) error {
	if len(gpas) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "uid"}, {Name: "statistics_type"}, {Name: "statistics_term"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"sid", "name", "college", "major", "grade", "class",
			"gpa", "avg_score", "total_credit", "completed_courses",
			"updated_at",
		}),
	}).CreateInBatches(gpas, 100).Error
}

// GetRankingByUid 根据uid获取排名数据（实时计算排名，只与同年级比较，按学号去重）
func (r *repository) GetRankingByUid(ctx context.Context, uid int, statisticsType, statisticsTerm string) (*StudentRankingView, error) {
	var result StudentRankingView

	// 使用子查询实时计算排名（只与同年级的人比较，按学号去重）
	sql := `
		SELECT
			g.*,
			-- 学院排名（同年级，按学号去重）
			(SELECT COUNT(DISTINCT g2.sid) + 1 FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			   AND g2.sid != g.sid
			   AND (g2.gpa > g.gpa OR (g2.gpa = g.gpa AND g2.avg_score > g.avg_score))
			) as college_rank,
			-- 专业排名（同年级，按学号去重）
			(SELECT COUNT(DISTINCT g2.sid) + 1 FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			   AND g2.sid != g.sid
			   AND (g2.gpa > g.gpa OR (g2.gpa = g.gpa AND g2.avg_score > g.avg_score))
			) as major_rank,
			-- 年级排名（与专业排名相同，因为已经是同年级同专业，按学号去重）
			(SELECT COUNT(DISTINCT g2.sid) + 1 FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			   AND g2.sid != g.sid
			   AND (g2.gpa > g.gpa OR (g2.gpa = g.gpa AND g2.avg_score > g.avg_score))
			) as grade_rank,
			-- 班级排名（按学号去重）
			(SELECT COUNT(DISTINCT g2.sid) + 1 FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade AND g2.class = g.class
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			   AND g2.sid != g.sid
			   AND (g2.gpa > g.gpa OR (g2.gpa = g.gpa AND g2.avg_score > g.avg_score))
			) as class_rank,
			-- 总人数（同年级，按学号去重）
			(SELECT COUNT(DISTINCT g2.sid) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as college_total,
			(SELECT COUNT(DISTINCT g2.sid) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as major_total,
			(SELECT COUNT(DISTINCT g2.sid) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as grade_total,
			(SELECT COUNT(DISTINCT g2.sid) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade AND g2.class = g.class
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as class_total
		FROM student_gpas g
		WHERE g.uid = ? AND g.statistics_type = ? AND g.statistics_term = ?
	`

	err := r.db.WithContext(ctx).Raw(sql, uid, statisticsType, statisticsTerm).Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if result.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &result, nil
}

// GetRankingList 获取排名列表（实时计算排名）
func (r *repository) GetRankingList(ctx context.Context, req *RankingListRequest) ([]*StudentRankingView, int64, error) {
	var total int64

	// 构建基础查询条件
	baseQuery := r.db.WithContext(ctx).Model(&StudentGPA{}).
		Where("statistics_type = ?", req.StatisticsType)

	if req.StatisticsTerm != "" {
		baseQuery = baseQuery.Where("statistics_term = ?", req.StatisticsTerm)
	}
	if req.College != "" {
		baseQuery = baseQuery.Where("college = ?", req.College)
	}
	if req.Major != "" {
		baseQuery = baseQuery.Where("major = ?", req.Major)
	}
	if req.Grade != "" {
		baseQuery = baseQuery.Where("grade = ?", req.Grade)
	}
	if req.Class != "" {
		baseQuery = baseQuery.Where("class = ?", req.Class)
	}

	// 统计总数
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 构建带排名的查询
	offset := (req.Page - 1) * req.PageSize

	// 构建WHERE条件
	whereClause := "statistics_type = ?"
	args := []interface{}{req.StatisticsType}

	if req.StatisticsTerm != "" {
		whereClause += " AND statistics_term = ?"
		args = append(args, req.StatisticsTerm)
	}
	if req.College != "" {
		whereClause += " AND college = ?"
		args = append(args, req.College)
	}
	if req.Major != "" {
		whereClause += " AND major = ?"
		args = append(args, req.Major)
	}
	if req.Grade != "" {
		whereClause += " AND grade = ?"
		args = append(args, req.Grade)
	}
	if req.Class != "" {
		whereClause += " AND class = ?"
		args = append(args, req.Class)
	}

	// 使用窗口函数计算排名
	sql := `
		SELECT
			g.*,
			ROW_NUMBER() OVER (
				PARTITION BY g.college
				ORDER BY g.gpa DESC, g.avg_score DESC
			) as college_rank,
			ROW_NUMBER() OVER (
				PARTITION BY g.college, g.major
				ORDER BY g.gpa DESC, g.avg_score DESC
			) as major_rank,
			ROW_NUMBER() OVER (
				PARTITION BY g.college, g.major, g.grade
				ORDER BY g.gpa DESC, g.avg_score DESC
			) as grade_rank,
			ROW_NUMBER() OVER (
				PARTITION BY g.college, g.major, g.grade, g.class
				ORDER BY g.gpa DESC, g.avg_score DESC
			) as class_rank,
			(SELECT COUNT(*) FROM student_gpas g2
			 WHERE g2.college = g.college
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as college_total,
			(SELECT COUNT(*) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as major_total,
			(SELECT COUNT(*) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as grade_total,
			(SELECT COUNT(*) FROM student_gpas g2
			 WHERE g2.college = g.college AND g2.major = g.major AND g2.grade = g.grade AND g2.class = g.class
			   AND g2.statistics_type = g.statistics_type
			   AND g2.statistics_term = g.statistics_term
			) as class_total
		FROM student_gpas g
		WHERE ` + whereClause + `
		ORDER BY g.gpa DESC, g.avg_score DESC
		LIMIT ? OFFSET ?
	`

	args = append(args, req.PageSize, offset)

	var results []*StudentRankingView
	err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&results).Error

	return results, total, err
}

// GetCollegeStats 获取学院统计
func (r *repository) GetCollegeStats(ctx context.Context, college, statisticsType, statisticsTerm string) (*CollegeRankingStats, error) {
	var stats CollegeRankingStats
	stats.College = college

	query := r.db.WithContext(ctx).Model(&StudentGPA{}).
		Where("college = ? AND statistics_type = ? AND statistics_term = ?", college, statisticsType, statisticsTerm)

	// 基础统计
	row := query.Select("COUNT(*) as total_students, AVG(gpa) as avg_gpa, MAX(gpa) as max_gpa, MIN(gpa) as min_gpa").Row()
	if err := row.Scan(&stats.TotalStudents, &stats.AvgGPA, &stats.MaxGPA, &stats.MinGPA); err != nil {
		return nil, err
	}

	// 前10%平均GPA
	top10Count := int(float64(stats.TotalStudents) * 0.1)
	if top10Count > 0 {
		var top10AvgGPA float64
		err := r.db.WithContext(ctx).Model(&StudentGPA{}).
			Where("college = ? AND statistics_type = ? AND statistics_term = ?", college, statisticsType, statisticsTerm).
			Order("gpa DESC").
			Limit(top10Count).
			Select("AVG(gpa)").
			Scan(&top10AvgGPA).Error
		if err == nil {
			stats.Top10AvgGPA = top10AvgGPA
		}
	}

	return &stats, nil
}

// GetMajorStats 获取专业统计
func (r *repository) GetMajorStats(ctx context.Context, college, major, statisticsType, statisticsTerm string) (*MajorRankingStats, error) {
	var stats MajorRankingStats
	stats.Major = major

	query := r.db.WithContext(ctx).Model(&StudentGPA{}).
		Where("college = ? AND major = ? AND statistics_type = ? AND statistics_term = ?",
			college, major, statisticsType, statisticsTerm)

	row := query.Select("COUNT(*) as total_students, AVG(gpa) as avg_gpa, MAX(gpa) as max_gpa, MIN(gpa) as min_gpa").Row()
	if err := row.Scan(&stats.TotalStudents, &stats.AvgGPA, &stats.MaxGPA, &stats.MinGPA); err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetAllColleges 获取所有学院列表
func (r *repository) GetAllColleges(ctx context.Context) ([]string, error) {
	var colleges []string
	err := r.db.WithContext(ctx).Model(&StudentGPA{}).
		Distinct("college").
		Where("college != ''").
		Order("college ASC").
		Pluck("college", &colleges).Error
	return colleges, err
}
