import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken, setWorkspaceId } from '../api/client';
import { useApp } from '../store/app-store';
import { SameNameDialog, TaskTray, UploadDialog } from './uploads';

/** A fake XMLHttpRequest that records requests and answers when told to. */
class FakeXHR {
  static all: FakeXHR[] = [];
  method = ''; url = ''; headers: Record<string, string> = {}; body: FormData | null = null; status = 0; responseText = '';
  withCredentials = false;
  upload: { onprogress: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null; onerror: (() => void) | null = null;
  constructor() { FakeXHR.all.push(this); }
  open(method: string, url: string) { this.method = method; this.url = url; }
  setRequestHeader(k: string, v: string) { this.headers[k] = v; }
  send(body: FormData) { this.body = body; }
  respond(status: number, body: unknown) { this.status = status; this.responseText = JSON.stringify(body); this.onload?.(); }
}

const ws = { id: 5, name: '王家', kind: 'family', color: 'green', role: 'editor', memberCount: 2 };
const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };

function backend(jobs: unknown[] = []) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok-1' }),
    'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 3, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule: { algorithm: 'auto', match: '', caseInsensitive: true } }]),
    'GET /api/v1/correspondents': () => jsonResponse(200, []),
    'GET /api/v1/jobs': () => jsonResponse(200, jobs),
  });
}

function OpenDialog() {
  const app = useApp();
  return <button onClick={() => app.set({ uploadOpen: true })}>打开</button>;
}

afterEach(() => { vi.unstubAllGlobals(); FakeXHR.all = []; setCsrfToken(null); setWorkspaceId(null); });

describe('UploadDialog', () => {
  it('选择文件和预设类型后上传：预设字段在文件之前，带 CSRF 和空间请求头', async () => {
    backend();
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    setCsrfToken('tok-1');
    setWorkspaceId(5);
    renderWithProviders(<><OpenDialog /><UploadDialog /><TaskTray /></>);
    await userEvent.click(screen.getByText('打开'));
    const dialog = await screen.findByRole('dialog', { name: '上传文档' });
    expect(within(dialog).getByRole('button', { name: '上传' })).toBeDisabled();
    await userEvent.upload(within(dialog).getByLabelText('上传文件'), [new File(['%PDF-1.4'], '电费.pdf', { type: 'application/pdf' })]);
    expect(within(dialog).getByText('电费.pdf')).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('combobox', { name: /类型/ }));
    await userEvent.click(await screen.findByRole('option', { name: '账单' }));
    await userEvent.click(within(dialog).getByRole('button', { name: '上传 1 个文件' }));

    await waitFor(() => expect(FakeXHR.all).toHaveLength(1));
    const xhr = FakeXHR.all[0];
    expect(xhr.method).toBe('POST');
    expect(xhr.url).toBe('/api/v1/documents');
    expect(xhr.headers['X-CSRF-Token']).toBe('tok-1');
    expect(xhr.headers['X-Workspace-ID']).toBe('5');
    expect([...xhr.body!.keys()]).toEqual(['documentTypeId', 'file']);
    expect(xhr.body!.get('documentTypeId')).toBe('3');

    // 托盘先显示上传进度
    act(() => xhr.upload.onprogress?.({ lengthComputable: true, loaded: 50, total: 100 }));
    expect(await screen.findByText('上传中 50%')).toBeInTheDocument();
    act(() => xhr.respond(409, { code: 'duplicate', message: '这份文件已经存在', details: { documentId: 42 } }));
    expect(await screen.findByText('这份文件已经存在，点击查看已有的文档')).toBeInTheDocument();
  });
});

