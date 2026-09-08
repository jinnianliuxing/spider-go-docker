package errors

// 错误码定义
const (
	CodeSuccess           = 0     // 成功
	CodeInvalidParams     = 40000 // 参数错误
	CodeUnauthorized      = 40100 // 未授权
	CodeInvalidToken      = 40101 // Token无效
	CodeForbidden         = 40300 // 禁止访问
	CodeUserNotFound      = 40400 // 用户不存在
	CodeNotFound          = 40404 // 资源不存在
	CodeInvalidPassword   = 40100 // 密码错误
	CodeUserAlreadyExists = 40900 // 用户已存在
	CodeCaptchaInvalid    = 40001 // 验证码错误
	CodeInternalError     = 50000 // 内部错误
	CodeJwcInvalidParams  = 40002 // 教务系统参数错误
	CodeJwcNotBound       = 40003 // 教务系统未绑定
	CodeJwcLoginFailed    = 40004 // 教务系统登录失败（密码错误、账号锁定等）
	CodeJwcParseFailed    = 40005 // 教务系统解析失败
	CodeJwcRequestFailed  = 40006 // 教务系统请求失败
	CodeJwcNoRegularGrade = 40007 // 该课程没有平时分数据
	CodeJwcLoginTimeout   = 40010 // 教务系统登录超时（网络问题、服务器响应慢）
	CodeJwcNotEvaluated   = 40009 // 未完成教评
	CodeCacheError        = 50001 // 缓存错误
	CodeWeChatLoginFailed = 60001 // 微信登录失败
	CodeWeChatBindFailed  = 60002 // 微信绑定失败
	CodeWeChatAlreadyBind = 60003 // 微信已被绑定
	CodeBindLimitExceeded = 40008 // 绑定次数超限
	CodeJwcMFARequired    = 40011 // 需要多因素认证（MFA）

	// 新增错误码
	CodeDatabaseError      = 50002 // 数据库错误
	CodeConfigError        = 50003 // 配置错误
	CodeRedisError         = 50004 // Redis错误
	CodeEmailError         = 50005 // 邮件发送错误
	CodeAdminNotFound      = 40401 // 管理员不存在
	CodeNoticeNotFound     = 40402 // 通知不存在
	CodeWeChatBindNotFound = 60004 // 微信绑定不存在
	CodeHttpRequestFailed  = 50010 // HTTP请求失败
	CodeInvalidResponse    = 50011 // 响应格式错误
	CodeNotImplemented     = 50100 // 功能未实现
	CodeUnbindCooldown     = 40012 // 注销冷却期未到
	CodeJwcBindExpired     = 40013 // 教务绑定已失效（会话过期或密码已变更），需重新输入教务密码
	CodeJwcSessionExpired  = 40014 // 教务会话已失效（HTTP 401：登录态过期/被踢出），需重新登录

)

// AppError 应用错误
type AppError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error 实现 error 接口
func (e *AppError) Error() string {
	return e.Message
}

// NewAppError 创建应用错误
func NewAppError(code int, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// IsAppError 判断是否为应用错误
func IsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	appErr, ok := err.(*AppError)
	return appErr, ok
}
