import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setWorkspaceId } from '../../api/client';
import { EditPanel } from './edit-panel';

const rule = { algorithm: 'auto', match: '', caseInsensitive: true };
afterEach(() => { vi.unstubAllGlobals(); setWorkspaceId(null); });

describe('类型的保留期限（v1.6.5）', () => {
  it('改为 5 年后保存', async () => {
    const calls = stubFetch({
      'PATCH /api/v1/document-types/10': async (r) => jsonResponse(200, { id: 10, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule, ...(await r.clone().json()) }),
    });
    renderWithProviders(<EditPanel kind="document-types" item={{ id: 10, name: '账单', initial: 'Z', sortKey: 'zhangdan', rule, retentionMonths: 24 } as never}
      readOnly={false} tags={[]} tagsById={new Map()} onClose={() => {}} />);
    const select = await screen.findByRole('combobox', { name: '保留期限' });
    expect(select).toHaveTextContent('2 年');
    await userEvent.click(select);
    await userEvent.click(await screen.findByRole('option', { name: '5 年' }));
    await userEvent.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(calls.some((r) => r.method === 'PATCH')).toBe(true));
    expect(await calls.find((r) => r.method === 'PATCH')!.clone().json()).toMatchObject({ name: '账单', retentionMonths: 60 });
  });
});
