import axios from "axios";
import type { AxiosInstance, AxiosResponse, AxiosError } from "axios";
import { triggerUnauthorized } from "./auth-events";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "";

// 导出给「不走 axios 的请求」用：附件下载交给浏览器直接发起（见 api/mail.ts），
// 拼 URL 时必须自己带上这个前缀，否则前后端分域部署时会请求到前端自己的域名上。
export const apiBaseURL = API_BASE_URL;

export interface ApiResponse<T = unknown> {
  code: number;
  data: T;
  message: string;
}

// 业务错误码，与 pkg/handler/response.go 一一对应（05 文档 §1.2）。
// 这里只列前端真的要分支处理的那个。
export const CODE_UPSTREAM_MAIL_ERR = 1005;

// shouldClearSession 判断一个失败响应是否意味着「**本人**的登录失效了」。
//
// 401 只应该有一个来源：本人会话过期 → 清会话、回登录页。
//
// 曾经不是这样：后端把托管邮箱的上游认证失败也映射成 401，
// 于是用户导入的一批账号里只要有一个上游认证失败，点开它就会把**用户自己**踢出登录。
// 根因已在服务端修掉——那类错误现在回 502 + 业务码 1005
// （pkg/handler/group_handler.go 的 upstreamFailure，由
// api/mail_messages_test.go 的 TestUpstreamAuthFailureIsNotUnauthorized 钉住）。
//
// 这里的 1005 判断作为第二道防线保留：清会话是个破坏性动作，
// 值得在两端各拦一次，何况别的端点将来也可能带着 1005 走别的状态码。
export function shouldClearSession(status: number, code?: number): boolean {
  return status === 401 && code !== CODE_UPSTREAM_MAIL_ERR;
}

// 接口错误。保留 status 和业务 code，调用方才能按状态码分支处理
// （例如区分 409 冲突和 422 校验失败），而不是只拿到一句文案。
export class ApiError extends Error {
  readonly status: number;
  readonly code?: number;

  constructor(message: string, status: number, code?: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

// errorBody 把错误响应体规整成 {message, code}。
//
// 不能直接把 data 当对象用：axios 只在 responseType 未指定或为 "json" 时才会
// 自动 JSON.parse，而导出接口要拿的是 text/plain 文件、于是显式设了
// responseType:"text"——那条路径上错误体会原样留成 JSON **字符串**，
// 当成对象读 message 只会得到 undefined，界面上就退化成「请求失败 (403)」，
// 用户分不清是密码打错了还是被限流了。
function errorBody(data: unknown): { message?: string; code?: number } | undefined {
  if (typeof data === "string") {
    try {
      return JSON.parse(data) as { message?: string; code?: number };
    } catch {
      // 不是 JSON（例如网关返回的 HTML 错误页），交给下面的状态码兜底文案
      return undefined;
    }
  }
  return data as { message?: string; code?: number } | undefined;
}

const client: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  timeout: 10000,
  withCredentials: true,
  headers: {
    "Content-Type": "application/json",
  },
});

// 认证走 cookie，请求侧没有需要加工的东西，因此只装响应拦截器。
client.interceptors.response.use(
  (response: AxiosResponse<ApiResponse>) => response,
  (error: AxiosError) => {
    if (!error.response) {
      // 请求已发出但没有收到响应
      const message = error.request ? "网络连接超时，请检查网络" : "网络错误，请稍后重试";
      return Promise.reject(new ApiError(message, 0));
    }

    const { status, data } = error.response;
    const body = errorBody(data);

    if (shouldClearSession(status, body?.code)) {
      triggerUnauthorized();
    }

    // 优先使用服务端返回的文案：后端已经对 5xx 做过脱敏，
    // 403 之类的拒绝原因也比前端硬编码的"权限不足"更具体。
    const fallback: Record<number, string> = {
      401: "未授权，请重新登录",
      404: "请求的资源不存在",
    };
    const message = body?.message || fallback[status] || `请求失败 (${status})`;

    return Promise.reject(new ApiError(message, status, body?.code));
  },
);

export default client;
