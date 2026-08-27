import { create } from "zustand";
import { onUnauthorized } from "@/lib/auth-events";
import {
  userApi,
  type AuthResponse,
  type LoginRequest,
  type RegisterRequest,
  type User,
} from "@/api";
interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  loading: boolean;
  loadSession: () => Promise<AuthResponse | null>;
  login: (data: LoginRequest) => Promise<AuthResponse>;
  register: (data: RegisterRequest) => Promise<AuthResponse>;
  logout: () => Promise<void>;
  setUser: (user: User) => void;
  clearAuth: () => void;
}
// 恢复会话的在途请求。并发调用共用同一个 promise，而不是各打一次 /auth/session。
//
// 触发点不止一处：Layout 挂载时恢复会话，将来任何组件也可能自己调一次。
// 最直接的证据是 StrictMode——它在开发模式下故意把 effect 跑两遍，
// 没有这道去重就能看到两次一模一样的会话请求（这正是 StrictMode 想让你发现的东西）。
let sessionInFlight: Promise<AuthResponse | null> | null = null;

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isAuthenticated: false,
  loading: true,
  loadSession: async () => {
    if (sessionInFlight) return sessionInFlight;
    sessionInFlight = (async () => {
      try {
        const r = await userApi.session();
        set({ user: r.data.user, isAuthenticated: true, loading: false });
        return r.data;
      } catch {
        set({ user: null, isAuthenticated: false, loading: false });
        return null;
      }
    })();
    try {
      return await sessionInFlight;
    } finally {
      // 用完就清，不做长期缓存：登出后再登录要能重新取一次真实状态。
      sessionInFlight = null;
    }
  },
  login: async (data) => {
    const r = await userApi.login(data);
    set({ user: r.data.user, isAuthenticated: true });
    return r.data;
  },
  register: async (data) => {
    const r = await userApi.register(data);
    set({ user: r.data.user, isAuthenticated: true });
    return r.data;
  },
  logout: async () => {
    try {
      await userApi.logout();
    } finally {
      set({ user: null, isAuthenticated: false });
    }
  },
  setUser: (user) => set({ user, isAuthenticated: true }),
  clearAuth: () => set({ user: null, isAuthenticated: false, loading: false }),
}));
onUnauthorized(() => useAuthStore.getState().clearAuth());
