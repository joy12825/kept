import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { Signatures, signaturesSummary } from './signatures';

afterEach(() => { vi.unstubAllGlobals(); });

const valid = {
  type: 'Seal', status: 'valid', sealName: '上海某某餐饮有限公司发票专用章', signer: '王小明', org: '上海某某餐饮有限公司', issuer: '某某 CA',
  time: '2026-10-19T10:00:00Z', validFrom: '2020-01-01T00:00:00Z', validTo: '2040-01-01T00:00:00Z', page: 1, version: 4,
  outsideValidity: false, selfSigned: false, certInSeal: true, hasSeal: true,
  files: [{ path: 'Doc_0/Document.xml', ok: true, missing: false }, { path: 'Doc_0/Pages/Page_0/Content.xml', ok: true, missing: false }], unsigned: [],
};
const changed = { ...valid, status: 'changed', reason: '签名以后，被签的文件有改动', hasSeal: false,
  files: [{ path: 'Doc_0/Document.xml', ok: true, missing: false }, { path: 'Doc_0/Pages/Page_0/Content.xml', ok: false, missing: false }] };

describe('电子签章（v1.11.3）', () => {
  it('列出签章：印章、签章人、时间、结论；详细里说明改过的文件', async () => {
    stubFetch({ 'GET /api/v1/documents/7/signatures': () => jsonResponse(200, { items: [valid, changed] }) });
    renderWithProviders(<Signatures docId={7} />);
    expect(await screen.findAllByText('上海某某餐饮有限公司发票专用章')).toHaveLength(2);
    expect(screen.getByText('有效')).toBeInTheDocument();
    expect(screen.getByText('被改过')).toBeInTheDocument();
    expect(screen.getByText('签名以后，被签的文件有改动')).toBeInTheDocument();
    expect(screen.getAllByText('王小明 · 上海某某餐饮有限公司')).toHaveLength(2);
    expect(screen.getByRole('img', { name: '上海某某餐饮有限公司发票专用章' })).toHaveAttribute('src', '/api/v1/documents/7/signatures/1/seal');
    expect(screen.getByText(/证书是哪家机构签发的，这里没有核验/)).toBeInTheDocument();
    await userEvent.click(screen.getAllByRole('button', { name: '详细' })[1]);
    expect(screen.getByText(/改过或缺了：Doc_0\/Pages\/Page_0\/Content.xml/)).toBeInTheDocument();
  });

  it('没有签章时照实说', async () => {
    stubFetch({ 'GET /api/v1/documents/7/signatures': () => jsonResponse(200, { items: [] }) });
    renderWithProviders(<Signatures docId={7} />);
    expect(await screen.findByText('这份 OFD 没有电子签章')).toBeInTheDocument();
  });

  it('标题旁的结论：有改过的就说改过', () => {
    expect(signaturesSummary([])).toBeNull();
    expect(signaturesSummary([valid as never])?.text).toBe('有效');
    expect(signaturesSummary([valid as never, changed as never])?.text).toBe('被改过');
    expect(signaturesSummary([valid as never, { ...valid, status: 'unknown' } as never])?.text).toBe('无法验证');
  });
});