describe('UploadDialog 的更多预设（v1.9.12）', () => {
  it('家人、标签、日期、原件编号和自定义字段都带进表单', async () => {
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok-1' }),
      'GET /api/v1/document-types': () => jsonResponse(200, []),
      'GET /api/v1/correspondents': () => jsonResponse(200, []),
      'GET /api/v1/jobs': () => jsonResponse(200, []),
      'GET /api/v1/persons': () => jsonResponse(200, [{ id: 60, name: '爸爸', initial: 'B', sortKey: 'baba', rule: { algorithm: 'auto', match: '', caseInsensitive: true } }]),
      'GET /api/v1/tags': () => jsonResponse(200, [{ id: 7, name: '报销', color: '', sortKey: 'baoxiao', initial: 'B', documentCount: 0, rule: { algorithm: 'auto', match: '', caseInsensitive: true } }]),
      'GET /api/v1/custom-fields': () => jsonResponse(200, [
        { id: 3, name: '金额', dataType: 'monetary', extra: {}, builtin: true },
        { id: 4, name: '关联文档', dataType: 'document_link', extra: {} },
      ]),
    });
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    renderWithProviders(<><OpenDialog /><UploadDialog /></>);
    await userEvent.click(screen.getByText('打开'));
    const dialog = await screen.findByRole('dialog', { name: '上传文档' });
    await userEvent.upload(within(dialog).getByLabelText('上传文件'), [new File(['x'], '发票.pdf', { type: 'application/pdf' })]);
    await userEvent.click(within(dialog).getByRole('button', { name: /更多预设/ }));

    await userEvent.click(within(dialog).getByRole('combobox', { name: /家人/ }));
    await userEvent.click(await screen.findByRole('option', { name: '爸爸' }));
    await userEvent.click(within(dialog).getByRole('combobox', { name: '标签' }));
    await userEvent.click(await screen.findByRole('option', { name: '报销' }));
    await userEvent.type(within(dialog).getByLabelText('日期'), '2024-03-05');
    await userEvent.type(within(dialog).getByLabelText('原件编号'), '0');
    expect(within(dialog).getByText('1 到 999999 的整数')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: '上传 1 个文件' })).toBeDisabled();
    await userEvent.clear(within(dialog).getByLabelText('原件编号'));
    await userEvent.type(within(dialog).getByLabelText('原件编号'), '12');

    await userEvent.click(within(dialog).getByRole('button', { name: '添加字段' }));
    expect(screen.queryByRole('menuitem', { name: '关联文档' })).not.toBeInTheDocument(); // 上传时还没有文档可链接
    await userEvent.click(await screen.findByRole('menuitem', { name: '金额' }));
    await userEvent.type(within(dialog).getByLabelText('金额'), '326.4');
    await userEvent.click(within(dialog).getByRole('button', { name: '上传 1 个文件' }));

    await waitFor(() => expect(FakeXHR.all).toHaveLength(1));
    const body = FakeXHR.all[0].body!;
    expect(body.get('personId')).toBe('60');
    expect(body.get('tagIds')).toBe('7');
    expect(body.get('createdDate')).toBe('2024-03-05');
    expect(body.get('asn')).toBe('12');
    expect(JSON.parse(body.get('customFields') as string)).toEqual([{ fieldId: 3, value: '326.40' }]);
    const keys = [...body.keys()];
    expect(keys[keys.length - 1]).toBe('file'); // 预设在文件之前
  });

  it('多个文件时不能填原件编号', async () => {
    backend();
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    renderWithProviders(<><OpenDialog /><UploadDialog /></>);
    await userEvent.click(screen.getByText('打开'));
    const dialog = await screen.findByRole('dialog', { name: '上传文档' });
    await userEvent.upload(within(dialog).getByLabelText('上传文件'), [new File(['a'], 'a.pdf'), new File(['b'], 'b.pdf')]);
    await userEvent.click(within(dialog).getByRole('button', { name: /更多预设/ }));
    expect(within(dialog).getByLabelText('原件编号')).toBeDisabled();
    expect(within(dialog).getByText('上传一个文件时才能填')).toBeInTheDocument();
  });
});

function Where() {
  return <div data-testid="where">{useLocation().pathname + useLocation().search}</div>;
}

