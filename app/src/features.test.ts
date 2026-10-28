import { describe, expect, it } from 'vitest';
import { isPrototypeRoute } from './features';

describe('isPrototypeRoute', () => {
  it('已接入真实接口的设置页不是示例数据', () => {
    for (const p of ['/settings/profile', '/settings/password', '/settings/token', '/settings/ui', '/settings/accounts', '/settings/members', '/settings/activity', '/settings/admin-activity', '/settings/brand', '/settings/pair', '/settings/devices', '/settings/system', '/settings/logs', '/settings/notify', '/settings/ai', '/settings/workflows', '/directory', '/inbox', '/files', '/trash', '/tasks', '/duplicates', '/highlights', '/docs/1', '/directory/tags/3', '/', '/drawers/3', '/review', '/read/3', '/settings/private', '/health', '/visits/2', '/docs/1/lab']) {
      expect(isPrototypeRoute(p)).toBe(false);
    }
  });
  it('尚未接入的页面是示例数据', () => {
    for (const p of ['/settings/unknown', '/somewhere']) {
      expect(isPrototypeRoute(p)).toBe(true);
    }
  });
});
