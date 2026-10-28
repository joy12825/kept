// 产品标志（v1.13.6）：没改过名时是收好的标志；改过名、没有 Logo 时是名称的第一个字；有 Logo 时是 Logo。
import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { BrandMark } from './icons';

afterEach(() => { vi.unstubAllGlobals(); });

describe('BrandMark', () => {
  it('默认名称：收好的标志', async () => {
    stubFetch({ 'GET /api/v1/branding': () => jsonResponse(200, { name: '收好' }) });
    renderWithProviders(<BrandMark />);
    expect(await screen.findByRole('img', { name: '收好' })).toHaveAttribute('src', '/brand-mark.svg');
  });

  it('改过名、没有 Logo：名称的第一个字', async () => {
    stubFetch({ 'GET /api/v1/branding': () => jsonResponse(200, { name: '王家文件柜' }) });
    renderWithProviders(<BrandMark />);
    expect(await screen.findByText('王')).toBeInTheDocument();
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('有 Logo：用 Logo', async () => {
    stubFetch({ 'GET /api/v1/branding': () => jsonResponse(200, { name: '某某工作室', logoUrl: '/api/v1/branding/logo?v=2' }) });
    renderWithProviders(<BrandMark />);
    expect(await screen.findByRole('img', { name: '某某工作室' })).toHaveAttribute('src', '/api/v1/branding/logo?v=2');
  });
});
