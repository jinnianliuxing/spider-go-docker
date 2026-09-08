package common

import pkgerrors "spider-go/pkg/errors"

// 重新导出 pkg/errors 的错误码(保持向后兼容)
const (
	CodeSuccess           = pkgerrors.CodeSuccess
	CodeInvalidParams     = pkgerrors.CodeInvalidParams
	CodeUnauthorized      = pkgerrors.CodeUnauthorized
	CodeInvalidToken      = pkgerrors.CodeInvalidToken
	CodeForbidden         = pkgerrors.CodeForbidden
	CodeUserNotFound      = pkgerrors.CodeUserNotFound
	CodeNotFound          = pkgerrors.CodeNotFound
	CodeInvalidPassword   = pkgerrors.CodeInvalidPassword
	CodeUserAlreadyExists = pkgerrors.CodeUserAlreadyExists
	CodeCaptchaInvalid    = pkgerrors.CodeCaptchaInvalid
	CodeInternalError     = pkgerrors.CodeInternalError
	CodeJwcInvalidParams  = pkgerrors.CodeJwcInvalidParams
	CodeJwcNotBound       = pkgerrors.CodeJwcNotBound
	CodeJwcLoginFailed    = pkgerrors.CodeJwcLoginFailed
	CodeJwcParseFailed    = pkgerrors.CodeJwcParseFailed
	CodeJwcRequestFailed  = pkgerrors.CodeJwcRequestFailed
	CodeJwcNoRegularGrade = pkgerrors.CodeJwcNoRegularGrade
	CodeJwcLoginTimeout   = pkgerrors.CodeJwcLoginTimeout
	CodeJwcNotEvaluated   = pkgerrors.CodeJwcNotEvaluated
	CodeBindLimitExceeded = pkgerrors.CodeBindLimitExceeded
	CodeJwcMFARequired    = pkgerrors.CodeJwcMFARequired
	CodeCacheError        = pkgerrors.CodeCacheError
	CodeWeChatLoginFailed = pkgerrors.CodeWeChatLoginFailed
	CodeWeChatBindFailed  = pkgerrors.CodeWeChatBindFailed
	CodeWeChatAlreadyBind = pkgerrors.CodeWeChatAlreadyBind

	// 新增错误码
	CodeDatabaseError      = pkgerrors.CodeDatabaseError
	CodeConfigError        = pkgerrors.CodeConfigError
	CodeRedisError         = pkgerrors.CodeRedisError
	CodeEmailError         = pkgerrors.CodeEmailError
	CodeAdminNotFound      = pkgerrors.CodeAdminNotFound
	CodeNoticeNotFound     = pkgerrors.CodeNoticeNotFound
	CodeWeChatBindNotFound = pkgerrors.CodeWeChatBindNotFound
	CodeHttpRequestFailed  = pkgerrors.CodeHttpRequestFailed
	CodeInvalidResponse    = pkgerrors.CodeInvalidResponse
	CodeNotImplemented     = pkgerrors.CodeNotImplemented
	CodeUnbindCooldown     = pkgerrors.CodeUnbindCooldown
	CodeJwcBindExpired     = pkgerrors.CodeJwcBindExpired
	CodeJwcSessionExpired  = pkgerrors.CodeJwcSessionExpired
)

// MsgJwcBindExpired 是"教务绑定已失效"的统一提示文案（前端据此弹出重新绑定弹窗）
const MsgJwcBindExpired = "教务系统绑定已失效（密码已变更或登录状态过期），请重新输入教务密码"

// ToBindExpired 把"教务系统认证类失败"统一转换为 CodeJwcBindExpired，
// 便于前端识别并弹出「重新输入教务密码」弹窗。
// 非认证类错误（超时、网络、解析失败、MFA、页改版等）原样返回，避免误判。
func ToBindExpired(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*AppError); ok {
		switch appErr.Code {
		case CodeJwcLoginFailed, CodeJwcNotBound, CodeJwcSessionExpired:
			return NewAppError(CodeJwcBindExpired, MsgJwcBindExpired)
		}
	}
	return err
}

// AppError 重新导出 pkg/errors 的类型（保持向后兼容）
type AppError = pkgerrors.AppError

// NewAppError 重新导出（保持向后兼容）
var NewAppError = pkgerrors.NewAppError
