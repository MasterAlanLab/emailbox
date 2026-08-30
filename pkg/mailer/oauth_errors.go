package mailer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type oauthErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
	Codes       []int  `json:"error_codes"`
}

// 诊断只允许输出这张表里的固定文案和数字码，响应中的账号、凭据和 trace 信息留在内存。
// 按具体原因优先匹配，避免宽泛的 invalid_grant 遮住过期、策略或权限错误。
// 依据：https://learn.microsoft.com/en-us/entra/identity-platform/reference-error-codes
var oauthCodeRules = []struct {
	codes   []int
	kind    ErrKind
	message string
}{
	{[]int{70008, 700082}, ErrKindAuthFailed, "refresh_token 因长期未使用而过期，请重新授权"},
	{[]int{700084}, ErrKindAuthFailed, "refresh_token 已达到 SPA 固定有效期，请重新登录授权"},
	{[]int{50173}, ErrKindAuthFailed, "refresh_token 对应的授权已被撤销，请重新授权"},
	{[]int{50072, 50074, 50076, 50078, 50079}, ErrKindAuthFailed, "邮箱账号需要完成多因素验证，请重新登录微软账号"},
	{[]int{50131, 53000, 53001, 53002, 53003, 530035}, ErrKindAuthFailed, "当前请求未满足条件访问策略，请联系邮箱管理员检查登录策略"},
	{[]int{70043}, ErrKindAuthFailed, "登录频率策略要求重新验证，请重新登录微软账号"},
	{[]int{50058, 50085, 50089, 700020}, ErrKindAuthFailed, "邮箱账号需要重新登录，请完成微软登录验证"},
	{[]int{65001, 65004, 90008}, ErrKindConsentRequired, "应用尚未获得所需权限，请重新授权或联系邮箱管理员同意"},
	{[]int{28002, 28003, 70011, 700022, 700023}, ErrKindConsentRequired, "应用请求的权限范围不匹配，请检查授权范围或重新授权"},
	{[]int{65005, 650056, 650057}, ErrKindProviderError, "应用的资源或权限配置不匹配，请联系应用管理员检查注册配置"},
	{[]int{70001, 700011, 700016, 7000112}, ErrKindProviderError, "应用未注册、已停用或与账号类型不匹配，请检查 client_id 和应用注册配置"},
	{[]int{70002, 7000215}, ErrKindProviderError, "应用客户端凭据校验失败，请联系应用管理员检查 client_secret"},
	{[]int{7000218}, ErrKindProviderError, "应用缺少客户端凭据，请联系应用管理员配置 client_secret 或客户端断言"},
	{[]int{7000222}, ErrKindProviderError, "应用的 client_secret 已过期，请联系应用管理员更新客户端凭据"},
	{[]int{70000}, ErrKindAuthFailed, "当前通道的令牌交换未通过，请核对账号授权与 client_id"},
	{[]int{90023, 9002313}, ErrKindProviderError, "OAuth 请求参数不正确，请检查应用配置"},
}

var aadstsCodePattern = regexp.MustCompile(`(?i)\bAADSTS([0-9]+)\b`)

// ClassifyOAuthError 按标准 OAuth 错误与已知微软诊断码生成脱敏结果。
// invalid_grant 本身只说明本次授权材料未被接受，未证明令牌超过有效期。
func ClassifyOAuthError(statusCode int, body string) (ErrKind, string) {
	// 服务不可用或限流时，响应正文即使附带旧的授权错误也不应改变故障归因。
	if statusCode == http.StatusProxyAuthRequired {
		return ErrKindProxyFailed, "代理认证失败，请检查代理账号和密码"
	}
	if statusCode == 429 {
		return ErrKindRateLimited, "请求过于频繁，已被限流，请稍后重试"
	}
	if statusCode >= 500 {
		return ErrKindProviderError, "服务商暂时不可用，请稍后重试"
	}
	response := parseOAuthError(body)
	if strings.Contains(strings.ToLower(response.Description), abuseMarker) {
		return ErrKindBanned, "账号已被服务商封禁"
	}
	for _, rule := range oauthCodeRules {
		for _, code := range rule.codes {
			if slices.Contains(response.Codes, code) {
				return rule.kind, fmt.Sprintf("%s（AADSTS%d）", rule.message, code)
			}
		}
	}
	if oauthScopeDescriptionError(response.Description) {
		return ErrKindConsentRequired, "应用请求的权限范围不匹配，请检查授权范围或重新授权"
	}
	return classifyOAuthName(response.Error, statusCode)
}

func parseOAuthError(body string) oauthErrorResponse {
	var response oauthErrorResponse
	if json.Unmarshal([]byte(body), &response) != nil {
		response = oauthErrorResponse{Error: strings.TrimSpace(body), Description: body}
	}
	response.Error = strings.ToLower(strings.TrimSpace(response.Error))
	// 部分旧端点仅在描述中返回 AADSTS 码；只取完整数字码，不透传原文。
	// 已有结构化代码时以它为准，避免描述里提到的其它代码覆盖真实诊断。
	if len(response.Codes) == 0 {
		for _, match := range aadstsCodePattern.FindAllStringSubmatch(response.Description, -1) {
			if code, err := strconv.Atoi(match[1]); err == nil {
				response.Codes = append(response.Codes, code)
			}
		}
	}
	return response
}

func classifyOAuthName(name string, statusCode int) (ErrKind, string) {
	switch name {
	case "invalid_scope":
		return ErrKindConsentRequired, "应用请求的权限范围不匹配，请检查授权范围或重新授权"
	case "consent_required":
		return ErrKindConsentRequired, "应用尚未获得所需权限，请重新授权或联系邮箱管理员同意"
	case "interaction_required", "login_required":
		return ErrKindAuthFailed, "邮箱账号需要交互式登录验证，请重新登录微软账号"
	case "invalid_client":
		return ErrKindProviderError, "应用客户端认证失败，请检查 client_id 与客户端凭据"
	case "unauthorized_client":
		return ErrKindProviderError, "应用未获准使用当前授权方式，请检查应用注册和账号类型配置"
	case "invalid_grant":
		return ErrKindAuthFailed, "当前通道的令牌交换未通过，请核对账号授权与 client_id"
	case "invalid_request", "unsupported_grant_type", "invalid_resource":
		return ErrKindProviderError, "OAuth 请求参数或资源配置不正确，请检查应用配置"
	case "temporarily_unavailable", "server_error":
		return ErrKindProviderError, "服务商暂时不可用，请稍后重试"
	default:
		return ErrKindProviderError, fmt.Sprintf("令牌请求失败（HTTP %d），请稍后重试或联系服务商", statusCode)
	}
}

// 部分端点只在 error_description 里给出权限诊断，即使外层仍是合法 JSON。
// 这里只保留两条明确措辞；单独的 consent 或 expired 也可能出现在登录策略错误里，
// 拿它们降级 scope 会制造无效重试。
func oauthScopeDescriptionError(description string) bool {
	lower := strings.ToLower(description)
	return strings.Contains(lower, "no applicable permissions") ||
		strings.Contains(lower, "permissions requested are unauthorized or expired")
}
