import { describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '../test/utils';

vi.mock('pdfjs-dist', () => ({
  GlobalWorkerOptions: {},
  getDocument: () => ({
    promise: Promise.resolve({
      numPages: 3,
      getPage: () => Promise.resolve({
        getViewport: () => ({ width: 160, height: 90 }),
        render: () => ({ promise: Promise.resolve(), cancel: () => {} }),
      }),
    }),
    destroy: () => Promise.resolve(),
  }),
}));

const { SlideView } = await import('./slide-view');

describe('幻灯片（v1.9.7）', () => {
  it('一张一张地看：按钮、方向键、缩略图都能翻', async () => {
    renderWithProviders(<SlideView url="/api/v1/documents/7/preview" shortcuts />);
    const slide = await screen.findByRole('group', { name: '第 1 张，共 3 张' });
    expect(screen.getByRole('button', { name: '上一张' })).toBeDisabled();
    expect(screen.getAllByRole('button', { name: /^第 \d 张$/ })).toHaveLength(3);
    await userEvent.click(screen.getByRole('button', { name: '下一张' }));
    expect(slide).toHaveAccessibleName('第 2 张，共 3 张');
    await userEvent.keyboard('{ArrowRight}');
    expect(slide).toHaveAccessibleName('第 3 张，共 3 张');
    expect(screen.getByRole('button', { name: '下一张' })).toBeDisabled();
    await userEvent.keyboard('{ArrowRight}');
    expect(slide).toHaveAccessibleName('第 3 张，共 3 张');
    await userEvent.click(screen.getByRole('button', { name: '第 1 张' }));
    expect(slide).toHaveAccessibleName('第 1 张，共 3 张');
    expect(screen.getByRole('button', { name: '第 1 张' })).toHaveAttribute('aria-current', 'true');
    await userEvent.keyboard('{End}');
    expect(slide).toHaveAccessibleName('第 3 张，共 3 张');
  });
});
