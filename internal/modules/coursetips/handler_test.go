package coursetips

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"spider-go/internal/common"

	"github.com/gin-gonic/gin"
)

// mockService implements Service for handler tests
type mockService struct {
	result *TeacherStatsResponse
	err    error
}

func (m *mockService) GetTeacherStats(_ context.Context, _ string) (*TeacherStatsResponse, error) {
	return m.result, m.err
}

func setupRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(svc)
	h.RegisterRoutes(&r.RouterGroup)
	return r
}

func TestGetTeacherStats_MissingCourseName(t *testing.T) {
	r := setupRouter(&mockService{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/course-tips", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Code != common.CodeInvalidParams {
		t.Errorf("expected code %d, got %d", common.CodeInvalidParams, resp.Code)
	}
	if resp.Message != "课程名称参数不能为空" {
		t.Errorf("expected message '课程名称参数不能为空', got '%s'", resp.Message)
	}
}

func TestGetTeacherStats_ServiceAppError(t *testing.T) {
	svc := &mockService{
		err: common.NewAppError(common.CodeInvalidParams, "不支持的课程名称，仅支持体育选项课Ⅰ/Ⅱ/Ⅲ"),
	}
	r := setupRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/course-tips?course_name=invalid", nil)
	r.ServeHTTP(w, req)

	var resp common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Code != common.CodeInvalidParams {
		t.Errorf("expected code %d, got %d", common.CodeInvalidParams, resp.Code)
	}
}

func TestGetTeacherStats_Success(t *testing.T) {
	expected := &TeacherStatsResponse{
		CourseName: "体育选项课Ⅰ",
		Teachers: []TeacherStats{
			{
				TeacherName:  "张老师",
				StudentCount: 3,
				AverageScore: 80,
				MaxScore:     90,
				MinScore:     70,
				FailRate:     0,
				ScoreDistribution: ScoreDistribution{
					Range70To79:  1,
					Range80To89:  1,
					Range90To100: 1,
				},
			},
		},
	}
	svc := &mockService{result: expected}
	r := setupRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/course-tips?course_name=体育选项课Ⅰ", nil)
	r.ServeHTTP(w, req)

	var resp common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Code != common.CodeSuccess {
		t.Errorf("expected code %d, got %d", common.CodeSuccess, resp.Code)
	}
	if resp.Data == nil {
		t.Fatal("expected data in response, got nil")
	}
}

func TestGetTeacherStats_GenericError(t *testing.T) {
	svc := &mockService{
		err: &common.AppError{Code: common.CodeInternalError, Message: "查询统计数据失败"},
	}
	r := setupRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/course-tips?course_name=体育选项课Ⅰ", nil)
	r.ServeHTTP(w, req)

	var resp common.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Code != common.CodeInternalError {
		t.Errorf("expected code %d, got %d", common.CodeInternalError, resp.Code)
	}
}

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(&mockService{})
	group := r.Group("/api")
	h.RegisterRoutes(group)

	// Verify the route is registered by making a request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/course-tips", nil)
	r.ServeHTTP(w, req)

	// Should get a response (not 404), even if params are missing
	if w.Code == http.StatusNotFound {
		t.Error("expected route to be registered, got 404")
	}
}
