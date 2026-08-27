// 路由元数据。挂在路由定义的 handle 上，由 Layout 通过 useMatches 读取。
//
// 用声明式的 handle 而不是在 Layout 里 `pathname.startsWith(...)`：
// 同一形态的页面往往不止一个，靠路径前缀判断迟早会漏掉一个，
// 而漏掉的表现是页面多出一个整页滚动条——很容易被当成样式问题去查 CSS，
// 其实是路由判断没覆盖到。
export interface RouteHandle {
  // app 表示这是登录后的应用页面：左侧常驻导航栏 + 右侧内容区，没有顶栏。
  app?: boolean;
  // shell 在 app 之上再进一步：内容区自身也不滚动（/mail 那种工作台，
  // 滚动由它内部的各个面板分别负责）。不带 shell 的 app 页面内容区正常滚动。
  shell?: boolean;
}

// 登录后的普通页面：侧边栏 + 可滚动内容区。
export const appRoute: RouteHandle = { app: true };

// 登录后的工作台页面：侧边栏 + 自身不滚动的内容区。
export const shellRoute: RouteHandle = { app: true, shell: true };
