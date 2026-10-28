import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { ConfirmDialog } from '../../components/overlays';
import { StorageLocations } from './storage';
import { EncryptionSection } from './storage-encryption';

const local = { name: 'local', kind: 'local', source: 'local', location: '/data/media', documents: 3, workspaces: ['王家'], selectable: true, description: '' };
const nas = { name: 'nas', kind: 'dir', source: 'env', location: '/mnt/nas/kept', documents: 0, workspaces: [], selectable: true, description: '' };
const oss = { name: 'oss', kind: 's3', source: 'page', location: 's3://kept', documents: 9, workspaces: ['王家'], selectable: true, description: '',
  encryption: { enabled: true, locked: false, converting: true, progress: { total: 10, converted: 3, skipped: 1, failed: 0 } } };
const CODE = 'ABCDE-FGHJK-MNPQR-STVWX-YZ012';

afterEach(() => { vi.unstubAllGlobals(); });

describe('云端加密（v1.11.0）', () => {
  it.skipIf(!eeBuild)('列表显示加密状态；开启加密后只显示一次恢复码，勾选已保存才能关', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [local, nas, oss] }),
      'PUT /api/v1/admin/storages/nas/encryption': () => jsonResponse(200, { recoveryCode: CODE, jobId: 7 }),
    });
    renderWithProviders(<><StorageLocations /><ConfirmDialog /></>);
    const sec = await screen.findByRole('region', { name: '文件放在哪里' });
    const rows = within(sec).getAllByRole('row');
    expect(within(rows[1]).queryByRole('button', { name: /加密/ })).not.toBeInTheDocument(); // 数据目录
    expect(within(rows[2]).getByText('未加密')).toBeInTheDocument();
    expect(within(rows[3]).getByText('正在加密 4/10')).toBeInTheDocument();
    expect(within(rows[3]).getByRole('button', { name: '关闭加密' })).toBeInTheDocument();

    await userEvent.click(within(rows[2]).getByRole('button', { name: '开启加密' }));
    const confirm = await screen.findByRole('dialog');
    expect(within(confirm).getByText(/可读导出和收件夹的文件不加密/)).toBeInTheDocument();
    await userEvent.click(within(confirm).getByRole('button', { name: '开启' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT' && r.url.endsWith('/nas/encryption')));
    expect(await put.clone().json()).toEqual({ enabled: true });
    expect(await screen.findByLabelText('恢复码')).toHaveTextContent(CODE);
    const done = screen.getByRole('button', { name: '完成' });
    expect(done).toBeDisabled();
    await userEvent.click(screen.getByLabelText('我已经抄下或保存了恢复码'));
    await userEvent.click(done);
    await vi.waitFor(() => expect(screen.queryByText(CODE)).not.toBeInTheDocument());
  });

  it.skipIf(!eeBuild)('新加的 S3 存储默认加密：保存后开启', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/storages': () => jsonResponse(200, { items: [local] }),
      'POST /api/v1/admin/storages': () => new Response(null, { status: 204 }),
      'PUT /api/v1/admin/storages/minio/encryption': () => jsonResponse(200, { jobId: 8 }),
    });
    renderWithProviders(<StorageLocations />);
    const sec = await screen.findByRole('region', { name: '文件放在哪里' });
    await userEvent.click(within(sec).getByRole('button', { name: '添加' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText(/加密存放/)).toBeChecked();
    await userEvent.click(within(dialog).getByLabelText('服务器上的目录'));
    expect(within(dialog).getByLabelText(/加密存放/)).not.toBeChecked(); // 本机目录默认不加密
    await userEvent.click(within(dialog).getByLabelText('S3 兼容存储'));
    await userEvent.type(within(dialog).getByLabelText(/名称/), 'minio');
    await userEvent.type(within(dialog).getByRole('textbox', { name: /^桶/ }), 'kept');
    await userEvent.type(within(dialog).getByLabelText(/Access Key/), 'ak');
    await userEvent.type(within(dialog).getByLabelText(/Secret Key/), 'sk');
    await userEvent.click(within(dialog).getByRole('button', { name: '保存' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT' && r.url.endsWith('/minio/encryption')));
    expect(await put.clone().json()).toEqual({ enabled: true });
  });

  it('主密钥变了：输入恢复码解开', async () => {
    const calls = stubFetch({
      'GET /api/v1/admin/storage-encryption': () => jsonResponse(200, { recovery: true, recoveryAt: '2026-10-18T08:00:00Z', locked: ['oss'] }),
      'POST /api/v1/admin/storage-encryption/recover': () => jsonResponse(200, { recovered: 1 }),
    });
    renderWithProviders(<EncryptionSection />);
    const sec = await screen.findByRole('region', { name: '加密存放' });
    expect(within(sec).getByText(/存储 oss 的钥匙解不开/)).toBeInTheDocument();
    expect(within(sec).getByRole('button', { name: '重新生成恢复码' })).toBeDisabled();
    await userEvent.type(within(sec).getByLabelText('恢复码'), CODE.toLowerCase());
    await userEvent.click(within(sec).getByRole('button', { name: '解开' }));
    const post = await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && r.url.endsWith('/recover')));
    expect(await post.clone().json()).toEqual({ code: CODE.toLowerCase() });
  });

  it('还没有恢复码时说明什么时候生成', async () => {
    stubFetch({ 'GET /api/v1/admin/storage-encryption': () => jsonResponse(200, { recovery: false, locked: [] }) });
    renderWithProviders(<EncryptionSection />);
    expect(await screen.findByText('还没有恢复码：第一次给存储开启加密时生成。')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '重新生成恢复码' })).not.toBeInTheDocument();
  });
});
