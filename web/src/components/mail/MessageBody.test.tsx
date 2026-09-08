import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MessageBody } from "./MessageBody";

// 邮件正文的三层防护里，第 1 层（DOMPurify 净化）由 messageDocument.test.ts 钉住，
// 第 3 层（CSP）在那边也断言了字符串。唯独第 2 层——iframe 的 sandbox——一直没有
// 任何用例守着，而它恰恰是「净化被绕过时仍然跑不了脚本」的最后一道。
//
// 改成桌面版之后这层更要钉死：正文此时渲染在系统 WebView 里
// （macOS WKWebView / Windows WebView2 / Linux WebKitGTK）。sandbox 与 CSP 都是
// 这两个引擎原生支持的标准特性，行为与浏览器一致；但一旦有人为了「让某封邮件
// 显示正常」加上 allow-scripts，桌面版丢的就不只是一个标签页。

function iframeOf(container: HTMLElement): HTMLIFrameElement {
  const frame = container.querySelector("iframe");
  if (!frame) throw new Error("没有渲染出 iframe");
  return frame;
}

// 断言图片地址要解析 DOM，不能对 srcdoc 做子串匹配：
// 被拦下的图片会把原地址留在 data-blocked-src 上，而字符串 `src="…"`
// 正好是 `data-blocked-src="…"` 的后缀，子串写法永远判不对。
function imageOf(container: HTMLElement): HTMLImageElement {
  const srcDoc = iframeOf(container).getAttribute("srcdoc") ?? "";
  const img = new DOMParser().parseFromString(srcDoc, "text/html").querySelector("img");
  if (!img) throw new Error("文档里没有 img");
  return img;
}

describe("MessageBody · sandbox 隔离", () => {
  it("iframe 带空 sandbox：属性存在且不授予任何能力", () => {
    const { container } = render(<MessageBody body="<p>hi</p>" bodyType="html" />);
    const sandbox = iframeOf(container).getAttribute("sandbox");

    // null 表示属性缺失——那等于完全没有沙箱，比空字符串危险得多，必须区分开。
    expect(sandbox).not.toBeNull();
    expect(sandbox).toBe("");
  });

  it("永远不授予 allow-scripts 与 allow-same-origin", () => {
    const { container } = render(
      <MessageBody body="<p>hi</p><img src='https://x.test/a.png'>" bodyType="html" />,
    );
    const sandbox = iframeOf(container).getAttribute("sandbox") ?? "";

    // 这两个同时出现时，iframe 可以脚本化地读到父页面的 DOM 与会话，
    // 沙箱就等于没有——DOMPurify 的任何一次绕过都会直接变成完整的账号接管。
    expect(sandbox).not.toContain("allow-scripts");
    expect(sandbox).not.toContain("allow-same-origin");
  });

  it("srcDoc 里带着 default-src 'none' 的 CSP", () => {
    const { container } = render(<MessageBody body="<p>hi</p>" bodyType="html" />);
    const srcDoc = iframeOf(container).getAttribute("srcdoc") ?? "";

    expect(srcDoc).toContain('http-equiv="Content-Security-Policy"');
    expect(srcDoc).toContain("default-src 'none'");
  });

  it("纯文本正文同样进沙箱，不走裸 innerHTML", () => {
    const { container } = render(<MessageBody body="<script>alert(1)</script>" bodyType="text" />);
    const frame = iframeOf(container);

    expect(frame.getAttribute("sandbox")).toBe("");
    // 纯文本要被转义成实体，而不是作为标签留在文档里。
    expect(frame.getAttribute("srcdoc") ?? "").toContain("&lt;script&gt;");
  });
});

describe("MessageBody · 远程图片", () => {
  it("默认阻断远程图片并给出提示", () => {
    const { container } = render(
      <MessageBody body="<img src='https://tracker.test/pixel.gif'>" bodyType="html" />,
    );

    expect(screen.getByText(/已阻止远程图片/)).toBeTruthy();
    const img = imageOf(container);
    expect(img.getAttribute("src")).toMatch(/^data:image\/gif;base64,/);
    // 原地址挪到 data-blocked-src 上，「仍要加载」要靠它还原。
    expect(img.getAttribute("data-blocked-src")).toBe("https://tracker.test/pixel.gif");
  });

  it("点「仍要加载」之后才放行，且沙箱不因此松开", () => {
    const { container } = render(
      <MessageBody body="<img src='https://tracker.test/pixel.gif'>" bodyType="html" />,
    );
    fireEvent.click(screen.getByText("仍要加载"));

    expect(imageOf(container).getAttribute("src")).toBe("https://tracker.test/pixel.gif");
    // 放行图片是用户对「暴露已读」的知情选择，与脚本隔离无关，
    // 后者在任何时候都不该跟着一起放开。
    expect(iframeOf(container).getAttribute("sandbox")).toBe("");
  });
});