describe('TaskTray', () => {
  it('上传完成后显示入库任务的进度；完成的点击跳到待整理里的那一份', async () => {
    backend([
      { id: 7, kind: 'ingest.document', status: 'running', stage: 'ocr', progress: 40, filename: 'scan.png', createdAt: '2024-03-05T10:00:00Z' },
      { id: 8, kind: 'ingest.document', status: 'succeeded', stage: 'done', progress: 100, filename: 'bill.pdf', documentId: 99, createdAt: '2024-03-05T10:00:00Z' },
      { id: 9, kind: 'ingest.document', status: 'failed', stage: 'inspect', progress: 20, filename: 'x.pdf', error: 'PDF 已加密，请先移除密码再上传', createdAt: '2024-03-05T10:00:00Z' },
    ]);
    function Seed() {
      const app = useApp();
      return <button onClick={() => app.set({ uploads: [
        { key: 'a', name: 'scan.png', pct: 100, jobId: 7 },
        { key: 'b', name: 'bill.pdf', pct: 100, jobId: 8 },
        { key: 'c', name: 'x.pdf', pct: 100, jobId: 9 },
      ] })}>seed</button>;
    }
    renderWithProviders(
      <Routes>
        <Route path="*" element={<><Seed /><TaskTray /><Where /></>} />
      </Routes>,
    );
    await userEvent.click(screen.getByText('seed'));
    expect(await screen.findByText('识别文字')).toBeInTheDocument();
    expect(screen.getByText('已进入待整理')).toBeInTheDocument();
    expect(screen.getByText('PDF 已加密，请先移除密码再上传')).toBeInTheDocument();
    expect(screen.getByText('正在处理 3 个文件')).toBeInTheDocument();
    await userEvent.click(screen.getByText('bill.pdf'));
    expect(screen.getByTestId('where')).toHaveTextContent('/inbox?doc=99');
  });

  it('处理失败、文件还在时可以重试：同一个任务回到排队（v1.9.12）', async () => {
    let status = 'failed';
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok-1' }),
      'GET /api/v1/jobs': () => jsonResponse(200, [status === 'failed'
        ? { id: 9, kind: 'ingest.document', status: 'failed', stage: 'inspect', progress: 20, filename: 'x.pdf', error: '处理失败，请稍后重试', retryable: true, createdAt: '2024-03-05T10:00:00Z' }
        : { id: 9, kind: 'ingest.document', status: 'queued', stage: 'queued', progress: 0, filename: 'x.pdf', createdAt: '2024-03-05T10:00:00Z' }]),
      'POST /api/v1/jobs/9/retry': () => { status = 'queued'; return new Response(null, { status: 204 }); },
    });
    function Seed() {
      const app = useApp();
      return <button onClick={() => app.set({ uploads: [{ key: 'c', name: 'x.pdf', pct: 100, jobId: 9 }] })}>seed</button>;
    }
    renderWithProviders(<><Seed /><TaskTray /></>);
    await userEvent.click(screen.getByText('seed'));
    await userEvent.click(await screen.findByRole('button', { name: '重试' }));
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/api/v1/jobs/9/retry'))).toBe(true);
    expect(await screen.findByText('排队中')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument();
  });
});

describe('加密的 PDF', () => {
  it('没有密码时托盘里出现“输入密码”，输入后用同一个文件带上密码重新上传', async () => {
    backend();
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    renderWithProviders(<><OpenDialog /><UploadDialog /><TaskTray /></>);
    await userEvent.click(screen.getByText('打开'));
    const dialog = await screen.findByRole('dialog', { name: '上传文档' });
    await userEvent.upload(within(dialog).getByLabelText('上传文件'), [new File(['%PDF-1.4 locked'], '工资单.pdf', { type: 'application/pdf' })]);
    await userEvent.click(within(dialog).getByRole('button', { name: '上传 1 个文件' }));
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1));
    act(() => FakeXHR.all[0].respond(400, { code: 'pdf_encrypted', message: '这个 PDF 有密码，请输入密码后重新上传' }));

    expect(await screen.findByText('这个 PDF 有密码，请输入密码后重新上传')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '上传文档' })).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: '输入密码' }));
    const pw = await screen.findByRole('dialog', { name: '输入 PDF 密码' });
    await userEvent.type(within(pw).getByLabelText('密码'), 'secret{Enter}');

    await waitFor(() => expect(FakeXHR.all).toHaveLength(2));
    const again = FakeXHR.all[1].body!;
    expect(again.get('password')).toBe('secret');
    expect((again.get('file') as File).name).toBe('工资单.pdf');
    act(() => FakeXHR.all[1].respond(202, { jobId: 7 }));
    await waitFor(() => expect(screen.queryByRole('button', { name: '输入密码' })).not.toBeInTheDocument());
  });
});

