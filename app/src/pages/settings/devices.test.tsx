import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId, type Workspace } from '../../api/client';
import { ConfirmDialog } from '../../components/overlays';
import { DevicesPanel, PairPanel, qrMatrix } from './devices';

const home: Workspace = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 };
const user = { id: 1, username: 'wang', displayName: '小王', email: 'wang@example.com', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const me = (ws = home) => () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok-1' });
const inMinutes = (m: number) => new Date(Date.now() + m * 60_000).toISOString();
const code = (urls = ['http://192.168.1.10:8000'], expiresAt = inMinutes(10)) => ({
  code: '12345678', expiresAt, workspaceName: '王家', urls, qr: JSON.stringify({ v: 1, urls, code: '12345678', ws: '王家' }),
});

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('PairPanel', () => {
  it('打开时生成配对码：二维码、分组显示的配对码、倒计时、空间和地址', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me(),
      'POST /api/v1/devices/pairing-codes': () => jsonResponse(201, code()),
      'GET /api/v1/devices': () => jsonResponse(200, []),
    });
    renderWithProviders(<PairPanel />);
    expect(await screen.findByText('1234 5678')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: '配对二维码' })).toBeInTheDocument();
    expect(screen.getByText(/^(10:00|09:5\d)$/)).toBeInTheDocument();
    expect(screen.getByText('“王家”空间，上传的文档进入这个空间的待整理')).toBeInTheDocument();
    expect(screen.getByText('http://192.168.1.10:8000')).toBeInTheDocument();
    expect(screen.queryByText(/手机无法访问/)).not.toBeInTheDocument();
    expect(calls.filter((r) => r.method === 'POST')).toHaveLength(1);

    await userEvent.click(screen.getByRole('button', { name: '重新生成' }));
    await vi.waitUntil(() => calls.filter((r) => r.method === 'POST').length === 2);
  });

  it('只有本机地址时提示手机无法访问；过期后可以重新生成', async () => {
    stubFetch({
      'GET /api/v1/me': me(),
      'POST /api/v1/devices/pairing-codes': () => jsonResponse(201, code(['http://localhost:5173'], inMinutes(-1))),
      'GET /api/v1/devices': () => jsonResponse(200, []),
    });
    renderWithProviders(<PairPanel />);
    expect(await screen.findByText(/这是本机地址，手机无法访问/)).toBeInTheDocument();
    expect(screen.getByText('二维码已过期')).toBeInTheDocument();
  });

  it('只看的成员也能生成配对码，说明手机只能查看', async () => {
    const calls = stubFetch({ 'GET /api/v1/me': me({ ...home, role: 'viewer' }), 'POST /api/v1/devices/pairing-codes': () => jsonResponse(201, code()), 'GET /api/v1/devices': () => jsonResponse(200, []) });
    renderWithProviders(<PairPanel />);
    expect(await screen.findByText(/手机上可以查看和搜索文档，不能上传/)).toBeInTheDocument();
    await waitFor(() => expect(calls.some((r) => r.method === 'POST')).toBe(true));
  });

  it('已连接的设备标明能否查看文档', async () => {
    stubFetch({ 'GET /api/v1/devices': () => jsonResponse(200, [
      { id: 3, name: '小王的 iPhone', workspaceId: 5, workspaceName: '王家', pairedAt: new Date().toISOString(), access: 'full' },
      { id: 2, name: '旧手机', workspaceId: 5, workspaceName: '王家', pairedAt: new Date().toISOString(), access: 'upload' },
    ]) });
    renderWithProviders(<DevicesPanel />);
    expect(await screen.findByText('可查看文档')).toBeInTheDocument();
    expect(screen.getByText('仅上传')).toBeInTheDocument();
  });

  it('手机连接上之后跳到已连接的设备', async () => {
    stubFetch({
      'GET /api/v1/me': me(),
      'POST /api/v1/devices/pairing-codes': () => jsonResponse(201, code()),
      'GET /api/v1/devices': () => jsonResponse(200, [{ id: 3, name: '小王的 iPhone', workspaceId: 5, workspaceName: '王家', pairedAt: new Date().toISOString() }]),
    });
    renderWithProviders(
      <Routes>
        <Route path="/settings/pair" element={<PairPanel />} />
        <Route path="/settings/devices" element={<p>设备列表页</p>} />
      </Routes>,
      { route: '/settings/pair' },
    );
    expect(await screen.findByText('设备列表页')).toBeInTheDocument();
  });

  it('二维码内容按 UTF-8 编码，中文空间名也能放进去', () => {
    const m = qrMatrix(JSON.stringify({ v: 1, urls: ['https://docs.example.com'], code: '12345678', ws: '王家' }));
    expect(m.length).toBeGreaterThanOrEqual(21);
    expect(m.every((row) => row.length === m.length)).toBe(true);
  });
});

describe('DevicesPanel', () => {
  it('列出设备，断开需要确认', async () => {
    const calls = stubFetch({
      'GET /api/v1/me': me(),
      'GET /api/v1/devices': () => jsonResponse(200, [
        { id: 3, name: '小王的 iPhone', workspaceId: 5, workspaceName: '王家', pairedAt: '2024-09-12T12:31:00Z', lastSeenAt: new Date(Date.now() - 3 * 60_000).toISOString() },
        { id: 4, name: '小王的 iPad', workspaceId: 1, workspaceName: '小王 的空间', pairedAt: '2024-09-13T12:31:00Z' },
      ]),
      'DELETE /api/v1/devices/3': () => new Response(null, { status: 204 }),
    });
    renderWithProviders(<><DevicesPanel /><ConfirmDialog /></>);
    expect(await screen.findByText('小王的 iPhone')).toBeInTheDocument();
    expect(screen.getByText(/上传到“王家”/)).toBeInTheDocument();
    expect(screen.getByText('最后活跃 3 分钟前')).toBeInTheDocument();
    expect(screen.getByText(/还没有使用过/)).toBeInTheDocument();

    await userEvent.click(screen.getAllByRole('button', { name: '断开' })[0]);
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByRole('heading', { name: '断开“小王的 iPhone”？' })).toBeInTheDocument();
    expect(calls.some((r) => r.method === 'DELETE')).toBe(false);
    await userEvent.click(within(dialog).getByRole('button', { name: '断开' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'DELETE'));
  });

  it('没有设备时引导去连接手机', async () => {
    stubFetch({ 'GET /api/v1/me': me(), 'GET /api/v1/devices': () => jsonResponse(200, []) });
    renderWithProviders(<DevicesPanel />);
    expect(await screen.findByText('还没有连接任何手机')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '连接手机' })).toBeInTheDocument();
  });
});
