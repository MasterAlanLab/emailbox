import client, { apiBaseURL } from "@/lib/client";
import type { ApiResponse } from "@/lib/client";
import { mailBase, type Pagination, type TenantRef } from "./mail";

// 与后端 pkg/model/job.go 手工同步。

export type JobStatus =
  | "pending"
  | "running"
  | "stopping"
  | "succeeded"
  | "partial"
  | "failed"
  | "stopped"
  | "interrupted";

// 终态判定必须和后端 model.IsTerminalJobStatus 保持一致，
// 前端据它决定什么时候停止订阅、什么时候允许再次提交。
const TERMINAL: JobStatus[] = ["succeeded", "partial", "failed", "stopped", "interrupted"];

export const isTerminal = (status: JobStatus) => TERMINAL.includes(status);

export interface Job {
  id: string;
  tenant_id: string;
  type: string;
  trigger: string;
  status: JobStatus;
  created_by: string;
  total_count: number;
  success_count: number;
  failed_count: number;
  params: string;
  error_summary: string;
  started_at: string | null;
  finished_at: string | null;
  created_at: string;
}

export type JobItemStatus = "pending" | "running" | "success" | "failed" | "skipped";

export interface JobItem {
  id: string;
  job_id: string;
  account_id: string;
  email: string;
  position: number;
  status: JobItemStatus;
  error_kind: string;
  error: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface RefreshLog {
  id: string;
  account_id: string;
  account_email: string;
  job_id: string;
  refresh_type: string;
  status: "success" | "failed";
  error_kind: string;
  error_message: string;
  created_at: string;
}

export interface RefreshStats {
  total: number;
  success: number;
  failed: number;
  never: number;
  by_error_kind: Record<string, number>;
  last_job: Job | null;
}

// SSE 事件的载荷，与后端 pkg/job/worker.go 的三个 payload 结构对应。
export interface ProgressPayload {
  total: number;
  success: number;
  failed: number;
  done: number;
  current: string;
}

export interface ItemPayload {
  account_id: string;
  email: string;
  status: JobItemStatus;
  error_kind: string;
  error: string;
}

export interface FinishedPayload {
  status: JobStatus;
  total: number;
  success: number;
  failed: number;
  skipped: number;
  error_summary: string;
}

interface Page<T> {
  items: T[];
  pagination: Pagination;
}

export const jobApi = {
  submitRefresh: async (
    tenant: TenantRef,
    data: {
      scope: "all" | "failed" | "selected" | "group";
      account_ids?: string[];
      // scope=group 时必填，对应后端 service.RefreshScopeGroup。
      group_ids?: string[];
    },
  ) => (await client.post<ApiResponse<Job>>(`${mailBase(tenant)}/jobs/token-refresh`, data)).data,

  refreshOne: async (tenant: TenantRef, accountID: string) =>
    (
      await client.post<ApiResponse<null>>(
        `${mailBase(tenant)}/accounts/${accountID}/token/refresh`,
        {},
        // 单个刷新是同步的，要真的等上游走完一遍回退链，10 秒的默认超时不够。
        { timeout: 120_000 },
      )
    ).data,

  list: async (tenant: TenantRef, params: { type?: string; status?: string; page?: number } = {}) =>
    (await client.get<ApiResponse<Page<Job>>>(`${mailBase(tenant)}/jobs`, { params })).data,

  get: async (tenant: TenantRef, jobID: string) =>
    (await client.get<ApiResponse<Job>>(`${mailBase(tenant)}/jobs/${jobID}`)).data,

  items: async (
    tenant: TenantRef,
    jobID: string,
    params: { status?: string; page?: number } = {},
  ) =>
    (
      await client.get<ApiResponse<Page<JobItem>>>(`${mailBase(tenant)}/jobs/${jobID}/items`, {
        params,
      })
    ).data,

  stop: async (tenant: TenantRef, jobID: string) =>
    (await client.post<ApiResponse<null>>(`${mailBase(tenant)}/jobs/${jobID}/stop`)).data,

  stats: async (tenant: TenantRef) =>
    (await client.get<ApiResponse<RefreshStats>>(`${mailBase(tenant)}/refresh/stats`)).data,

  logs: async (
    tenant: TenantRef,
    params: { status?: string; account_id?: string; job_id?: string; page?: number } = {},
  ) =>
    (
      await client.get<ApiResponse<Page<RefreshLog>>>(`${mailBase(tenant)}/refresh/logs`, {
        params,
      })
    ).data,

  // SSE 不走 axios，因此要自己拼上 apiBaseURL——前后端分域部署时，
  // 少了它 EventSource 会连到前端自己的域名上。
  streamURL: (tenant: TenantRef, jobID: string, lastEventID?: number) => {
    const base = `${apiBaseURL}${mailBase(tenant)}/jobs/${jobID}/stream`;
    return lastEventID ? `${base}?last_event_id=${lastEventID}` : base;
  },
};
