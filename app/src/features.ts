/**
 * 已接入真实接口的页面。其余页面仍显示示例数据，页面顶部有提示条。
 * 每接入一个页面，就把它加到这里，并删除对应的示例数据（见《前后端联合开发计划》六、3）。
 */
const REAL_ROUTES = ['/', '/review', '/settings/profile', '/settings/password', '/settings/token', '/settings/ui', '/settings/accounts', '/settings/members', '/settings/storage', '/settings/classifier', '/settings/activity', '/settings/admin-activity', '/settings/brand', '/settings/pair', '/settings/devices', '/settings/mail', '/settings/storages', '/settings/edition', '/settings/remote-inbox', '/settings/system', '/settings/logs', '/settings/notify', '/settings/ai', '/settings/workflows', '/settings/sharing', '/settings/links', '/settings/upload-links', '/settings/emergency', '/settings/emergency-access', '/settings/private', '/settings/scenes', '/scenes', '/health', '/health/report', '/directory', '/inbox', '/files', '/trash', '/cleanup', '/duplicates', '/tasks', '/highlights', '/book-lists', '/ask', '/checklists'];

/** 路径以这些开头的页面也已接入（详情页、目录条目页、抽屉页）。 */
const REAL_PREFIXES = ['/docs/', '/directory/', '/drawers/', '/shared/', '/shared-collections/', '/read/', '/book-lists/', '/ask/', '/asn/', '/checklists/', '/visits/', '/scenes/'];

export function isPrototypeRoute(pathname: string): boolean {
  return !REAL_ROUTES.includes(pathname) && !REAL_PREFIXES.some((p) => pathname.startsWith(p));
}
