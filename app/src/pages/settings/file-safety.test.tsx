import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { LocalBackupSection, backupStatus, coldStatus, placeStatus } from './file-safety';
import { InstanceStoragePanel } from './storage';
import { ConfirmDialog } from '../../components/overlays';
import type { AdminWorkspace, BackupTarget } from '../../api/storage';

const now = new Date('2026-10-26T09:00:00');
const space = (name: string, storage: object = {}) => ({ id: name.length, name, kind: 'family', color: 'green', members: 2, owners: '小王',
  usage: { originals: 0, searchable: 0, thumbnails: 0, versions: 0, trash: 0, total: 0, documents: 0, quota: 0, quotaDefault: true, countedAt: '2026-10-26T00:00:00Z' },
  storage: { name: 'local', pending: 0, moving: false, ...storage } }) as unknown as AdminWorkspace;
const target = (extra: Partial<BackupTarget> = {}): BackupTarget => ({ storage: '', keep: 7, eligible: [], ...extra });

afterEach(() => { vi.unstubAllGlobals(); });

describe('文件安全的三张卡片（v1.13.9）', () => {
  it('每天多存一份：没开、存到另一块硬盘（各版本）、存到网络位置（收费版）的成功、失败、跳过', () => {
    const none = { snapshots: 0 };
    expect(backupStatus(target(), none, true, now)).toEqual({ state: 'off', status: '还没开启。服务器的硬盘坏了，文件就找不回来了。' });
    // 免费版也能存到另一块硬盘，不再说“收费版才有”
    expect(backupStatus(target(), none, false, now)).toEqual({ state: 'off', status: '还没开启。可以在下面选服务器上的另一块硬盘，每天存一份；收费版还能存到 NAS 或网盘。' });
    const disk = { dir: '/mnt/usb/kept', snapshots: 3 };
    expect(backupStatus(target(), disk, false, now).status).toBe('已开启：每天 02:30 存到 /mnt/usb/kept，还没运行过。');
    expect(backupStatus(target(), { ...disk, last: { status: 'succeeded', at: '2026-10-26T02:30:00', documents: 812 } }, false, now))
      .toEqual({ state: 'on', status: '6 小时前已多存一份到 /mnt/usb/kept，812 份文档。' });
    expect(backupStatus(target(), { ...disk, last: { status: 'failed', at: '2026-10-26T02:30:00', error: 'disk full' } }, false, now).state).toBe('warn');
    expect(backupStatus(target(), { ...disk, last: { status: 'succeeded', at: '2026-10-26T02:30:00', documents: 8, missing: 1 } }, false, now).state).toBe('warn');
    // 存到网络位置时先说它，另一块硬盘附在后面
    expect(backupStatus(target({ storage: 'nas' }), none, true, now).status).toBe('已开启：每天 02:45 存到“nas”，还没运行过。');
    const ok = backupStatus(target({ storage: 'nas', last: { status: 'succeeded', at: '2026-10-25T02:45:00', documents: 1234 } }), disk, true, now);
    expect(ok).toEqual({ state: 'on', status: '昨天 02:45已多存一份到“nas”，1234 份文档。另外每天 02:30 存到 /mnt/usb/kept。' });
    expect(backupStatus(target({ storage: 'nas', last: { status: 'failed', at: '2026-10-25T02:45:00', error: 'disk full' } }), none, true, now).state).toBe('warn');
    expect(backupStatus(target({ storage: 'nas', last: { status: 'succeeded', at: '2026-10-25T02:45:00', skipped: '当时没有设置目标' } }), none, true, now).status).toBe('最近一次没有存（昨天 02:45）：当时没有设置目标');
    expect(backupStatus(target({ storage: 'nas', last: { status: 'succeeded', at: '2026-10-25T02:45:00', documents: 9, missing: 2 } }), none, true, now).state).toBe('warn');
  });

  it('旧文件放到网盘：收费版、没开、一个空间、几个空间', () => {
    expect(coldStatus([space('王家')], false).state).toBe('paid');
    expect(coldStatus([space('王家')], true)).toEqual({ state: 'off', status: '还没开启。服务器放得下就不用开。' });
    const one = [space('王家', { cold: 'oss', coldAfterMonths: 24 }), space('工作室')];
    expect(coldStatus(one, true)).toEqual({ state: 'on', status: '“王家”超过 24 个月的文档放在“oss”。' });
    const two = [space('王家', { cold: 'oss', coldAfterMonths: 24 }), space('工作室', { cold: 'oss', coldAfterMonths: 12 })];
    expect(coldStatus(two, true).status).toBe('2 个空间已开启，例如“王家”超过 24 个月的文档放在“oss”。');
  });

  it('整个换个地方放：都在数据目录、正在移动、还有没移完的、放在别处', () => {
    expect(placeStatus([space('王家')]).status).toBe('文档都放在服务器的数据目录里。');
    expect(placeStatus([space('王家', { name: 'nas', moving: true, progress: { total: 10, moved: 3, skipped: 1, failed: 0 } })]).status).toBe('正在把“王家”的文档移到“nas”：4/10。');
    expect(placeStatus([space('王家', { name: 'nas', pending: 5 })])).toEqual({ state: 'warn', status: '“王家”还有 5 份文档在原来的位置。' });
    expect(placeStatus([space('王家', { name: 'nas' }), space('工作室')]).status).toBe('“王家”的文档放在“nas”，其余在服务器的数据目录。');
  });

  it('页面：先说不用同步，三张卡片写着现状，“设置”跳到对应的分区', async () => {
    stubFetch({
      'GET /api/v1/admin/backup-target': () => jsonResponse(200, target({ storage: 'nas', eligible: ['nas'], last: { status: 'failed', at: '2026-10-25T02:45:00', error: 'disk full' } })),
      'GET /api/v1/admin/workspaces': () => jsonResponse(200, { defaultQuota: 0, items: [space('王家', { cold: 'oss', coldAfterMonths: 24 })] }),
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [{ name: 'local', kind: 'local', location: '/data/media', documents: 3, workspaces: ['王家'], source: 'local', selectable: true, description: '' }] }),
      'GET /api/v1/admin/storage-encryption': () => jsonResponse(200, { recovery: false, locked: [] }),
      'GET /api/v1/admin/export-target': () => jsonResponse(200, { storage: '', folder: '', sensitive: false, choices: [], runs: [] }),
      'GET /api/v1/admin/system': () => jsonResponse(200, { backup: { snapshots: 0 } }),
    });
    const scrolled: string[] = [];
    // jsdom 没有 scrollIntoView：临时挂一个，记下跳到了哪个分区
    HTMLElement.prototype.scrollIntoView = function (this: HTMLElement) { scrolled.push(this.id); };
    renderWithProviders(<InstanceStoragePanel />);
    expect(screen.getByRole('heading', { name: '文件安全' })).toBeInTheDocument();
    expect(screen.getByText(/不用同步/)).toBeInTheDocument();
    const cards = await screen.findByRole('region', { name: '想做什么' });
    const backup = within(cards).getByRole('article', { name: '每天多存一份' });
    expect(within(backup).getByText(eeBuild ? '备份到存储 · 每天 02:45' : '本机备份 · 每天 02:30')).toBeInTheDocument();
    if (eeBuild) {
      expect(await within(backup).findByText(/最近一次没存成功/)).toBeInTheDocument();
      expect(within(backup).getByText('要留意')).toBeInTheDocument();
      await userEvent.click(within(backup).getByRole('button', { name: '设置' }));
      expect(scrolled).toEqual(['safety-backup']);
      const cold = within(cards).getByRole('article', { name: '旧文件放到网盘' });
      expect(await within(cold).findByText('“王家”超过 24 个月的文档放在“oss”。')).toBeInTheDocument();
    } else {
      // 开源构建没有存到网络位置的分区：“设置”跳到“存到另一块硬盘”
      await userEvent.click(within(backup).getByRole('button', { name: '设置' }));
      expect(scrolled).toEqual(['safety-local']);
    }
    expect(screen.getByRole('region', { name: '存到另一块硬盘' })).toBeInTheDocument();
    const place = within(cards).getByRole('article', { name: '整个换个地方放' });
    await userEvent.click(within(place).getByRole('button', { name: '设置' }));
    expect(scrolled[scrolled.length - 1]).toBe('safety-spaces');
    delete (HTMLElement.prototype as Partial<HTMLElement>).scrollIntoView;
  });

  it('存到另一块硬盘：选目录保存，停止前确认；部署时设置的只读', async () => {
    let saved: unknown[] = [];
    let backup: object = { snapshots: 0 };
    stubFetch({
      'GET /api/v1/admin/system': () => jsonResponse(200, { backup }),
      'PUT /api/v1/admin/backup': async (req) => {
        const body = (await req.json()) as { dir: string };
        saved = [...saved, body.dir];
        backup = body.dir ? { dir: body.dir, snapshots: 0 } : { snapshots: 0 };
        return new Response(null, { status: 204 });
      },
    });
    renderWithProviders(<><LocalBackupSection /><ConfirmDialog /></>);
    const sec = await screen.findByRole('region', { name: '存到另一块硬盘' });
    expect(within(sec).getByText(/本机备份/)).toBeInTheDocument();
    expect(within(sec).queryByRole('button', { name: '立即备份' })).not.toBeInTheDocument();
    await userEvent.type(within(sec).getByLabelText('备份目录'), '/mnt/usb/kept');
    await userEvent.click(within(sec).getByRole('button', { name: '保存' }));
    await vi.waitUntil(() => saved.length === 1);
    expect(saved).toEqual(['/mnt/usb/kept']);
    expect(await within(sec).findByRole('button', { name: '立即备份' })).toBeInTheDocument();
    await userEvent.click(within(sec).getByRole('button', { name: '停止' }));
    const ask = await screen.findByRole('dialog');
    expect(within(ask).getByText('已经备份的文件留在那个目录里，不会删除。')).toBeInTheDocument();
    await userEvent.click(within(ask).getByRole('button', { name: '停止' }));
    await vi.waitUntil(() => saved.length === 2);
    expect(saved).toEqual(['/mnt/usb/kept', '']);
  });

  it('存到另一块硬盘：部署时设置了 KEPT_BACKUP_DIR，只显示不让改', async () => {
    stubFetch({ 'GET /api/v1/admin/system': () => jsonResponse(200, { backup: { dir: '/srv/backup', dirFromEnv: true, snapshots: 2 } }) });
    renderWithProviders(<LocalBackupSection />);
    const sec = await screen.findByRole('region', { name: '存到另一块硬盘' });
    expect(within(sec).getByText('部署时设置的目录：/srv/backup。要改请修改部署配置（KEPT_BACKUP_DIR）。')).toBeInTheDocument();
    expect(within(sec).queryByLabelText('备份目录')).not.toBeInTheDocument();
    expect(within(sec).getByRole('button', { name: '立即备份' })).toBeEnabled();
  });
});
