import { afterEach, describe, expect, it, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { Route, Routes } from 'react-router-dom';
import { jsonResponse, renderWithProviders, stubFetch } from '../../test/utils';
import { setCsrfToken } from '../../api/client';
import SharedDrawerPage from './shared-drawer-page';


afterEach(() => { vi.unstubAllGlobals(); setCsrfToken(null); });

describe('只读共享', () => {
  it('共享来的抽屉：来源空间、文档列表与名称', async () => {
    stubFetch({
      'GET /api/v1/shared-drawers/1': () => jsonResponse(200, { id: 1, name: '房子', icon: 'home', color: 'blue', description: '', documentCount: 2, sourceWorkspaceId: 2, sourceWorkspaceName: '王家' }),
      'GET /api/v1/shared-drawers/1/documents': () => jsonResponse(200, { total: 1, items: [
        { id: 9, title: '2024年10月电费账单', createdDate: '2024-10-05', modifiedAt: '2024-10-05T00:00:00Z', mimeType: 'application/pdf', pageCount: 1, hasArchive: false, hasThumbnail: false, tagNames: ['水电'], typeName: '账单', correspondentName: '国家电网', amount: '236.40' },
      ] }),
    });
    renderWithProviders(<Routes><Route path="/shared/:id" element={<SharedDrawerPage />} /></Routes>, { route: '/shared/1' });
    expect(await screen.findByText('2 份 · 来自空间“王家”')).toBeInTheDocument();
    expect(await screen.findByText('2024年10月电费账单')).toBeInTheDocument();
    expect(screen.getByText('国家电网')).toBeInTheDocument();
    expect(screen.getByText('¥236.40')).toBeInTheDocument();
  });

});
