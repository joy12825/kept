// 打开文档的链接（v1.6.0，参照 paperless-ngx）：写成真正的链接，⌘ / Ctrl 点、Shift 点、中键点由浏览器在新标签页或新窗口打开；
// 普通点击照旧在当前页打开，由调用方记下来源列表（上一份 / 下一份、返回的位置）。

/** 链接的 onClick：普通左键点击时阻止浏览器跳转，改为调用 open；带修饰键或非左键时交给浏览器。 */
export function docLinkClick(open: () => void) {
  return (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
    e.preventDefault();
    open();
  };
}

export const docHref = (id: number) => `/docs/${id}`;
