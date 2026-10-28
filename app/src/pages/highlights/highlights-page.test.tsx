import { eeBuild } from '@ee';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import HighlightsPage from './highlights-page';

const h = (id: number, documentId: number, documentTitle: string, extra: Record<string, unknown> = {}) => ({
  id, documentId, documentTitle, locator: 'epubcfi(/6/2!/4/4,/1:0,/1:5)', quote: '', note: '', color: 'yellow', createdAt: '2024-10-01T08:00:00Z', updatedAt: '2024-10-01T08:00:00Z', ...extra,
});

afterEach(() => { vi.unstubAllGlobals(); });

describe('我的批注', () => {
  it('按书分组；搜索与颜色筛选；打开到阅读器的那个位置', async () => {
    const calls = stubFetch({
      'GET /api/v1/highlights': () => jsonResponse(200, { items: [
        h(3, 7, '唐诗选读', { quote: '感时花溅泪', note: '安史之乱', color: 'pink' }),
        h(2, 8, '红楼梦', { locator: 'page:12', note: '判词' }),
        h(1, 7, '唐诗选读', { quote: '床前明月光' }),
      ] }),
    });
    renderWithProviders(<Routes><Route path="/highlights" element={<HighlightsPage />} /><Route path="/read/:id" element={<div>阅读器</div>} /></Routes>, { route: '/highlights' });
    const poems = await screen.findByRole('region', { name: '唐诗选读' });
    expect(within(poems).getByText('感时花溅泪')).toBeInTheDocument();
    expect(within(poems).getByText('床前明月光')).toBeInTheDocument();
    expect(within(screen.getByRole('region', { name: '红楼梦' })).getByText(/第 12 页/)).toBeInTheDocument();

    await userEvent.type(screen.getByRole('textbox', { name: '搜索摘录和批注' }), '花溅{Enter}');
    await userEvent.click(screen.getByRole('button', { name: '粉色' }));
    await waitFor(() => {
      const hs = calls.filter((r) => r.url.includes('/highlights'));
      const q = new URL(hs[hs.length - 1].url).searchParams;
      expect([q.get('q'), q.get('color')]).toEqual(['花溅', 'pink']);
    });

    await userEvent.click(within(await screen.findByRole('region', { name: '唐诗选读' })).getAllByRole('button', { name: '打开' })[0]);
    expect(await screen.findByText('阅读器')).toBeInTheDocument();
  });

  it('只看一本书', async () => {
    const calls = stubFetch({ 'GET /api/v1/highlights': () => jsonResponse(200, { items: [h(1, 7, '唐诗选读', { quote: '床前明月光' })] }) });
    renderWithProviders(<Routes><Route path="/highlights" element={<HighlightsPage />} /></Routes>, { route: '/highlights?doc=7' });
    expect(await screen.findByText('只看《唐诗选读》')).toBeInTheDocument();
    expect(new URL(calls.find((r) => r.url.includes('/highlights'))!.url).searchParams.get('documentId')).toBe('7');
  });

  it.skipIf(!eeBuild)('复习卡片：今天到期的逐张复习，记得或忘了', async () => {
    const card = (id: number, front: string, back: string) => ({ id, documentId: 7, documentTitle: '唐诗选读', front, back, quote: back, dueOn: '2026-10-06', intervalDays: 0, reps: 0, lapses: 0, createdAt: '2026-10-06T00:00:00Z' });
    const calls = stubFetch({
      'GET /api/v1/highlights': () => jsonResponse(200, { items: [h(1, 7, '唐诗选读', { quote: '感时花溅泪' })] }),
      'GET /api/v1/agents/settings': () => jsonResponse(200, { enabled: true, library: true }),
      'GET /api/v1/review-cards': () => jsonResponse(200, { items: [card(1, '《春望》里，花为什么溅泪？', '感时花溅泪'), card(2, '下一句？', '恨别鸟惊心')], total: 5, due: 2 }),
      'POST /api/v1/review-cards/1/review': () => jsonResponse(200, { dueOn: '2026-10-07' }),
      'POST /api/v1/review-cards/2/review': () => jsonResponse(200, { dueOn: '2026-10-07' }),
      'POST /api/v1/review-cards/generate': () => jsonResponse(202, { jobId: 3 }),
    });
    renderWithProviders(<Routes><Route path="/highlights" element={<HighlightsPage />} /></Routes>, { route: '/highlights' });
    const bar = await screen.findByRole('region', { name: '复习卡片' });
    expect(within(bar).getByText('今天有 2 张要复习，一共 5 张')).toBeInTheDocument();
    await userEvent.click(within(await screen.findByRole('region', { name: '唐诗选读' })).getByRole('button', { name: '做成复习卡片' }));
    await vi.waitUntil(() => calls.find((r) => r.method === 'POST' && r.url.endsWith('/review-cards/generate')));

    await userEvent.click(within(bar).getByRole('button', { name: '开始复习' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('《春望》里，花为什么溅泪？')).toBeInTheDocument();
    expect(within(dialog).queryByText('感时花溅泪')).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '显示答案' }));
    expect(within(dialog).getByText('感时花溅泪')).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: '记得' }));
    await userEvent.click(await within(dialog).findByRole('button', { name: '显示答案' }));
    await userEvent.click(within(dialog).getByRole('button', { name: '忘了' }));
    expect(await within(dialog).findByText('复习了 2 张，记得 1 张。忘了的明天再来。')).toBeInTheDocument();
    const bodies = await Promise.all(calls.filter((r) => r.url.includes('/review')).filter((r) => r.method === 'POST' && !r.url.endsWith('generate')).map((r) => r.clone().json()));
    expect(bodies).toEqual([{ remembered: true }, { remembered: false }]);
  });
});
