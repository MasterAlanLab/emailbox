import { useCallback, useRef, useState } from "react";

// useAsyncAction 统一处理表单提交的三件事：捕获错误、暴露 pending、防止重复提交。
// 没有 pending 的话按钮无法禁用，双击"创建"会真的发出两个请求。
export function useAsyncAction() {
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  // 用 ref 而非 pending 状态做去重：同一轮事件里的两次提交之间没有重新渲染，
  // 闭包捕获的 pending 仍是 false，两次都会被放行。
  const running = useRef(false);

  // 返回是否执行成功，便于调用方决定要不要继续（例如跳转页面）。
  const run = useCallback(async (action: () => Promise<void>): Promise<boolean> => {
    if (running.current) return false;
    running.current = true;
    setError("");
    setPending(true);
    try {
      await action();
      return true;
    } catch (caught) {
      setError((caught as Error).message);
      return false;
    } finally {
      running.current = false;
      setPending(false);
    }
  }, []);

  return { error, pending, run, setError };
}
