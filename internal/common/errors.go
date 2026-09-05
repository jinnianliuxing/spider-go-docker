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
)

// AppError 重新导出 pkg/errors 的类型（保持向后兼容）
type AppError = pkgerrors.AppError

// NewAppError 重新导出（保持向后兼容）
var NewAppError = pkgerrors.NewAppError
