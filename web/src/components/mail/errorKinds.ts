// 上游失败原因的中文名，与后端 mailer.ErrKind 手工同步。
//
// 分类用于汇总，具体处置看逐账号原因：同为 auth_failed，
// 可能是令牌过期，也可能是重新登录要求，分类本身不等于已确认过期。
export const ERROR_KIND_LABEL: Record<string, string> = {
  banned: "账号被封禁",
  account_unavailable: "邮箱拒绝连接（账号被锁定或停用）",
  auth_failed: "认证失败",
  consent_required: "权限不足",
  proxy_failed: "代理不可用",
  network: "网络不可达",
  rate_limited: "被限流",
  folder_unavailable: "邮箱文件夹不可用",
  provider_error: "服务商或应用配置错误",
  canceled: "已取消",
};

export const errorKindLabel = (kind: string, fallback = "未分类") =>
  ERROR_KIND_LABEL[kind] ?? (kind || fallback);
