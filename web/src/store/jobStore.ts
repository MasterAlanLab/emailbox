import { create } from "zustand";
import {
  isTerminal,
  jobApi,
  type FinishedPayload,
  type ItemPayload,
  type JobStatus,
  type ProgressPayload,
} from "@/api/jobs";
import type { TenantRef } from "@/api/mail";

// 界面上保留的最近结果条数。
//
// 5000 个账号会推 5000 条 item 事件，全存进 state 会让页面越跑越卡，
// 而用户真正在看的只有最近这几十条——完整结果在任务详情页按需分页取。
const MAX_RECENT = 50;

export interface RecentItem {
  email: string;
  status: ItemPayload["status"];
  errorKind: string;
  error: string;
}

interface JobProgress {
  total: number;
  success: number;
  failed: number;
  done: number;
  current: string;
}

interface JobState {
  jobID: string | null;
  status: JobStatus | null;
  progress: JobProgress;
  recent: RecentItem[];
  summary: string;
  // lastEventID 是断线重连的续看位置。它必须一直维护着——
  // 浏览器原生 EventSource 会自动带 Last-Event-ID 重连，但我们在
  // 组件卸载/重新挂载时是手工重连的，那时只能靠这个值。
  lastEventID: number;
  error: string;

  watch: (tenant: TenantRef, jobID: string) => void;
  stopWatching: () => void;
  reset: () => void;
}

const emptyProgress: JobProgress = { total: 0, success: 0, failed: 0, done: 0, current: "" };

// source 放在 store 之外：它是浏览器资源而不是渲染状态，
// 放进 state 会让每次事件都触发一次无谓的重渲染。
let source: EventSource | null = null;

const closeSource = () => {
  source?.close();
  source = null;
};

export const useJobStore = create<JobState>((set, get) => ({
  jobID: null,
  status: null,
  progress: emptyProgress,
  recent: [],
  summary: "",
  lastEventID: 0,
  error: "",

  watch: (tenant, jobID) => {
    // 切到另一个任务时先把旧连接关掉，否则两个流的事件会混在一起。
    if (get().jobID !== jobID) {
      closeSource();
      set({
        jobID,
        status: null,
        progress: emptyProgress,
        recent: [],
        summary: "",
        lastEventID: 0,
        error: "",
      });
    }
    if (source) return;

    const connect = () => {
      const url = jobApi.streamURL(tenant, jobID, get().lastEventID || undefined);
      // withCredentials 让 EventSource 带上会话 Cookie；
      // 少了它，跨域部署时这个流会直接 401。
      source = new EventSource(url, { withCredentials: true });

      const remember = (event: MessageEvent) => {
        const seq = Number(event.lastEventId);
        if (Number.isFinite(seq) && seq > 0) set({ lastEventID: seq });
      };

      source.addEventListener("started", (event) => {
        remember(event as MessageEvent);
        set({ status: "running" });
      });

      source.addEventListener("progress", (event) => {
        const message = event as MessageEvent;
        remember(message);
        const payload = JSON.parse(message.data) as ProgressPayload;
        set({
          status: "running",
          progress: {
            total: payload.total,
            success: payload.success,
            failed: payload.failed,
            done: payload.done,
            current: payload.current,
          },
        });
      });

      source.addEventListener("item", (event) => {
        const message = event as MessageEvent;
        remember(message);
        const payload = JSON.parse(message.data) as ItemPayload;
        set((state) => ({
          recent: [
            {
              email: payload.email,
              status: payload.status,
              errorKind: payload.error_kind,
              error: payload.error,
            },
            ...state.recent,
          ].slice(0, MAX_RECENT),
        }));
      });

      source.addEventListener("finished", (event) => {
        const message = event as MessageEvent;
        remember(message);
        const payload = JSON.parse(message.data) as FinishedPayload;
        set({
          status: payload.status,
          summary: payload.error_summary,
          progress: {
            total: payload.total,
            success: payload.success,
            failed: payload.failed,
            done: payload.success + payload.failed + payload.skipped,
            current: "",
          },
        });
        // 任务结束就断开：留着连接只会让服务端一直发心跳帧。
        closeSource();
      });

      source.onerror = () => {
        // EventSource 自己会重连，且会带上 Last-Event-ID。这里只在任务已经
        // 结束时收手——否则一个已经终结的任务会让浏览器无限重连下去。
        const status = get().status;
        if (status && isTerminal(status)) closeSource();
      };
    };

    connect();
  },

  stopWatching: () => closeSource(),

  reset: () => {
    closeSource();
    set({
      jobID: null,
      status: null,
      progress: emptyProgress,
      recent: [],
      summary: "",
      lastEventID: 0,
      error: "",
    });
  },
}));
