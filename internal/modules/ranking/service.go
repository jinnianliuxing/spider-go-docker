package ranking

import (
	"context"
	"time"
)

// Service 排名服务接口
type Service interface {
	// UpdateStudentGPA 更新单个学生的GPA（在对账时调用）
	UpdateStudentGPA(ctx context.Context, data *StudentGPAData, statisticsType, statisticsTerm string) error

	// BatchUpdateGPAs 批量更新学生GPA
	BatchUpdateGPAs(ctx context.Context, dataList []*StudentGPAData, statisticsType, statisticsTerm string) error

	// GetMyRanking 获取我的排名
	GetMyRanking(ctx context.Context, uid int, statisticsType, statisticsTerm string) (*MyRankingResponse, error)

	// GetStudentRanking 查询排名（实时计算）- 保留用于内部调用
	GetStudentRanking(ctx context.Context, uid int, statisticsType, statisticsTerm string) (*RankingResponse, error)

	// GetCollegeStats 统计 - 保留用于内部调用
	GetCollegeStats(ctx context.Context, college, statisticsType, statisticsTerm string) (*CollegeRankingStats, error)
}

// service 实现
type service struct {
	repo Repository
}

// NewService 创建服务实例
func NewService(repo Repository) Service {
	return &service{repo: repo}
}

// UpdateStudentGPA 更新单个学生的GPA（只存储GPA，不计算排名）
func (s *service) UpdateStudentGPA(ctx context.Context, data *StudentGPAData, statisticsType, statisticsTerm string) error {
	now := time.Now()

	gpa := &StudentGPA{
		Uid:              data.Uid,
		Sid:              data.Sid,
		Name:             data.Name,
		College:          data.College,
		Major:            data.Major,
		Grade:            data.Grade,
		Class:            data.Class,
		GPA:              data.GPA,
		AvgScore:         data.AvgScore,
		TotalCredit:      data.TotalCredit,
		CompletedCourses: data.CompletedCourses,
		StatisticsType:   statisticsType,
		StatisticsTerm:   statisticsTerm,
		UpdatedAt:        now,
	}

	return s.repo.UpsertGPA(ctx, gpa)
}

// BatchUpdateGPAs 批量更新学生GPA
func (s *service) BatchUpdateGPAs(ctx context.Context, dataList []*StudentGPAData, statisticsType, statisticsTerm string) error {
	if len(dataList) == 0 {
		return nil
	}

	now := time.Now()
	gpas := make([]*StudentGPA, 0, len(dataList))

	for _, data := range dataList {
		gpa := &StudentGPA{
			Uid:              data.Uid,
			Sid:              data.Sid,
			Name:             data.Name,
			College:          data.College,
			Major:            data.Major,
			Grade:            data.Grade,
			Class:            data.Class,
			GPA:              data.GPA,
			AvgScore:         data.AvgScore,
			TotalCredit:      data.TotalCredit,
			CompletedCourses: data.CompletedCourses,
			StatisticsType:   statisticsType,
			StatisticsTerm:   statisticsTerm,
			UpdatedAt:        now,
		}
		gpas = append(gpas, gpa)
	}

	return s.repo.BatchUpsertGPAs(ctx, gpas)
}

// GetMyRanking 获取我的排名（只显示自己的排名，不显示他人信息）
func (s *service) GetMyRanking(ctx context.Context, uid int, statisticsType, statisticsTerm string) (*MyRankingResponse, error) {
	// 获取学生排名数据（排名实时计算）
	ranking, err := s.repo.GetRankingByUid(ctx, uid, statisticsType, statisticsTerm)
	if err != nil {
		return nil, err
	}

	return &MyRankingResponse{
		Name:           ranking.Name,
		College:        ranking.College,
		Major:          ranking.Major,
		Grade:          ranking.Grade,
		Class:          ranking.Class,
		GPA:            ranking.GPA,
		AvgScore:       ranking.AvgScore,
		CollegeRank:    ranking.CollegeRank,
		CollegeTotal:   ranking.CollegeTotal,
		MajorRank:      ranking.MajorRank,
		MajorTotal:     ranking.MajorTotal,
		StatisticsType: statisticsType,
		StatisticsTerm: statisticsTerm,
	}, nil
}

// GetStudentRanking 获取学生排名信息（实时计算）- 保留用于内部调用
func (s *service) GetStudentRanking(ctx context.Context, uid int, statisticsType, statisticsTerm string) (*RankingResponse, error) {
	// 获取学生排名数据（排名实时计算）
	ranking, err := s.repo.GetRankingByUid(ctx, uid, statisticsType, statisticsTerm)
	if err != nil {
		return nil, err
	}

	// 获取学院统计
	collegeStats, err := s.repo.GetCollegeStats(ctx, ranking.College, statisticsType, statisticsTerm)
	if err != nil {
		collegeStats = nil // 允许统计失败
	}

	// 获取专业统计
	majorStats, err := s.repo.GetMajorStats(ctx, ranking.College, ranking.Major, statisticsType, statisticsTerm)
	if err != nil {
		majorStats = nil
	}

	return &RankingResponse{
		Student:      ranking,
		CollegeStats: collegeStats,
		MajorStats:   majorStats,
	}, nil
}

// GetCollegeStats 获取学院统计
func (s *service) GetCollegeStats(ctx context.Context, college, statisticsType, statisticsTerm string) (*CollegeRankingStats, error) {
	return s.repo.GetCollegeStats(ctx, college, statisticsType, statisticsTerm)
}

// UpdateStudentRanking 兼容旧接口（内部调用 UpdateStudentGPA）
// Deprecated: 使用 UpdateStudentGPA 代替
func (s *service) UpdateStudentRanking(ctx context.Context, data *StudentGPAData, statisticsType, statisticsTerm string) error {
	return s.UpdateStudentGPA(ctx, data, statisticsType, statisticsTerm)
}
