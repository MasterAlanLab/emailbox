import { describe, expect, it } from "vitest";
import { buildDocument } from "./messageDocument";

// 06 文档 §9 把「MessageBody 净化」列为必测项：邮件正文是全平台唯一
// 由陌生人完全控制的 HTML，这里每一条用例都对应一个真实见过的攻击手法。
// 组件层还有 sandbox iframe 与 CSP 两层，但那两层挡不住「清洗把正文改坏了」，
// 所以正向用例（cid 图片、普通链接要留下）和负向用例一样重要。

const parse = (html: string) => new DOMParser().parseFromString(html, "text/html");

const bodyOf = (source: string, showRemoteImages = false) => {
  const { html } = buildDocument(source, "html", showRemoteImages);
  return parse(html).body;
};

describe("buildDocument · HTML 净化", () => {
  it("移除 script 标签，且不把脚本内容当文本留下", () => {
    const body = bodyOf("<p>hi</p><script>alert(1)</script>");
    expect(body.querySelector("script")).toBeNull();
    expect(body.textContent).toBe("hi");
  });

  it("移除事件处理属性，但保留元素本身", () => {
    const body = bodyOf('<img src="cid:logo" onerror="alert(1)" onload="alert(2)">');
    const img = body.querySelector("img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("onerror")).toBeNull();
    expect(img?.getAttribute("onload")).toBeNull();
  });

  it("剥掉 javascript: 伪协议链接", () => {
    const body = bodyOf('<a href="javascript:alert(1)">点我</a>');
    expect(body.querySelector("a")?.getAttribute("href")).toBeNull();
    // 文字要留着：用户至少还能看到这封信写了什么
    expect(body.textContent).toContain("点我");
  });

  it("移除 iframe / object / embed / form / base 这类可执行或可劫持的容器", () => {
    const body = bodyOf(
      '<iframe src="https://evil.example"></iframe>' +
        '<object data="x.swf"></object>' +
        '<embed src="x.swf">' +
        '<form action="https://evil.example"><input name="pw"></form>' +
        '<base href="https://evil.example/">',
    );
    for (const tag of ["iframe", "object", "embed", "form", "base", "input"]) {
      expect(body.querySelector(tag), `${tag} 不应存在`).toBeNull();
    }
  });

  it("拒绝 data:text/html 这类可执行载荷，只放行 data: 图片", () => {
    const body = bodyOf(
      '<img src="data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==">' +
        '<img src="data:image/png;base64,iVBORw0KGgo=">',
    );
    const sources = [...body.querySelectorAll("img")].map((img) => img.getAttribute("src") ?? "");
    expect(sources.some((src) => src.startsWith("data:text/html"))).toBe(false);
    expect(sources).toContain("data:image/png;base64,iVBORw0KGgo=");
  });

  it("挡住 svg / math 里的 mXSS 载荷", () => {
    const body = bodyOf(
      "<svg><script>alert(1)</script></svg>" +
        '<math><mi xlink:href="data:x,&lt;script&gt;alert(2)&lt;/script&gt;"></mi></math>',
    );
    expect(body.querySelector("script")).toBeNull();
    expect(body.innerHTML).not.toContain("alert(1)");
  });

  it("默认阻断远程图片，换成可解码的透明占位并记下原地址", () => {
    const tracker = "https://tracker.example/open.gif?id=abc";
    const { html, hadRemoteImages } = buildDocument(`<img src="${tracker}">`, "html", false);
    const img = parse(html).body.querySelector("img");

    expect(hadRemoteImages).toBe(true);
    expect(img?.getAttribute("src")).toMatch(/^data:image\/gif;base64,/);
    expect(img?.getAttribute("data-blocked-src")).toBe(tracker);
  });

  it("协议相对地址（//host/x.png）同样算远程图片", () => {
    const { hadRemoteImages } = buildDocument('<img src="//tracker.example/x.png">', "html", false);
    expect(hadRemoteImages).toBe(true);
  });

  it("用户点了「仍要加载」之后才放行原始图片地址", () => {
    const tracker = "https://tracker.example/open.gif";
    const body = bodyOf(`<img src="${tracker}">`, true);
    expect(body.querySelector("img")?.getAttribute("src")).toBe(tracker);
  });

  it("不误伤内联附件图片（cid:）与普通图文", () => {
    const body = bodyOf('<p><b>账单</b><img src="cid:logo@1"></p>');
    expect(body.querySelector("img")?.getAttribute("src")).toBe("cid:logo@1");
    expect(body.querySelector("b")?.textContent).toBe("账单");
  });

  it("给外链补上 target=_blank 与 noopener", () => {
    const link = bodyOf('<a href="https://example.com">x</a>').querySelector("a");
    expect(link?.getAttribute("target")).toBe("_blank");
    expect(link?.getAttribute("rel")).toContain("noopener");
  });
});

describe("buildDocument · 纯文本正文", () => {
  it("整段转义，不产生任何元素", () => {
    const { html, hadRemoteImages } = buildDocument("<script>alert(1)</script>", "text", false);
    const body = parse(html).body;

    expect(body.querySelector("script")).toBeNull();
    expect(body.textContent).toContain("<script>alert(1)</script>");
    expect(hadRemoteImages).toBe(false);
  });
});

describe("buildDocument · 文档外壳", () => {
  it("带上 default-src 'none' 的 CSP —— 挡住清洗漏掉的外链资源", () => {
    const { html } = buildDocument("<p>hi</p>", "html", false);
    const csp = parse(html)
      .querySelector('meta[http-equiv="Content-Security-Policy"]')
      ?.getAttribute("content");

    expect(csp).toContain("default-src 'none'");
    expect(csp).not.toContain("script-src");
  });
});
