import DOMPurify from "dompurify";

// 邮件正文是本平台**攻击面最大**的一处：内容完全由陌生发件人控制，
// 而这是个多租户 SaaS，一次 XSS 的爆炸半径是整个平台的邮箱凭据。
// 因此做三层，任何一层单独失效都还有另外两层兜着：
//
//   1. DOMPurify 清洗 —— 去掉 script/iframe/事件属性/javascript: URL
//   2. sandbox iframe —— 不给 allow-scripts、不给 allow-same-origin，
//      即使清洗漏了什么，脚本也跑不起来，更读不到父页面的 cookie
//   3. CSP default-src 'none' —— 唯一能挡住「已经在 DOM 里的」外链资源的机制
//
// 第 2、3 层不是「多此一举」：DOMPurify 历史上出过绕过，而这两层是浏览器级别的。
//
// 本文件只做纯字符串处理、不含组件，这样净化逻辑可以直接被单测覆盖
// （见 messageDocument.test.ts，06 文档 §9 把它列为必测项）。

// 远程图片默认阻断，占位用一张 1x1 全透明 GIF。
// 邮件里的 1x1 图片是最常见的阅读追踪手段，加载它等于向发件人确认
// 「这个地址是活的、刚刚有人看了」——对本平台的用户来说，这个确认本身就是要避免的。
// 用真实可解码的图片而不是空 data URI，否则每个被拦下的图都显示成破图图标，
// 看起来像邮件坏了而不是我们有意拦的。
const BLOCKED_IMAGE =
  "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";

// 唯一允许的 data: 形态——base64 编码的图片。两处都用它，不能各写各的：
// 一处进 DOMPurify 的 ALLOWED_URI_REGEXP，一处在钩子里补 DOMPurify 的一个特例。
const DATA_IMAGE_PATTERN = String.raw`data:image\/(?:png|jpe?g|gif|webp|svg\+xml);base64,`;
const DATA_IMAGE = new RegExp(`^${DATA_IMAGE_PATTERN}`, "i");
const ALLOWED_URI = new RegExp(`^(?:https?|mailto|tel|cid|${DATA_IMAGE_PATTERN})`, "i");

export interface BuiltDocument {
  // html 是可以直接喂给 iframe srcDoc 的完整文档。
  html: string;
  // hadRemoteImages 用来决定要不要显示「已阻止远程图片」那条提示。
  hadRemoteImages: boolean;
}

// buildDocument 把邮件正文变成一个可以安全塞进 sandbox iframe 的完整文档。
export function buildDocument(
  body: string,
  bodyType: "text" | "html",
  showRemoteImages: boolean,
): BuiltDocument {
  if (bodyType !== "html") {
    return { html: wrap(escapeHTML(body), true), hadRemoteImages: false };
  }

  let hadRemoteImages = false;
  const purify = DOMPurify();

  // 钩子逐个节点处理外链资源与超链接。放在 DOMPurify 内部做，
  // 而不是清洗后再用正则改 HTML——正则处理 HTML 正是漏洞的常见来源。
  purify.addHook("uponSanitizeElement", (node) => {
    if (!(node instanceof Element)) return;
    if (node.tagName === "IMG") {
      const src = node.getAttribute("src") ?? "";
      if (isRemoteURL(src)) {
        hadRemoteImages = true;
        if (!showRemoteImages) {
          node.setAttribute("src", BLOCKED_IMAGE);
          node.setAttribute("data-blocked-src", src);
        }
      }
    }
  });
  purify.addHook("afterSanitizeAttributes", (node) => {
    if (!(node instanceof Element)) return;

    // DOMPurify 对 img/video/audio/source/track 这几个标签的 src 有个特例：
    // 只要是 data: 开头就直接放行，**根本不过 ALLOWED_URI_REGEXP**
    // （见其 _isValidAttribute 里的 DATA_URI_TAGS 分支）。
    // 也就是说光配 ALLOWED_URI_REGEXP 挡不住 data:text/html 载荷，必须在这里补一刀。
    // 这类 URI 在 <img> 里本来渲染不出东西，但「配置写了却不生效」的差距迟早会被
    // 后来放宽配置的人踩中，不如现在就让实际行为和注释说的一致。
    const src = node.getAttribute("src");
    if (src?.trim().toLowerCase().startsWith("data:") && !DATA_IMAGE.test(src.trim())) {
      node.removeAttribute("src");
    }

    if (node.tagName !== "A") return;
    // sandbox 里点链接默认哪儿也去不了，显式指到新标签页，
    // 并且加 noopener 防止目标页通过 window.opener 操纵本页。
    node.setAttribute("target", "_blank");
    node.setAttribute("rel", "noopener noreferrer nofollow");
  });

  const clean = purify.sanitize(body, {
    // 前八个 DOMPurify 默认就会去掉，写出来是为了让意图可读，
    // 也防止将来有人放宽配置时不小心把它们放回来。
    //
    // 表单控件是自己加的：DOMPurify 删掉 <form> 时默认**保留子节点**（KEEP_CONTENT），
    // 于是钓鱼邮件里那个「请重新登录」的密码框会原样渲染出来。sandbox 让它提交不出去，
    // 但一个长得像登录框的东西出现在邮件正文里，本身就是在教用户把密码往那儿填。
    FORBID_TAGS: [
      "script",
      "iframe",
      "object",
      "embed",
      "form",
      "base",
      "link",
      "meta",
      "input",
      "button",
      "select",
      "textarea",
    ],
    FORBID_ATTR: ["srcset", "ping", "formaction"],
    // data: URI 只允许图片，别的（比如 data:text/html）等于一个可执行载荷。
    // 注意这一条挡不住 img/video/audio 的 src，那几个要靠上面的钩子。
    ALLOWED_URI_REGEXP: ALLOWED_URI,
  });

  return { html: wrap(clean, false), hadRemoteImages };
}

function isRemoteURL(src: string): boolean {
  return /^(https?:)?\/\//i.test(src.trim());
}

function escapeHTML(s: string): string {
  return s
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

// wrap 把正文包成一个完整文档。
function wrap(content: string, plainText: boolean): string {
  const csp = [
    "default-src 'none'",
    "img-src data: https: http: cid:",
    "style-src 'unsafe-inline'",
    "font-src data:",
  ].join("; ");

  return `<!doctype html>
<html><head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  :root { color-scheme: light; }
  body {
    margin: 0; padding: 16px;
    font: 14px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    color: #1f2328; background: #fff;
    word-break: break-word; overflow-wrap: anywhere;
  }
  img { max-width: 100%; height: auto; }
  table { max-width: 100%; }
  pre { white-space: pre-wrap; }
  ${plainText ? "body { white-space: pre-wrap; font-family: ui-monospace, monospace; }" : ""}
</style>
</head><body>${content}</body></html>`;
}
