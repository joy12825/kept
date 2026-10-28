import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken, setWorkspaceId } from '../../api/client';
import { ClassifierPanel } from './classifier';

const user = { id: 1, username: 'wang', displayName: '小王', isAdmin: false, disabled: false, createdAt: '2024-03-05T10:00:00Z', twoFactorEnabled: false };
const rule = { algorithm: 'auto', match: '', caseInsensitive: true };

function backend(role: string, status: object) {
  return stubFetch({
    'GET /api/v1/me': () => jsonResponse(200, { user, workspace: { id: 5, name: '王家', kind: 'family', color: 'green', role, memberCount: 2 }, csrfToken: 'tok-1' }),
    'GET /api/v1/classifier': () => jsonResponse(200, status),
    'POST /api/v1/classifier/train': () => jsonResponse(202, { jobId: 9 }),
    'GET /api/v1/document-types': () => jsonResponse(200, [{ id: 3, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule }]),
    'GET /api/v1/correspondents': () => jsonResponse(200, [{ id: 7, name: '京东', initial: 'J', sortKey: 'jingdong', rule }]),
    'GET /api/v1/tags': () => jsonResponse(200, []),
  });
}

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

describe('设置 → 自动分类', () => {
  it('显示模型状态和各对象的样本数，可以重新训练', async () => {
    const calls = backend('editor', {
      trainedAt: '2026-10-02T08:00:00Z', documentCount: 90, minDocuments: 10, training: false,
      items: [{ kind: 'type', id: 3, samples: 66, learned: true }, { kind: 'correspondent', id: 7, samples: 1, learned: false }],
      applying: { jobId: 4, status: 'succeeded', done: 90, total: 90, changed: 12 },
    });
    renderWithProviders(<ClassifierPanel />);
    expect(await screen.findByText('模型可用')).toBeInTheDocument();
    expect(screen.getByText(/用了 90 份已整理的文档/)).toBeInTheDocument();
    const table = screen.getByRole('table', { name: '参与自动学习的对象' });
    expect(await within(table).findByText('账单')).toBeInTheDocument();
    expect(within(table).getByText('已学会')).toBeInTheDocument();
    expect(within(table).getByText('样本不足')).toBeInTheDocument();
    expect(screen.getByText('上次重新分类：90 份文档，改动了 12 份。')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '重新训练' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'POST'));
  });

  it('没有模型时说明原因；只看的成员没有按钮', async () => {
    backend('viewer', { trainedAt: '2026-10-02T08:00:00Z', documentCount: 3, minDocuments: 10, training: false, skipped: 'too_few_documents', items: [] });
    renderWithProviders(<ClassifierPanel />);
    expect(await screen.findByText('还没有可用的模型')).toBeInTheDocument();
    expect(screen.getByText(/已整理的文档还不够多：至少需要 10 份/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '重新训练' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '对全部文档重新分类' })).not.toBeInTheDocument();
  });
});
