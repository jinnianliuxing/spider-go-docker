package coursetips

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"spider-go/internal/common"

	"github.com/redis/go-redis/v9"
)

const (
	cacheKeyPrefix = "course_tips:"
	cacheTTL       = 24 * time.Hour
)

// Service 选课提示服务接口
type Service interface {
	GetTeacherStats(ctx context.Context, courseName string) (*TeacherStatsResponse, error)
}

type service struct {
	repo        Repository
	redisClient *redis.Client
}

// NewService 创建选课提示服务
func NewService(repo Repository, redisClient *redis.Client) Service {
	return &service{
		repo:        repo,
		redisClient: redisClient,
	}
}

// GetTeacherStats 获取体育选修课教师统计数据
func (s *service) GetTeacherStats(ctx context.Context, courseName string) (*TeacherStatsResponse, error) {
	// 1. 校验课程名称
	if !IsValidPECourse(courseName) {
		return nil, common.NewAppError(common.CodeInvalidParams, "不支持的课程名称，仅支持体育选项课Ⅰ/Ⅱ/Ⅲ")
	}

	// 2. 查 Redis 缓存
	cacheKey := fmt.Sprintf("%s%s", cacheKeyPrefix, courseName)
	cached, err := s.getFromCache(ctx, cacheKey)
	if err == nil && cached != nil {
		return cached, nil
	}
	// Redis 读取失败静默降级，继续查库

	// 3. 缓存未命中，查数据库
	grades, err := s.repo.GetPEGradesWithTeacher(ctx, courseName)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "查询统计数据失败")
	}

	// 4. 聚合计算
	teachers := aggregateByTeacher(grades)

	result := &TeacherStatsResponse{
		CourseName: courseName,
		Teachers:   teachers,
	}

	// 5. 写入缓存（失败静默降级）
	_ = s.setToCache(ctx, cacheKey, result)

	return result, nil
}

// getFromCache 从 Redis 缓存读取统计数据
// 读取失败返回 error，调用方静默降级
func (s *service) getFromCache(ctx context.Context, key string) (*TeacherStatsResponse, error) {
	data, err := s.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var result TeacherStatsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// setToCache 将统计数据写入 Redis 缓存
// 写入失败返回 error，调用方静默降级
func (s *service) setToCache(ctx context.Context, key string, data *TeacherStatsResponse) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return s.redisClient.Set(ctx, key, bytes, cacheTTL).Err()
}

// aggregateByTeacher 按教师分组聚合成绩统计
func aggregateByTeacher(grades []GradeWithTeacher) []TeacherStats {
	// 按教师分组收集有效分数
	teacherScores := make(map[string][]float64)

	for _, g := range grades {
		score, ok := ParseScore(g.Score)
		if !ok {
			continue // 跳过无法解析的成绩
		}
		teacherScores[g.Teacher] = append(teacherScores[g.Teacher], score)
	}

	// 计算每位教师的统计数据
	var stats []TeacherStats
	for teacher, scores := range teacherScores {
		stats = append(stats, computeTeacherStats(teacher, scores))
	}

	// 过滤掉学生人数少于30的教师（数据异常，非体育老师的误关联）
	var filtered []TeacherStats
	for _, s := range stats {
		if s.StudentCount >= 30 {
			filtered = append(filtered, s)
		}
	}

	// 按教师名称排序，保证结果稳定
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].TeacherName < filtered[j].TeacherName
	})

	return filtered
}

// computeTeacherStats 计算单个教师的统计数据
func computeTeacherStats(teacher string, scores []float64) TeacherStats {
	count := len(scores)
	if count == 0 {
		return TeacherStats{TeacherName: teacher}
	}

	var sum float64
	maxScore := scores[0]
	minScore := scores[0]
	failCount := 0
	dist := ScoreDistribution{}

	for _, s := range scores {
		sum += s

		if s > maxScore {
			maxScore = s
		}
		if s < minScore {
			minScore = s
		}

		if s < 60 {
			failCount++
		}

		// 分数段分布
		switch {
		case s < 60:
			dist.Range0To59++
		case s < 70:
			dist.Range60To69++
		case s < 80:
			dist.Range70To79++
		case s < 90:
			dist.Range80To89++
		default:
			dist.Range90To100++
		}
	}

	avg := math.Round(sum/float64(count)*100) / 100
	failRate := math.Round(float64(failCount)/float64(count)*10000) / 10000

	return TeacherStats{
		TeacherName:       teacher,
		StudentCount:      count,
		AverageScore:      avg,
		MaxScore:          maxScore,
		MinScore:          minScore,
		FailRate:          failRate,
		ScoreDistribution: dist,
	}
}
