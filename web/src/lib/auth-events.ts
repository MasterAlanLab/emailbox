// 认证事件管理 - 用于解耦 client.ts 和 authStore.ts，避免循环依赖

type AuthEventCallback = () => void;

let onUnauthorizedCallback: AuthEventCallback | null = null;

// 注册 401 未授权回调（由 authStore 调用）
export function onUnauthorized(callback: AuthEventCallback) {
  onUnauthorizedCallback = callback;
}

// 触发 401 未授权事件（由 client.ts 调用）
export function triggerUnauthorized() {
  if (onUnauthorizedCallback) {
    onUnauthorizedCallback();
  }
}
