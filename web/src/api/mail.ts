import client, { apiBaseURL } from "@/lib/client";
import type { ApiResponse } from "@/lib/client";

// 与后端 model 手工同步。集中在这一个文件里，改动时只需要对照 pkg/model 的
// mail_group.go 与 mail_account.go 两个文件。
export type GroupColor = "blue" | "green" | "amber" | "red" | "purple" | "gray";
export type AccountStatus = "active" | "disabled" | "banned";
export type RefreshStatus = "never" | "success" | "failed";

export interface MailGroup {
  id: string;
  name: string;
  description: string;
  color: GroupColor;
  sort_order: number;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

// 带账号数的分组。列表里的代理只回打码后的串——明文要单独走 groupProxy()。
export interface MailGroupNode extends MailGroup {
  proxy_url_masked: string;
  account_count: number;
}

// 分组代理的明文，只有编辑表单会取。回填打码串的话，用户改完名字一按保存就把
// "****" 当口令存回去了，代理从此是坏的、界面上还看不出来。
export interface GroupProxy {
  proxy_url: string;
}

// 分组的代理，创建与更新共用这个形状。
export interface GroupProxyInput {
  proxy_url?: string;
}

// 账号。凭据字段只回 has_* 布尔值，明文永不出接口。
export interface MailAccount {
  id: string;
  group_id: string;
  email: string;
  provider: string;
  account_type: string;
  auth_channel: string;
  client_id: string;
  imap_host: string;
  imap_port: number;
  status: AccountStatus;
  remark: string;
  sort_order: number;
  last_refresh_at: string | null;
  last_refresh_status: RefreshStatus;
  last_refresh_error: string;
  last_refresh_error_kind: string;
  created_at: string;
  updated_at: string;
  has_password: boolean;
  has_refresh_token: boolean;
  has_imap_password: boolean;
  proxy_url_masked: string;
  fallback_proxy_url_1_masked: string;
  fallback_proxy_url_2_masked: string;
  aliases: string[];
}

export interface Pagination {
  page: number;
  limit: number;
  total: number;
  pages: number;
}

export interface AccountListResponse {
  items: MailAccount[];
  pagination: Pagination;
}

export interface AccountFilterParams {
  group_id?: string;
  q?: string;
  status?: AccountStatus | "";
  refresh_status?: RefreshStatus | "";
  provider?: string;
  sort?: "sort_order" | "email" | "created_at" | "last_refresh_at";
  order?: "asc" | "desc";
  page?: number;
  limit?: number;
}

export interface ImportError {
  line: number;
  email: string;
  reason: string;
}

// 导入是逐行统计的：解析失败计 failed，重复或超配额计 skipped，
// 已导入的部分始终保留。errors 会被后端截断，truncated 标记是否有省略。
export interface ImportResult {
  total: number;
  created: number;
  updated: number;
  skipped: number;
  failed: number;
  errors: ImportError[];
  truncated: boolean;
}

export interface BatchResult {
  requested: number;
  succeeded: number;
  failed: number;
  errors: { account_id: string; reason: string }[];
}

export interface OAuthStartResult {
  flow_id: string;
  authorization_url: string;
  expires_at: string;
}

export interface OAuthCompleteResult {
  account_id: string;
  email: string;
  status: "success";
}

export interface Limits {
  plan_code: string;
  plan_name: string;
  max_accounts: number;
  max_groups: number;
  daily_mail_fetch: number;
}

// 邮件相关类型。与 pkg/mailer/mailer.go 手工同步。
export type MailFolder = "inbox" | "junkemail" | "deleteditems" | "all";

// IDMode 说明 id 是 IMAP 的 UID 还是序列号。详情与附件请求必须原样带回来，
// 混用会取到错误的邮件。Graph 的 id 是全局唯一字符串，这里是空串。
export type IDMode = "uid" | "sequence" | "";

export interface Message {
  id: string;
  id_mode: IDMode;
  folder: MailFolder;
  subject: string;
  from: string;
  to: string;
  cc: string;
  received_at: string;
  is_read: boolean;
  has_attachments: boolean;
  body_preview: string;
}

export interface AttachmentMeta {
  id: string;
  name: string;
  content_type: string;
  size: number;
  is_inline: boolean;
}

export interface MessageDetail extends Message {
  body: string;
  body_type: "text" | "html";
  attachments: AttachmentMeta[];
}

export interface MessageListResponse {
  items: Message[];
  // channel 是本次实际走通的通道（graph / imap_new / imap_old / imap）。
  channel: string;
}

export interface MessageRef {
  id: string;
  id_mode: IDMode;
  folder: MailFolder;
}

export interface MessageBatchResult {
  succeeded: number;
  failed: number;
  items: { ref: MessageRef; ok: boolean; error?: string }[];
}

// MailScope 决定这批请求打到谁的邮箱上。
//
// 管理员视图与用户视图的接口是**完全同构**的（05 文档 §12.2），差别只有路径前缀，
// 因此整个 /mail 的组件树可以原样复用，只需把 scope 换掉。
// 反过来说，这个前缀绝不能出错：admin 前缀漏掉会让管理员打到自己的租户上
// （看到的是自己的邮箱，还以为在看别人的），多加了则普通用户直接 403。
export interface MailScope {
  tenantID: string;
  admin?: boolean;
}

// TenantRef 让调用方既能传裸的 tenantID（默认用户视图），也能传完整 scope。
export type TenantRef = string | MailScope;

export const asScope = (ref: TenantRef): MailScope =>
  typeof ref === "string" ? { tenantID: ref } : ref;

// mailScopeKey 是 scope 的字符串形式，用来拼 effect 的重置键。
// 组件里不能直接把 scope 对象插进模板串——那会得到 "[object Object]"，
// 于是切换租户时重置逻辑失效，管理员会看到上一个租户的邮件。
export const mailScopeKey = (ref: TenantRef): string => {
  const scope = asScope(ref);
  return scope.admin ? `admin:${scope.tenantID}` : scope.tenantID;
};

export const mailBase = (ref: TenantRef): string => {
  const scope = asScope(ref);
  return scope.admin
    ? `/api/v1/admin/tenants/${scope.tenantID}/mail`
    : `/api/v1/tenants/${scope.tenantID}/mail`;
};

const base = mailBase;

const messageBase = (tenant: TenantRef, accountID: string) =>
  `${base(tenant)}/accounts/${accountID}/messages`;

// 邮件端点每一个都要打上游（Graph / IMAP，可能还过代理），client.ts 的默认 10 秒
// 远远不够：一次 IMAP SELECT + FETCH 走代理十几秒是常态，回退链还可能连试三条通道。
// 超时被前端掐断最糟的地方在于——配额已经在服务端扣掉了，用户却什么都没拿到。
const MESSAGE_TIMEOUT = 120_000;

// 导出要逐个解密上万条凭据，10 秒的默认超时不够。
const EXPORT_TIMEOUT = 60_000;

export const mailApi = {
  groups: async (tenantID: TenantRef) =>
    (await client.get<ApiResponse<MailGroupNode[]>>(`${base(tenantID)}/groups`)).data,
  // 明文代理需要 account:secret 权限并会写一条审计——只在编辑表单打开时调一次。
  groupProxy: async (tenantID: TenantRef, groupID: string) =>
    (await client.get<ApiResponse<GroupProxy>>(`${base(tenantID)}/groups/${groupID}/proxy`)).data,
  createGroup: async (
    tenantID: TenantRef,
    data: { name: string; color?: GroupColor; description?: string } & GroupProxyInput,
  ) => (await client.post<ApiResponse<MailGroup>>(`${base(tenantID)}/groups`, data)).data,
  // 更新用 PATCH 语义：字段不传就保持原值。代理三项因此可以整组省略——
  // 明文没读到时正是这么做的，免得拿一组空串把用户配好的代理洗掉。
  updateGroup: async (
    tenantID: TenantRef,
    groupID: string,
    data: Partial<{ name: string; color: GroupColor; description: string }> & GroupProxyInput,
  ) =>
    (await client.patch<ApiResponse<MailGroup>>(`${base(tenantID)}/groups/${groupID}`, data)).data,
  reorderGroups: async (tenantID: TenantRef, groupIDs: string[]) =>
    (
      await client.post<ApiResponse<null>>(`${base(tenantID)}/groups/reorder`, {
        group_ids: groupIDs,
      })
    ).data,
  deleteGroup: async (tenantID: TenantRef, groupID: string) =>
    (await client.delete<ApiResponse<null>>(`${base(tenantID)}/groups/${groupID}`)).data,

  accounts: async (tenantID: TenantRef, params: AccountFilterParams = {}) =>
    (await client.get<ApiResponse<AccountListResponse>>(`${base(tenantID)}/accounts`, { params }))
      .data,
  account: async (tenantID: TenantRef, accountID: string) =>
    (await client.get<ApiResponse<MailAccount>>(`${base(tenantID)}/accounts/${accountID}`)).data,
  createAccount: async (tenantID: TenantRef, data: Record<string, unknown>) =>
    (await client.post<ApiResponse<MailAccount>>(`${base(tenantID)}/accounts`, data)).data,
  updateAccount: async (tenantID: TenantRef, accountID: string, data: Record<string, unknown>) =>
    (await client.patch<ApiResponse<MailAccount>>(`${base(tenantID)}/accounts/${accountID}`, data))
      .data,
  deleteAccount: async (tenantID: TenantRef, accountID: string) =>
    (await client.delete<ApiResponse<null>>(`${base(tenantID)}/accounts/${accountID}`)).data,
  startReauthorization: async (tenantID: TenantRef, accountID: string) =>
    (
      await client.post<ApiResponse<OAuthStartResult>>(
        `${base(tenantID)}/accounts/${accountID}/oauth/start`,
      )
    ).data,
  completeReauthorization: async (
    tenantID: TenantRef,
    accountID: string,
    data: { flow_id: string; redirected_url?: string },
  ) =>
    (
      await client.post<ApiResponse<OAuthCompleteResult>>(
        `${base(tenantID)}/accounts/${accountID}/oauth/complete`,
        data,
        { timeout: MESSAGE_TIMEOUT },
      )
    ).data,
  importAccounts: async (tenantID: TenantRef, data: Record<string, unknown>) =>
    (await client.post<ApiResponse<ImportResult>>(`${base(tenantID)}/accounts/import`, data)).data,
  // 导出返回的是 text/plain 文件本身，不是 {code,data,message} 结构。
  // 用 responseType:"text" 而不是 "blob"：出错时后端回的仍是 JSON，
  // 若按 blob 收，拦截器读不到 message，界面上只剩一句「请求失败 (403)」，
  // 用户就分不清是密码打错了还是被限流了。
  exportAccounts: async (tenantID: TenantRef, data: Record<string, unknown>) =>
    (
      await client.post<string>(`${base(tenantID)}/accounts/export`, data, {
        responseType: "text",
        timeout: EXPORT_TIMEOUT,
      })
    ).data,

  batchMove: async (tenantID: TenantRef, accountIDs: string[], groupID: string) =>
    (
      await client.post<ApiResponse<BatchResult>>(`${base(tenantID)}/accounts/batch/move`, {
        account_ids: accountIDs,
        group_id: groupID,
      })
    ).data,
  batchStatus: async (tenantID: TenantRef, accountIDs: string[], status: AccountStatus) =>
    (
      await client.post<ApiResponse<BatchResult>>(`${base(tenantID)}/accounts/batch/status`, {
        account_ids: accountIDs,
        status,
      })
    ).data,
  batchDelete: async (tenantID: TenantRef, accountIDs: string[]) =>
    (
      await client.post<ApiResponse<BatchResult>>(`${base(tenantID)}/accounts/batch/delete`, {
        account_ids: accountIDs,
      })
    ).data,

  messages: async (
    tenantID: TenantRef,
    accountID: string,
    params: { folder: MailFolder; skip?: number; top?: number },
  ) =>
    (
      await client.get<ApiResponse<MessageListResponse>>(messageBase(tenantID, accountID), {
        params,
        timeout: MESSAGE_TIMEOUT,
      })
    ).data,
  message: async (
    tenantID: TenantRef,
    accountID: string,
    messageID: string,
    params: { folder: MailFolder; id_mode: IDMode },
  ) =>
    (
      await client.get<ApiResponse<MessageDetail>>(
        `${messageBase(tenantID, accountID)}/${encodeURIComponent(messageID)}`,
        { params, timeout: MESSAGE_TIMEOUT },
      )
    ).data,
  // 附件走浏览器直接下载而不是 XHR：后端已经带了 Content-Disposition，
  // 交给浏览器处理能省掉一次内存里的完整拷贝，大附件上差别明显。
  attachmentURL: (
    tenantID: TenantRef,
    accountID: string,
    messageID: string,
    attachmentID: string,
    params: { folder: MailFolder; id_mode: IDMode },
  ) => {
    const query = new URLSearchParams({ folder: params.folder, id_mode: params.id_mode });
    return `${apiBaseURL}${messageBase(tenantID, accountID)}/${encodeURIComponent(messageID)}/attachments/${encodeURIComponent(attachmentID)}?${query}`;
  },
  attachmentsZipURL: (
    tenantID: TenantRef,
    accountID: string,
    messageID: string,
    params: { folder: MailFolder; id_mode: IDMode },
  ) => {
    const query = new URLSearchParams({ folder: params.folder, id_mode: params.id_mode });
    return `${apiBaseURL}${messageBase(tenantID, accountID)}/${encodeURIComponent(messageID)}/attachments.zip?${query}`;
  },
  markMessagesRead: async (tenantID: TenantRef, accountID: string, items: MessageRef[]) =>
    (
      await client.post<ApiResponse<MessageBatchResult>>(
        `${messageBase(tenantID, accountID)}/read`,
        { items },
        { timeout: MESSAGE_TIMEOUT },
      )
    ).data,
  deleteMessages: async (tenantID: TenantRef, accountID: string, items: MessageRef[]) =>
    (
      await client.post<ApiResponse<MessageBatchResult>>(
        `${messageBase(tenantID, accountID)}/delete`,
        { items },
        { timeout: MESSAGE_TIMEOUT },
      )
    ).data,
};
