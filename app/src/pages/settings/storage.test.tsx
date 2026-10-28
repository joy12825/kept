import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import { StorageLocations, StoragePanel, WorkspacesStorage } from './storage';

const me = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };

const GB = 1 << 30;
const usage = (extra: object = {}) => ({ originals: 3 * GB, searchable: GB, thumbnails: 50 << 20, versions: 0, trash: 900 << 20, total: 4.93 * GB, documents: 812,
  quota: 5 * GB, quotaDefault: false, countedAt: '2026-10-06T00:00:00Z', ...extra });

const wsStorage = (extra: object = {}) => ({ name: 'local', pending: 0, moving: false, ...extra });
const localStorage_ = { name: 'local', kind: 'local', location: '/data/media', documents: 812, workspaces: ['王家'] };
const nas = { name: 'nas', kind: 'dir', location: '/mnt/nas/kept', documents: 0, workspaces: [] };
const minio = { name: 'minio', kind: 's3', location: 's3://kept/docs（http://192.168.1.10:9000）', documents: 5, workspaces: [] };

afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

describe('存储', () => {
  it('分项列出用量；快满时提示', async () => {
    stubFetch({ 'GET /api/v1/storage': () => jsonResponse(200, usage()) });
    renderWithProviders(<StoragePanel />);
    expect(await screen.findByText('4.9 GB')).toBeInTheDocument();
    expect(screen.getByText('已用 99%，配额 5.0 GB')).toBeInTheDocument();
    expect(screen.getByText('812 份文档')).toBeInTheDocument();
    expect(screen.getByText('900 MB')).toBeInTheDocument();
    expect(screen.getByText(/空间快满了/)).toBeInTheDocument();
  });

  it('没有配额时不显示进度', async () => {
    stubFetch({ 'GET /api/v1/storage': () => jsonResponse(200, usage({ quota: 0, quotaDefault: true })) });
    renderWithProviders(<StoragePanel />);
    expect(await screen.findByText('已用，这个空间没有配额')).toBeInTheDocument();
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
  });

  it('空间管理员可以导出整个空间，选择导出原件', async () => {
    setWorkspaceId(5);
    stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage()),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 }, csrfToken: 't' }),
    });
    const clicked: string[] = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) { clicked.push(this.getAttribute('href') ?? ''); });
    renderWithProviders(<StoragePanel />);
    const sec = await screen.findByRole('region', { name: '导出整个空间' });
    await userEvent.click(within(sec).getByLabelText('上传时的原件'));
    await userEvent.click(within(sec).getByRole('button', { name: '导出' }));
    expect(clicked).toEqual(['/api/v1/export?file=original&workspace=5']);
    click.mockRestore();
  });

  it('旧版本：空间管理员限制每份文档保留的数量，其他成员只看（v1.7.13）', async () => {
    let saved: unknown;
    stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage({ keepVersions: 0 })),
      'PUT /api/v1/storage/versions': async (req) => { saved = await req.json(); return new Response(null, { status: 204 }); },
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 }, csrfToken: 't' }),
    });
    renderWithProviders(<StoragePanel />);
    const sec = await screen.findByRole('region', { name: '旧版本' });
    expect(await within(sec).findByLabelText('全部保留')).toBeChecked();
    expect(within(sec).getByRole('button', { name: '保存' })).toBeDisabled();
    await userEvent.click(within(sec).getByLabelText('限制数量'));
    const n = within(sec).getByLabelText('每份文档最多保留');
    await userEvent.clear(n);
    await userEvent.type(n, '0');
    expect(within(sec).getByText('填 1 到 1000 之间的整数')).toBeInTheDocument();
    await userEvent.clear(n);
    await userEvent.type(n, '3');
    await userEvent.click(within(sec).getByRole('button', { name: '保存' }));
    await vi.waitFor(() => expect(saved).toEqual({ keep: 3 }));
  });

  it('旧版本：不是空间管理员时只显示设置', async () => {
    stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage({ keepVersions: 5 })),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 't' }),
    });
    renderWithProviders(<StoragePanel />);
    const sec = await screen.findByRole('region', { name: '旧版本' });
    expect(await within(sec).findByText('每份文档最多保留 5 个旧版本')).toBeInTheDocument();
    expect(within(sec).queryByRole('button', { name: '保存' })).not.toBeInTheDocument();
  });

  it('不是空间管理员时没有导出', async () => {
    stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage()),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 }, csrfToken: 't' }),
    });
    renderWithProviders(<StoragePanel />);
    await screen.findByText('812 份文档');
    expect(screen.queryByRole('region', { name: '导出整个空间' })).not.toBeInTheDocument();
  });

  it.skipIf(!eeBuild)('实例管理员修改空间的配额', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/workspaces': () => jsonResponse(200, { defaultQuota: 20 * GB, items: [
        { id: 5, name: '王家', kind: 'family', color: 'green', members: 3, owners: '小王', usage: usage({ quota: 20 * GB, quotaDefault: true }), storage: wsStorage() },
      ] }),
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [localStorage_] }),
      'PUT /api/v1/admin/workspaces/5/quota': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<WorkspacesStorage />);
    const table = await screen.findByRole('region', { name: '各空间放在哪' });
    expect(within(table).getByText('20.0 GB（默认）')).toBeInTheDocument();
    // 只有数据目录时不显示存储一列
    expect(within(table).queryByRole('button', { name: '换位置' })).not.toBeInTheDocument();
    await userEvent.click(within(table).getByRole('button', { name: '改配额' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('radio', { name: '跟随默认（20.0 GB）' })).toBeChecked();
    await userEvent.click(within(dialog).getByRole('radio', { name: '指定上限' }));
    expect(within(dialog).getByRole('button', { name: '保存' })).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText('上限'), '2');
    expect(within(dialog).getByText(/比已用的少/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    expect(await put.clone().json()).toEqual({ mode: 'limit', bytes: 2 * GB });
  });

  it('列出存储位置，测试能否读写；没有配置的存储给出提示', async () => {
    stubFetch({
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [localStorage_, nas, minio, { name: 'old', kind: 'missing', location: '', documents: 3, workspaces: [] }] }),
      'POST /api/v1/admin/storages/nas/test': () => jsonResponse(200, { ok: false, tookMs: 2, error: '写入失败：permission denied' }),
      'POST /api/v1/admin/storages/local/test': () => jsonResponse(200, { ok: true, tookMs: 4 }),
    });
    renderWithProviders(<StorageLocations />);
    const sec = await screen.findByRole('region', { name: '文件放在哪里' });
    expect(within(sec).getByText('/mnt/nas/kept')).toBeInTheDocument();
    expect(within(sec).getByText('本机目录')).toBeInTheDocument();
    expect(within(sec).getByText('S3 兼容')).toBeInTheDocument();
    expect(within(sec).getByText(/有 3 份文档放在存储“old”里/)).toBeInTheDocument();
    const rows = within(sec).getAllByRole('row');
    expect(rows).toHaveLength(5);
    expect(within(rows[4]).queryByRole('button', { name: '测试' })).not.toBeInTheDocument();
    await userEvent.click(within(rows[1]).getByRole('button', { name: '测试' }));
    expect(await within(rows[1]).findByText('可以读写 · 4 ms')).toBeInTheDocument();
    await userEvent.click(within(rows[2]).getByRole('button', { name: '测试' }));
    expect(await within(rows[2]).findByText('写入失败：permission denied')).toBeInTheDocument();
  });

  it('给空间换存储，并把已有文档移过去；显示移动进度', async () => {
    let moving = false;
    const calls = stubFetch({
      'GET /api/v1/admin/workspaces': () => jsonResponse(200, { defaultQuota: 0, items: [
        { id: 5, name: '王家', kind: 'family', color: 'green', members: 3, owners: '小王', usage: usage(),
          storage: moving ? wsStorage({ name: 'nas', pending: 812, moving: true, progress: { total: 812, moved: 100, skipped: 0, failed: 0 } }) : wsStorage() },
      ] }),
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [localStorage_, nas] }),
      'PUT /api/v1/admin/workspaces/5/storage': () => { moving = true; return jsonResponse(200, { jobId: 9 }); },
    });
    renderWithProviders(<WorkspacesStorage />);
    const table = await screen.findByRole('region', { name: '各空间放在哪' });
    await userEvent.click(await within(table).findByRole('button', { name: '换位置' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('button', { name: '保存' })).toBeDisabled();
    expect(within(dialog).queryByLabelText('把已有的文档也移过去')).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('radio', { name: /nas/ }));
    expect(within(dialog).getByLabelText('把已有的文档也移过去')).toBeChecked();
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    expect(await put.clone().json()).toEqual({ storage: 'nas', move: true });
    expect(await within(table).findByText('正在移动 100/812')).toBeInTheDocument();
  });

  it('实例管理员添加 S3 存储：先测试连接再保存', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [{ ...localStorage_, source: 'local', selectable: true, description: '' },
        { ...nas, source: 'env', selectable: true, description: '' }] }),
      'POST /api/v1/admin/storage-test': () => jsonResponse(200, { ok: true, tookMs: 12 }),
      'POST /api/v1/admin/storages': () => new Response(null, { status: 204 }),
      'PUT /api/v1/admin/storages/minio/encryption': () => jsonResponse(200, {}),
    });
    renderWithProviders(<StorageLocations />);
    const sec = await screen.findByRole('region', { name: '文件放在哪里' });
    // 部署配置的只能测试
    const rows = within(sec).getAllByRole('row');
    expect(within(rows[2]).getByText(/部署配置/)).toBeInTheDocument();
    expect(within(rows[2]).queryByRole('button', { name: '修改' })).not.toBeInTheDocument();

    await userEvent.click(within(sec).getByRole('button', { name: '添加' }));
    const dialog = await screen.findByRole('dialog');
    const save = within(dialog).getByRole('button', { name: '保存' });
    await userEvent.type(within(dialog).getByLabelText(/名称/), 'minio');
    await userEvent.type(within(dialog).getByLabelText(/说明/), '家里的 NAS');
    await userEvent.type(within(dialog).getByLabelText(/服务地址/), 'http://192.168.1.10:9000');
    await userEvent.type(within(dialog).getByRole('textbox', { name: /^桶/ }), 'kept');
    await userEvent.type(within(dialog).getByLabelText(/Access Key/), 'ak');
    expect(save).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Secret Key/), 'sk');
    await userEvent.type(within(dialog).getByLabelText(/容量上限/), '500');
    await userEvent.click(within(dialog).getByRole('button', { name: '测试连接' }));
    expect(await within(dialog).findByText('可以读写 · 12 ms')).toBeInTheDocument();
    await userEvent.click(save);
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && r.url.endsWith('/admin/storages')));
    expect(await post.clone().json()).toEqual({ name: 'minio', kind: 's3', description: '家里的 NAS', selectable: true, secretKey: 'sk', capacity: 500 * 2 ** 30,
      settings: { endpoint: 'http://192.168.1.10:9000', region: '', bucket: 'kept', prefix: '', pathStyle: true, accessKey: 'ak' } });
  });

  it('修改已放了文档的存储：桶不能改，密钥留空不变', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [{ name: 'minio', kind: 's3', source: 'page', location: 's3://kept', description: '家里的 NAS',
        selectable: true, documents: 3, workspaces: ['王家'], settings: { endpoint: 'http://192.168.1.10:9000', bucket: 'kept', pathStyle: true, accessKey: 'ak' } }] }),
      'PUT /api/v1/admin/storages/minio': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<StorageLocations />);
    const sec = await screen.findByRole('region', { name: '文件放在哪里' });
    await userEvent.click(within(sec).getByRole('button', { name: '修改' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(/已经放了 3 份文档/)).toBeInTheDocument();
    expect(within(dialog).getByRole('textbox', { name: /^桶/ })).toBeDisabled();
    await userEvent.clear(within(dialog).getByLabelText(/服务地址/));
    await userEvent.type(within(dialog).getByLabelText(/服务地址/), 'http://192.168.1.11:9000');
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    const body = await put.clone().json();
    expect(body.secretKey).toBeUndefined();
    expect(body.settings.endpoint).toBe('http://192.168.1.11:9000');
  });

  it('空间管理员给自己的空间换存储', async () => {
    setWorkspaceId(5);
    const calls = stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage()),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 }, csrfToken: 't' }),
      'GET /api/v1/storage/location': () => jsonResponse(200, { current: wsStorage(), choices: [
        { name: 'local', kind: 'local', description: '' }, { name: 'minio', kind: 's3', description: '家里的 NAS' }] }),
      'PUT /api/v1/storage/location': () => jsonResponse(200, { jobId: 3 }),
    });
    renderWithProviders(<StoragePanel />);
    const sec = await screen.findByRole('region', { name: '文档放在哪里' });
    expect(within(sec).getByText('服务器的数据目录（local）')).toBeInTheDocument();
    await userEvent.click(within(sec).getByRole('button', { name: '更换' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('radio', { name: '家里的 NAS（minio）' }));
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    expect(await put.clone().json()).toEqual({ storage: 'minio', move: true });
  });

  it('移动文档时显示进度，结束后保留上次的结果，可以到任务记录里看（v1.7.14）', async () => {
    setWorkspaceId(5);
    let current: object = wsStorage({ name: 'minio', pending: 812, moving: true, progress: { total: 812, moved: 200, skipped: 3, failed: 1 } });
    stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage()),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 }, csrfToken: 't' }),
      'GET /api/v1/storage/location': () => jsonResponse(200, { current, choices: [
        { name: 'local', kind: 'local', description: '' }, { name: 'minio', kind: 's3', description: '家里的 NAS' }] }),
    });
    const { unmount } = renderWithProviders(<StoragePanel />);
    const sec = await screen.findByRole('region', { name: '文档放在哪里' });
    expect(within(sec).getByRole('progressbar', { name: '移动文档' })).toHaveAttribute('aria-valuenow', '25');
    expect(within(sec).getByText(/正在移动文档：204\/812 份，3 份正在处理，稍后再移，1 份失败/)).toBeInTheDocument();
    expect(within(sec).getByRole('link', { name: '在任务记录里查看' })).toHaveAttribute('href', '/tasks?group=storage');
    unmount();

    current = wsStorage({ name: 'minio', last: { jobId: 9, finishedAt: '2026-10-12T13:30:00Z', total: 812, moved: 812, skipped: 0, failed: 0 } });
    renderWithProviders(<StoragePanel />);
    const again = await screen.findByRole('region', { name: '文档放在哪里' });
    expect(await within(again).findByText(/上次移动：.*完成，移了 812 份。/)).toBeInTheDocument();
    expect(within(again).queryByRole('progressbar')).not.toBeInTheDocument();
  });

  it('只有数据目录时空间设置里不显示存储', async () => {
    setWorkspaceId(5);
    stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage()),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 }, csrfToken: 't' }),
      'GET /api/v1/storage/location': () => jsonResponse(200, { current: wsStorage(), choices: [{ name: 'local', kind: 'local', description: '' }] }),
    });
    renderWithProviders(<StoragePanel />);
    await screen.findByRole('region', { name: '导出整个空间' });
    expect(screen.queryByRole('region', { name: '文档放在哪里' })).not.toBeInTheDocument();
  });

  it('添加 WebDAV 存储：地址、用户名和密码', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [{ ...localStorage_, source: 'local', selectable: true, description: '' }] }),
      'POST /api/v1/admin/storages': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<StorageLocations />);
    const sec = await screen.findByRole('region', { name: '文件放在哪里' });
    await userEvent.click(within(sec).getByRole('button', { name: '添加' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('radio', { name: 'WebDAV（网盘、NAS）' }));
    expect(within(dialog).queryByRole('textbox', { name: /^桶/ })).not.toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText(/名称/), 'jgy');
    const save = within(dialog).getByRole('button', { name: '保存' });
    await userEvent.type(within(dialog).getByLabelText(/^地址/), 'dav.jianguoyun.com/dav/kept');
    expect(save).toBeDisabled(); // 地址要带 https://
    await userEvent.clear(within(dialog).getByLabelText(/^地址/));
    await userEvent.type(within(dialog).getByLabelText(/^地址/), 'https://dav.jianguoyun.com/dav/kept');
    await userEvent.type(within(dialog).getByLabelText(/用户名/), 'wang@example.com');
    await userEvent.type(within(dialog).getByLabelText(/^密码/), 'app-pass');
    await userEvent.click(save);
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST'));
    expect(await post.clone().json()).toEqual({ name: 'jgy', kind: 'webdav', description: '', selectable: true, secretKey: 'app-pass', capacity: 0,
      settings: { url: 'https://dav.jianguoyun.com/dav/kept', username: 'wang@example.com' } });
  });

  it.skipIf(!eeBuild)('空间管理员把旧文档移到冷存储', async () => {
    setWorkspaceId(5);
    const calls = stubFetch({
      'GET /api/v1/storage': () => jsonResponse(200, usage()),
      'GET /api/v1/me': () => jsonResponse(200, { user: me, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role: 'owner', memberCount: 2 }, csrfToken: 't' }),
      'GET /api/v1/storage/location': () => jsonResponse(200, { current: wsStorage(), choices: [
        { name: 'local', kind: 'local', description: '' }, { name: 'oss', kind: 's3', description: '阿里云归档' }] }),
      'PUT /api/v1/storage/cold': () => jsonResponse(200, { jobId: 8 }),
    });
    renderWithProviders(<StoragePanel />);
    const sec = await screen.findByRole('region', { name: '文档放在哪里' });
    expect(within(sec).getByText('和新文档放在一起')).toBeInTheDocument();
    await userEvent.click(within(sec).getByRole('button', { name: '设置' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('combobox', { name: '移到' }));
    await userEvent.click(await screen.findByRole('option', { name: '阿里云归档（oss）' }));
    await userEvent.clear(within(dialog).getByLabelText(/超过/));
    await userEvent.type(within(dialog).getByLabelText(/超过/), '36');
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    expect(await put.clone().json()).toEqual({ storage: 'oss', months: 36 });
  });
});

