import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { rowsToRule, type RuleRow } from '../utils/drawer-rules';
import { blankRow, RuleRowsEditor } from './rule-rows';

afterEach(() => { vi.unstubAllGlobals(); });

describe('条件编辑器（v1.6.1）', () => {
  it('所有类型的自定义字段都能作条件，例如保修截止今天起 90 天内', async () => {
    stubFetch({
      'GET /api/v1/custom-fields': () => jsonResponse(200, [{ id: 60, name: '保修截止', initial: 'B', sortKey: 'b', dataType: 'date', extra: {}, builtIn: false }]),
      'GET /api/v1/document-types': () => jsonResponse(200, []),
      'GET /api/v1/correspondents': () => jsonResponse(200, []),
      'GET /api/v1/tags': () => jsonResponse(200, []),
      'GET /api/v1/drawers': () => jsonResponse(200, { items: [] }),
    });
    let latest: RuleRow[] = [];
    function Host() {
      const [rows, setRows] = useState<RuleRow[]>([blankRow('and')]);
      latest = rows;
      return <RuleRowsEditor rows={rows} onChange={setRows} empty="没有条件" hint="" />;
    }
    renderWithProviders(<Host />);
    await userEvent.click(screen.getByLabelText('第 1 条的字段'));
    await userEvent.click(await screen.findByRole('option', { name: '保修截止' }));
    expect(screen.queryByLabelText('第 1 条的运算')).not.toBeInTheDocument(); // 字段条件有自己的“条件”
    await userEvent.type(screen.getByRole('spinbutton', { name: '第 1 条的天数' }), '90');
    await waitFor(() => expect(rowsToRule(latest)).toEqual({ field: 'cf:60', op: 'within_days', value: 90 }));
  });
});
