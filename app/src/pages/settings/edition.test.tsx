import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import { EditionNotice, EditionPanel } from './edition';
import { hasFeature, type EditionStatus } from '../../api/edition';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

const usage = { members: { used: 3, limit: 6 }, sharedWorkspaces: { used: 1, limit: 1 } };
const trial: EditionStatus = { edition: 'pro', trial: true, trialEndsAt: '2026-11-23T00:00:00Z', daysLeft: 5, warn: true, expired: false, features: ['ai', 'privacy'], instance: 'a1b2c3d4e5f60718', ...usage };
const team: EditionStatus = { edition: 'team', trial: false, daysLeft: -1, warn: false, expired: false, features: ['ai', 'sso'], instance: 'a1b2c3d4e5f60718',
  license: { id: 'L-1', edition: 'team', holder: '某公司', issued: '2026-10-24' }, members: { used: 3, limit: 0 }, sharedWorkspaces: { used: 1, limit: 0 } };
const free: EditionStatus = { edition: 'free', trial: false, daysLeft: -1, warn: false, expired: false, features: [], ...usage };

describe('EditionPanel', () => {
  it('开源构建（v1.13.1）：写明是开源版，不显示导入许可证；收费设置页说明开源版不包含', async () => {
    const oss: EditionStatus = { ...free, openSource: true, instance: 'a1b2c3d4e5f60718' };
    stubFetch({ 'GET /api/v1/edition': () => jsonResponse(200, oss) });
    renderWithProviders(<><EditionPanel /><EditionNotice feature="ai" /></>);
    expect(await screen.findByText('开源构建：不含收费模块，始终是免费版')).toBeInTheDocument();
    expect(screen.getByText('开源版')).toBeInTheDocument();
    expect(screen.getByTestId('edition-open-source')).toBeInTheDocument();
    expect(screen.queryByLabelText('许可证')).not.toBeInTheDocument();
    expect(screen.queryByText(/申请许可证时请提供实例编号/)).not.toBeInTheDocument();
    expect(await screen.findByTestId('edition-notice')).toHaveTextContent('开源的免费版不包含');
  });

  it('显示试用剩余天数、用量和实例编号；导入许可证后显示企业版', async () => {
    let now = trial;
    const calls = stubFetch({
      'GET /api/v1/edition': () => jsonResponse(200, now),
      'PUT /api/v1/admin/license': () => { now = team; return jsonResponse(200, team); },
    });
    renderWithProviders(<EditionPanel />);
    expect(await screen.findByText('收费版试用，还剩 5 天')).toBeInTheDocument();
    expect(screen.getByText('3 / 6')).toBeInTheDocument();
    expect(screen.getByTestId('instance-id')).toHaveTextContent('a1b2c3d4e5f60718');
    await userEvent.type(screen.getByLabelText('许可证'), 'KEPT1.abc.def');
    await userEvent.click(screen.getByRole('button', { name: '导入' }));
    const put = await vi.waitUntil(() => calls.find((r) => r.method === 'PUT'));
    expect(await put.clone().json()).toEqual({ license: 'KEPT1.abc.def' });
    expect(await screen.findByText('许可证长期有效')).toBeInTheDocument();
    expect(screen.getByText(/持有人：某公司/)).toBeInTheDocument();
    expect(screen.getByText('3（不限）')).toBeInTheDocument();
  });

  it('许可证不对时显示服务端的说明', async () => {
    stubFetch({
      'GET /api/v1/edition': () => jsonResponse(200, free),
      'PUT /api/v1/admin/license': () => jsonResponse(400, { code: 'license_invalid', message: '许可证校验没有通过：内容被改过，或不是收好签发的' }),
    });
    renderWithProviders(<EditionPanel />);
    await userEvent.type(await screen.findByLabelText('许可证'), 'KEPT1.x.y');
    await userEvent.click(screen.getByRole('button', { name: '导入' }));
    expect(await screen.findByText('许可证校验没有通过：内容被改过，或不是收好签发的')).toBeInTheDocument();
  });
});

describe('EditionNotice', () => {
  it('当前版本不包含时说明；包含时不显示', async () => {
    stubFetch({
      'GET /api/v1/edition': () => jsonResponse(200, free),
      'GET /api/v1/me': () => jsonResponse(200, { user: { id: 1, username: 'a', isAdmin: false }, workspace: { id: 1, name: '家', role: 'owner' } }),
    });
    renderWithProviders(<EditionNotice feature="workflows" />);
    expect(await screen.findByTestId('edition-notice')).toHaveTextContent('这是收费版的功能');
    expect(hasFeature(trial, 'ai')).toBe(true);
    expect(hasFeature(free, 'ai')).toBe(false);
    expect(hasFeature(undefined, 'ai')).toBe(true); // 没取到时不误拦
  });
});
