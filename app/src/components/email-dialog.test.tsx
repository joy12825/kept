import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { setCsrfToken, setWorkspaceId } from '../api/client';
import { EmailDialog } from './email-dialog';

afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); setWorkspaceId(null); });

const docs = [{ id: 9, title: '租房合同' }, { id: 10, title: '电费账单' }];

describe('EmailDialog', () => {
  it('填多个收件人、附言和原件后发送', async () => {
    const calls = stubFetch({ 'POST /api/v1/documents/email': () => jsonResponse(202, { jobId: 3 }) });
    const onClose = vi.fn();
    renderWithProviders(<EmailDialog docs={docs} open onClose={onClose} />);
    expect(screen.getByText('2 份：租房合同、电费账单')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: '主题' })).toHaveAttribute('placeholder', '租房合同 等 2 份文档');
    const to = screen.getByRole('combobox', { name: '收件人' });
    await userEvent.type(to, 'li@example.com,zhang@example.com');
    await userEvent.type(screen.getByRole('textbox', { name: '附言' }), '请查收');
    await userEvent.click(screen.getByRole('radio', { name: '原件' }));
    await userEvent.click(screen.getByRole('button', { name: '发送' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const body = await calls.find((r) => r.method === 'POST')!.clone().json();
    expect(body).toEqual({ ids: [9, 10], to: ['li@example.com', 'zhang@example.com'], message: '请查收', file: 'original' });
  });

  it('地址不对时不能发送，并说明哪个不对', async () => {
    stubFetch({});
    renderWithProviders(<EmailDialog docs={docs.slice(0, 1)} open onClose={() => {}} />);
    expect(screen.getByRole('button', { name: '发送' })).toBeDisabled();
    await userEvent.type(screen.getByRole('combobox', { name: '收件人' }), 'li@ ');
    expect(screen.getByText('“li@”不是有效的邮箱地址')).toBeInTheDocument();
    // 粘贴一串地址：拆成一个个收件人
    await userEvent.click(screen.getByRole('combobox', { name: '收件人' }));
    await userEvent.paste('zhang@example.com；wang@example.com, x');
    expect(screen.getByRole('button', { name: 'zhang@example.com' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'wang@example.com' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: '收件人' })).toHaveValue('x');
    expect(screen.getByRole('button', { name: '发送' })).toBeDisabled();
  });

  it('服务器拒绝时显示原因', async () => {
    stubFetch({ 'POST /api/v1/documents/email': () => jsonResponse(400, { code: 'attachments_too_large', message: '附件共 30.0 MB，超过了一封邮件允许的 20.0 MB，请减少文档' }) });
    renderWithProviders(<EmailDialog docs={docs} open onClose={() => {}} />);
    await userEvent.type(screen.getByRole('combobox', { name: '收件人' }), 'li@example.com{Enter}');
    await userEvent.click(screen.getByRole('button', { name: '发送' }));
    expect(await screen.findByText(/超过了一封邮件允许的 20.0 MB/)).toBeInTheDocument();
  });
});
