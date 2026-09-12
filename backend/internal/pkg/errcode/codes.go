package errcode

// 错误码分段约定(04-coding-standards §2.2):
// 2xxxx 成功无需定义;4xxxx 客户端错误;5xxxx 服务端错误。
const (
	// ParamInvalid 40001: 请求参数非法。
	ParamInvalid = 40001
	// Unauthorized 40100: 未认证或凭证过期。
	Unauthorized = 40100
	// Forbidden 40300: 已认证但无权限。
	Forbidden = 40300
	// NotFound 40400: 通用资源不存在。
	NotFound = 40400
	// UserNotFound 40410: 用户不存在。
	UserNotFound = 40410
	// UserDisabled 40411: 用户已被禁用。
	UserDisabled = 40411
	// UserAlreadyExists 40412: 用户名已存在。
	UserAlreadyExists = 40412
	// ClusterNotFound 40401: 集群不存在。
	ClusterNotFound = 40401
	// ClusterKubeconfigBad 40402: kubeconfig 非法或无法解析。
	ClusterKubeconfigBad = 40402
	// ClusterKubeconfigMultiContext 40403: kubeconfig 含多个 context 且未显式指定。
	ClusterKubeconfigMultiContext = 40403
	// ClusterUnreachable 40404: 目标集群 API Server 不可达或凭证无效。
	ClusterUnreachable = 40404
	// ClusterAlreadyExists 40405: 同名集群已注册。
	ClusterAlreadyExists = 40405
	// ResourceConflict 40406: resourceVersion 冲突(并发修改),HTTP 409。
	ResourceConflict = 40406
	// DownloadCodeInvalid 40407: 一次性下载 code 无效、已使用或已过期。
	DownloadCodeInvalid = 40407
	// EnrollTokenInvalid 40101: Agent Enrollment Token 无效、过期或已吊销。
	EnrollTokenInvalid = 40101
	// IssuedTokenInvalid 40102: 签发的客户端短期 token 无效、过期或已撤销。
	IssuedTokenInvalid = 40102
	// RoleAlreadyExists 40413: 角色名已存在。
	RoleAlreadyExists = 40413
	// RoleNotFound 40414: 角色不存在。
	RoleNotFound = 40414
	// UserGroupAlreadyExists 40415: 用户组名已存在。
	UserGroupAlreadyExists = 40415
	// UserGroupNotFound 40416: 用户组不存在。
	UserGroupNotFound = 40416
	// InternalError 50000: 平台内部错误。
	InternalError = 50000
	// UpstreamK8sError 50100: 访问用户集群失败。
	UpstreamK8sError = 50100
	// AgentTunnelUnavailable 50200: Agent 通道不可用。
	AgentTunnelUnavailable = 50200
)
