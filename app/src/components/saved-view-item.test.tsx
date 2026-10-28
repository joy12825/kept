import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { List } from '@mui/material';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { SavedViewItem } from './saved-view-item';

afterEach(() => { vi.unstubAllGlobals(); });

describe('侧栏的常用筛选', () => {
  it('显示符合条件的份数', async () => {
    const view = { id: 3, name: '待报销', filter: { field: 'tag', op: 'has_any', value: [32] }, sort: 'created:desc', display: {}, createdBy: 1, creatorName: '小王', createdAt: '2024-10-01T08:00:00Z' };
    const calls = stubFetch({
      'GET /api/v1/me': () => jsonResponse(200, { user: { id: 1 }, workspace: { id: 5, role: 'editor' }, csrfToken: 't' }),
      'GET /api/v1/documents': () => jsonResponse(200, { items: [], total: 7 }),
    });
    renderWithProviders(<List><SavedViewItem view={view} active={false} onApply={() => {}} /></List>);
    expect(await screen.findByLabelText('7 份')).toHaveTextContent('7');
    await waitFor(() => {
      const q = new URL(calls.find((r) => new URL(r.url).pathname === '/api/v1/documents')!.url).searchParams;
      expect(JSON.parse(q.get('filter')!)).toEqual(view.filter);
      expect(q.get('limit')).toBe('1');
    });
  });
});
