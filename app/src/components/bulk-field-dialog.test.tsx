import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { BulkFieldDialog } from './bulk-field-dialog';

afterEach(() => { vi.unstubAllGlobals(); });

const fields = [
  { id: 50, name: '金额', initial: 'J', sortKey: 'jine', dataType: 'monetary', extra: { currency: 'CNY' }, builtIn: true },
  { id: 60, name: '报销状态', initial: 'B', sortKey: 'baoxiao', dataType: 'select', extra: { options: [{ id: 'a', label: '未报销' }, { id: 'b', label: '已报销' }] }, builtIn: false },
  { id: 61, name: '关联合同', initial: 'G', sortKey: 'guanlian', dataType: 'document_link', extra: {}, builtIn: false },
];

describe('批量设字段', () => {
  it('选字段和值后设置；可以清空', async () => {
    const calls = stubFetch({
      'GET /api/v1/custom-fields': () => jsonResponse(200, fields),
      'POST /api/v1/documents/bulk': () => jsonResponse(200, { count: 3 }),
    });
    const onDone = vi.fn();
    renderWithProviders(<BulkFieldDialog open selection={{ ids: [1, 2, 3] }} count={3} onClose={() => {}} onDone={onDone} />);
    await userEvent.click(await screen.findByRole('combobox', { name: '字段' }));
    expect(screen.queryByRole('option', { name: '关联合同' })).not.toBeInTheDocument(); // links are per document
    await userEvent.click(await screen.findByRole('option', { name: '报销状态' }));
    expect(screen.getByRole('button', { name: '设置' })).toBeDisabled();
    await userEvent.click(screen.getByRole('combobox', { name: '报销状态' }));
    await userEvent.click(await screen.findByRole('option', { name: '已报销' }));
    await userEvent.click(screen.getByRole('button', { name: '设置' }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(await calls.find((c) => c.method === 'POST')!.clone().json()).toEqual({ ids: [1, 2, 3], edit: { customFields: [{ fieldId: 60, value: 'b' }] } });
  });
});
