import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { optionMatches, Picker } from './pickers';

afterEach(() => { vi.unstubAllGlobals(); });

describe('选择面板（v1.6.0）', () => {
  it('按名称（不分大小写）、完整路径或拼音查找', () => {
    const o = { id: 1, label: 'Apple 发票', title: '财务 / Apple 发票', sortKey: 'applefapiao' };
    expect(optionMatches(o, 'apple')).toBe(true);
    expect(optionMatches(o, '财务')).toBe(true);
    expect(optionMatches(o, 'fa piao')).toBe(true);
    expect(optionMatches(o, '水电')).toBe(false);
  });

  it('名称不存在时可以新建，建好直接选上；已有同名时不出现', async () => {
    const onCreate = vi.fn(async () => 42);
    function Host() {
      const [el, setEl] = useState<HTMLElement | null>(null);
      const [picked, setPicked] = useState<number[]>([]);
      return (
        <>
          <button onClick={(e) => setEl(e.currentTarget)}>打开</button>
          <p>已选 {picked.join(',')}</p>
          <Picker anchor={el} onClose={() => setEl(null)} options={[{ id: 1, label: '水电', sortKey: 'shuidian' }]} selected={picked} multi
            onPick={(id) => setPicked((p) => [...p, id])} placeholder="搜索标签" onCreate={onCreate} />
        </>
      );
    }
    stubFetch({});
    renderWithProviders(<Host />);
    await userEvent.click(screen.getByRole('button', { name: '打开' }));
    await userEvent.type(screen.getByRole('textbox', { name: '搜索标签' }), '水电');
    expect(screen.queryByText(/新建“/)).not.toBeInTheDocument();
    await userEvent.clear(screen.getByRole('textbox', { name: '搜索标签' }));
    await userEvent.type(screen.getByRole('textbox', { name: '搜索标签' }), '物业费');
    await userEvent.click(screen.getByText('新建“物业费”'));
    expect(onCreate).toHaveBeenCalledWith('物业费');
    await waitFor(() => expect(screen.getByText('已选 42')).toBeInTheDocument());
  });

  it('只看的地方不传 onCreate，就没有新建', async () => {
    renderWithProviders(<Picker anchor={document.body} onClose={() => {}} options={[]} selected={[]} onPick={() => {}} placeholder="搜索类型" />);
    await userEvent.type(screen.getByRole('textbox', { name: '搜索类型' }), '合同');
    expect(screen.getByText('没有符合的选项')).toBeInTheDocument();
  });
});