describe('同名文件（v1.7.13）', () => {
  const drawers = () => jsonResponse(200, { unfiled: 0, items: [{ id: 4, kind: 'normal', name: '合同', icon: 'file', color: 'blue', description: '', documentCount: 1, alertCount: 0, createdAt: '2024-03-05T10:00:00Z' }] });

  it('在抽屉页上传：默认放进这个抽屉；有同名文档时逐个选择，勾选的作为新版本', async () => {
    let checked: unknown;
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok-1' }),
      'GET /api/v1/document-types': () => jsonResponse(200, []),
      'GET /api/v1/correspondents': () => jsonResponse(200, []),
      'GET /api/v1/jobs': () => jsonResponse(200, []),
      'GET /api/v1/drawers': drawers,
      'POST /api/v1/uploads/same-name': async (req) => {
        checked = await req.json();
        return jsonResponse(200, { auto: false, matches: [
          { name: '租房合同.pdf', documentId: 11, title: '租房合同' },
          { name: '保险单.pdf', documentId: 12, title: '车险保单' },
        ] });
      },
    });
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    renderWithProviders(<><OpenDialog /><UploadDialog /><SameNameDialog /><TaskTray /></>, { route: '/drawers/4' });
    await userEvent.click(screen.getByText('打开'));
    const dialog = await screen.findByRole('dialog', { name: '上传文档' });
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: /抽屉/ })).toHaveTextContent('合同'));
    await userEvent.upload(within(dialog).getByLabelText('上传文件'), [
      new File(['%PDF-1.4 a'], '租房合同.pdf', { type: 'application/pdf' }),
      new File(['%PDF-1.4 b'], '保险单.pdf', { type: 'application/pdf' }),
      new File(['%PDF-1.4 c'], '新的.pdf', { type: 'application/pdf' }),
    ]);
    await userEvent.click(within(dialog).getByRole('button', { name: '上传 3 个文件' }));

    const ask = await screen.findByRole('dialog', { name: '有同名的文档' });
    expect(checked).toEqual({ drawerId: 4, names: ['租房合同.pdf', '保险单.pdf', '新的.pdf'] });
    expect(within(ask).getByText('作为“车险保单”的新版本')).toBeInTheDocument();
    expect(FakeXHR.all).toHaveLength(0);
    await userEvent.click(within(ask).getByRole('checkbox', { name: /保险单\.pdf/ }));
    expect(within(ask).getByRole('checkbox', { name: '全部作为新版本' })).toHaveAttribute('data-indeterminate', 'true');
    await userEvent.click(within(ask).getByRole('button', { name: '上传' }));

    await waitFor(() => expect(FakeXHR.all).toHaveLength(3));
    const by = (n: string) => FakeXHR.all.find((x) => (x.body!.get('file') as File).name === n)!.body!;
    expect(by('租房合同.pdf').get('onConflict')).toBe('version');
    expect(by('保险单.pdf').get('onConflict')).toBe('new');
    expect(by('新的.pdf').get('onConflict')).toBe('new');
    expect(by('新的.pdf').get('drawerId')).toBe('4');
    expect([...by('租房合同.pdf').keys()]).toEqual(['drawerId', 'onConflict', 'file']);
  });

  it('抽屉设为直接作为新版本时不询问；取消询问就不上传', async () => {
    let auto = true;
    stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user, workspace: ws, csrfToken: 'tok-1' }),
      'GET /api/v1/document-types': () => jsonResponse(200, []),
      'GET /api/v1/correspondents': () => jsonResponse(200, []),
      'GET /api/v1/jobs': () => jsonResponse(200, []),
      'GET /api/v1/drawers': drawers,
      'POST /api/v1/uploads/same-name': () => jsonResponse(200, { auto, matches: [{ name: 'a.pdf', documentId: 11, title: 'A' }] }),
    });
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    renderWithProviders(<><OpenDialog /><UploadDialog /><SameNameDialog /><TaskTray /></>);
    const send = async () => {
      await userEvent.click(screen.getByText('打开'));
      const dialog = await screen.findByRole('dialog', { name: '上传文档' });
      await userEvent.upload(within(dialog).getByLabelText('上传文件'), [new File(['%PDF-1.4 a'], 'a.pdf', { type: 'application/pdf' })]);
      await userEvent.click(within(dialog).getByRole('button', { name: '上传 1 个文件' }));
    };
    await send();
    await waitFor(() => expect(FakeXHR.all).toHaveLength(1));
    expect(FakeXHR.all[0].body!.get('onConflict')).toBeNull();
    expect(FakeXHR.all[0].body!.get('drawerId')).toBeNull();
    act(() => FakeXHR.all[0].respond(202, { jobId: 7, versionOf: 11 }));

    auto = false;
    await send();
    const ask = await screen.findByRole('dialog', { name: '有同名的文档' });
    await userEvent.click(within(ask).getByRole('button', { name: '取消' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '有同名的文档' })).not.toBeInTheDocument());
    expect(FakeXHR.all).toHaveLength(1);
  });

  it('托盘：作为新版本的显示“已作为新版本”，点击打开那份文档', async () => {
    backend([{ id: 8, kind: 'ingest.reprocess', status: 'succeeded', stage: 'done', progress: 100, filename: 'a.pdf', documentId: 11, createdAt: '2024-03-05T10:00:00Z' }]);
    function Seed() {
      const app = useApp();
      return <button onClick={() => app.set({ uploads: [{ key: 'a', name: 'a.pdf', pct: 100, jobId: 8, versionOf: 11 }] })}>seed</button>;
    }
    renderWithProviders(<Routes><Route path="*" element={<><Seed /><TaskTray /><Where /></>} /></Routes>);
    await userEvent.click(screen.getByText('seed'));
    await userEvent.click(await screen.findByText('已作为新版本'));
    expect(screen.getByTestId('where')).toHaveTextContent('/docs/11');
  });
});
