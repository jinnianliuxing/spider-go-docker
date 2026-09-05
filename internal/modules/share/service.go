package share

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"spider-go/internal/common"
	"spider-go/internal/modules/course"
	"spider-go/internal/shared"
)

// Service 分享服务接口
type Service interface {
	CreateShareToken(ctx context.Context, uid int, req *CreateShareRequest) (*CreateShareResponse, error)
	GetSharedCourse(ctx context.Context, token string) (*SharedCourseResponse, error)
}

type shareService struct {
	userQuery     shared.UserQuery
	courseService course.Service
}

// NewService 创建分享服务
func NewService(userQuery shared.UserQuery, courseService course.Service) Service {
	return &shareService{
		userQuery:     userQuery,
		courseService: courseService,
	}
}

func (s *shareService) CreateShareToken(ctx context.Context, uid int, req *CreateShareRequest) (*CreateShareResponse, error) {
	startWk, endWk, err := resolveWeekRange(req)
	if err != nil {
		return nil, err
	}

	token := ShareToken{Uid: uid, Term: req.Term, StartWk: startWk, EndWk: endWk}
	data, err := json.Marshal(token)
	if err != nil {
		return nil, common.NewAppError(common.CodeInternalError, "生成分享token失败")
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	return &CreateShareResponse{Token: encoded}, nil
}

func (s *shareService) GetSharedCourse(ctx context.Context, token string) (*SharedCourseResponse, error) {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, common.NewAppError(common.CodeInvalidParams, "无效的分享token")
	}

	var st ShareToken
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, common.NewAppError(common.CodeInvalidParams, "无效的分享token")
	}

	if st.Uid <= 0 || st.Term == "" || st.StartWk < 1 || st.EndWk > 20 || st.StartWk > st.EndWk {
		return nil, common.NewAppError(common.CodeInvalidParams, "分享参数无效")
	}

	u, err := s.userQuery.GetUserByUid(ctx, st.Uid)
	if err != nil {
		return nil, common.NewAppError(common.CodeUserNotFound, "用户不存在")
	}

	// 单周直接返回对象，多周返回数组
	if st.StartWk == st.EndWk {
		schedule, err := s.courseService.GetCourseTableByWeek(ctx, st.Uid, st.StartWk, st.Term)
		if err != nil {
			return nil, err
		}
		return &SharedCourseResponse{
			UserName:  u.Name,
			Term:      st.Term,
			StartWeek: st.StartWk,
			EndWeek:   st.EndWk,
			Schedule:  schedule,
		}, nil
	}

	// 多周：逐周获取
	var schedules []interface{}
	for wk := st.StartWk; wk <= st.EndWk; wk++ {
		schedule, err := s.courseService.GetCourseTableByWeek(ctx, st.Uid, wk, st.Term)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}

	return &SharedCourseResponse{
		UserName:  u.Name,
		Term:      st.Term,
		StartWeek: st.StartWk,
		EndWeek:   st.EndWk,
		Schedule:  schedules,
	}, nil
}

// resolveWeekRange 解析请求中的周次参数
func resolveWeekRange(req *CreateShareRequest) (int, int, error) {
	hasWeek := req.Week != nil
	hasRange := req.StartWeek != nil && req.EndWeek != nil

	if hasWeek && hasRange {
		return 0, 0, common.NewAppError(common.CodeInvalidParams, "week和start_week/end_week不能同时指定")
	}
	if !hasWeek && !hasRange {
		return 0, 0, common.NewAppError(common.CodeInvalidParams, "请指定week或start_week+end_week")
	}

	if hasWeek {
		w := *req.Week
		return w, w, nil
	}

	start, end := *req.StartWeek, *req.EndWeek
	if start > end {
		return 0, 0, common.NewAppError(common.CodeInvalidParams, fmt.Sprintf("start_week(%d)不能大于end_week(%d)", start, end))
	}
	return start, end, nil
}
