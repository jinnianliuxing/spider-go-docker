package coursetips

import (
	"context"

	"gorm.io/gorm"
)

// Repository 选课提示数据仓储接口
type Repository interface {
	GetPEGradesWithTeacher(ctx context.Context, courseName string) ([]GradeWithTeacher, error)
}

// repository 实现
type repository struct {
	db *gorm.DB
}

// NewRepository 创建仓储实例
func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

// GetPEGradesWithTeacher 关联查询成绩和教师信息
// 通过 grades 表 JOIN courses 表获取教师信息
// courses 表仅存储当前学期的课表数据，因此放宽关联条件：
// 按 uid + course_name 关联（不限制 term），从 courses 中取任意一条教师记录
// 仅查询 is_deleted = 0 的有效记录
func (r *repository) GetPEGradesWithTeacher(ctx context.Context, courseName string) ([]GradeWithTeacher, error) {
	var results []GradeWithTeacher

	sql := `
		SELECT g.score, c.teacher
		FROM grades g
		INNER JOIN (
			SELECT DISTINCT uid, name, teacher
			FROM courses
			WHERE name = ? AND teacher != '' AND is_deleted = 0
		) c ON g.uid = c.uid AND g.subject = c.name
		WHERE g.subject = ? AND g.is_deleted = 0
	`

	err := r.db.WithContext(ctx).Raw(sql, courseName, courseName).Scan(&results).Error
	if err != nil {
		return nil, err
	}

	return results, nil
}
