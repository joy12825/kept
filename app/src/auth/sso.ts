// 单点登录（v1.0 阶段 35）：转到身份服务登录；失败时服务端转回 /?sso=原因，登录页据此提示。
import { t } from '../i18n';

const MESSAGES: Record<string, string> = {
  no_account: t('这个账号在收好里没有对应的账号。请联系管理员用你的邮箱开通账号。'),
  disabled: t('你的账号已停用，请联系管理员。'),
  unverified: t('身份服务没有确认你的邮箱，无法找到对应的账号。请联系管理员。'),
  taken: t('这个邮箱对应的账号已经绑定了身份服务里的另一个人。请联系管理员解除绑定。'),
  unavailable: t('暂时连不上身份服务，请稍后再试，或用密码登录。'),
  cancelled: t('已取消单点登录。'),
  off: t('这台服务器没有配置单点登录。'),
  failed: t('单点登录没有完成，请再试一次。'),
};

// 读过一次就记住：开发模式的 StrictMode 会把 useState 的初始化调用两次，地址栏里的参数第一次就去掉了。
// 只记 2 秒，免得以后退出登录回到同一个地址时又出现。
let taken: { href: string; message: string; at: number } | null = null;

/** 读出地址里的 ?sso= 原因并从地址栏去掉；没有时为 null。 */
export function takeSSOError(): string | null {
  if (taken && taken.href === window.location.href && Date.now() - taken.at < 2000) return taken.message;
  const url = new URL(window.location.href);
  const reason = url.searchParams.get('sso');
  if (!reason) return null;
  url.searchParams.delete('sso');
  window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash);
  taken = { href: window.location.href, message: MESSAGES[reason] ?? MESSAGES.failed, at: Date.now() };
  return taken.message;
}

/** 单点登录的起点：带上“记住我”和登录后回到的页面。 */
export function ssoStartUrl(remember: boolean): string {
  const next = window.location.pathname + window.location.search + window.location.hash;
  return `/api/v1/auth/sso/start?remember=${remember}&next=${encodeURIComponent(next)}`;
}
