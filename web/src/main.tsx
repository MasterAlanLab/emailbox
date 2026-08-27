import { LinkProvider } from "@cloudflare/kumo/utils";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router-dom";
import { AppLink } from "./lib/AppLink.tsx";
import { initTheme } from "./lib/theme.ts";
import { router } from "./router/index.tsx";
import "./style.css";

// 主题必须在首次渲染之前挂到根元素上，否则会先闪一帧亮色再切到暗色。
initTheme();

// LinkProvider 放在 RouterProvider 外层没问题：它只提供 context，
// 而路由元素是 RouterProvider 的后代，照样能读到。
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <LinkProvider component={AppLink}>
      <RouterProvider router={router} />
    </LinkProvider>
  </StrictMode>,
);
