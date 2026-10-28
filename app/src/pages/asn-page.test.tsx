import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useParams } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../test/utils';
import { AsnField } from '../components/doc-fields';
import { AsnPage } from './asn-page';

afterEach(() => { vi.unstubAllGlobals(); });

function Doc() { return <p>文档 {useParams().id}</p>; }

function app(route: string) {
  renderWithProviders(
    <Routes>
      <Route path="/asn/:n" element={<AsnPage />} />
      <Route path="/docs/:id" element={<Doc />} />
      <Route path="/files" element={<p>文件列表</p>} />
    </Routes>, { route });
}

describe('按原件编号打开', () => {
  it('有这个编号时直接打开文档', async () => {
    const calls = stubFetch({ 'GET /api/v1/documents': () => jsonResponse(200, { items: [{ id: 42, title: '保修卡' }] }) });
    app('/asn/123');
    expect(await screen.findByText('文档 42')).toBeInTheDocument();
    expect(JSON.parse(new URL(calls[0].url).searchParams.get('filter')!)).toEqual({ field: 'asn', op: 'eq', value: 123 });
  });

  it('没有时说明，并可以去文件列表', async () => {
    stubFetch({ 'GET /api/v1/documents': () => jsonResponse(200, { items: [] }) });
    app('/asn/7');
    expect(await screen.findByText('没有原件编号为 #000007 的文档')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '打开文件列表' }));
    expect(screen.getByText('文件列表')).toBeInTheDocument();
  });

  it('编号不是数字时去文件列表', async () => {
    stubFetch({});
    app('/asn/abc');
    expect(await screen.findByText('文件列表')).toBeInTheDocument();
  });
});

describe('原件编号的“下一个”', () => {
  function Field() {
    const [v, setV] = useState<number | undefined>();
    return <AsnField value={v} onChange={setV} />;
  }

  it('空着时填入最大编号加 1，有编号后不再显示', async () => {
    stubFetch({ 'GET /api/v1/documents/next-asn': () => jsonResponse(200, { asn: 41 }) });
    renderWithProviders(<Field />);
    await userEvent.click(screen.getByRole('button', { name: '下一个' }));
    await waitFor(() => expect(screen.getByRole('textbox', { name: '原件编号' })).toHaveValue('41'));
    expect(screen.queryByRole('button', { name: '下一个' })).not.toBeInTheDocument();
  });

  it('只看时没有按钮', () => {
    renderWithProviders(<AsnField onChange={() => {}} readOnly />);
    expect(screen.queryByRole('button', { name: '下一个' })).not.toBeInTheDocument();
  });
});
