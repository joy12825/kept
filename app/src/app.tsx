import { lazy, Suspense, useMemo } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import { CssBaseline, ThemeProvider } from '@mui/material';
import { buildTheme, localized } from './theme';
import { StoreProvider, useApp } from './store/app-store';
import Layout from './components/layout';
import HomePage from './pages/home';
import { FilesPage } from './pages/lists';
import DrawerPage from './pages/drawer/drawer-page';
import SharedDrawerPage from './pages/shared/shared-drawer-page';
import SharedDocumentPage from './pages/shared/shared-document-page';
import { collectionSource } from './pages/shared/sources';
import InboxPage from './pages/inbox/inbox-page';
import DetailPage from './pages/detail/detail-page';
import YearbookPage from './pages/yearbook-page';
import TrashPage from './pages/trash/trash-page';
import CleanupPage from './pages/cleanup-page';
import EntryPage from './pages/directory/entry-page';
import DirectoryPage from './pages/directory/directory-page';
import SettingsPage from './pages/settings';
import PublicSharePage from './pages/public-share-page';
import PublicBundlePage from './pages/public-bundle-page';
import DuplicatesPage from './pages/duplicates-page';
import TasksPage, { OldTasksRedirect } from './pages/tasks-page';
import { HealthPage, ScenePage, VisitPage, HealthReportPage, ScenesPage, LabReviewPage, PublicUploadPage, AskPage } from '@ee';
import ComparePage from './pages/detail/compare-page';
import { AsnPage } from './pages/asn-page';
import ChecklistsPage from './pages/checklists/checklists-page';
import ChecklistPage from './pages/checklists/checklist-page';
import HighlightsPage from './pages/highlights/highlights-page';
import BookListsPage from './pages/booklists/book-lists-page';
import BookListPage from './pages/booklists/book-list-page';
import { AuthGate } from './auth/auth-gate';
import { useDocumentTitle } from './api/branding';
import { LoadingState } from './components/common';

// 阅读器带着 epub.js，按需加载
const ReaderPage = lazy(() => import('./pages/reader/reader-page'));

function Themed() {
  const { s } = useApp();
  useDocumentTitle();
  const theme = useMemo(() => localized(buildTheme(s.mode)), [s.mode]);
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <BrowserRouter>
        <Routes>
          {/* 分享链接不用登录 */}
          <Route path="/s/:token" element={<PublicSharePage />} />
          <Route path="/b/:token" element={<PublicBundlePage />} />
          <Route path="/u/:token" element={<PublicUploadPage />} />
          <Route path="*" element={<AuthGate><AppRoutes /></AuthGate>} />
        </Routes>
      </BrowserRouter>
    </ThemeProvider>
  );
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="read/:id" element={<Suspense fallback={<LoadingState />}><ReaderPage /></Suspense>} />
      <Route element={<Layout />}>
        <Route index element={<HomePage />} />
        <Route path="inbox" element={<InboxPage />} />
        <Route path="files" element={<FilesPage />} />
        <Route path="review" element={<YearbookPage />} />
        <Route path="drawers/:id" element={<DrawerPage />} />
        <Route path="shared/:id" element={<SharedDrawerPage />} />
        <Route path="shared/:id/docs/:docId" element={<SharedDocumentPage />} />
        <Route path="shared-collections/:id" element={<SharedDrawerPage source={collectionSource} />} />
        <Route path="shared-collections/:id/docs/:docId" element={<SharedDocumentPage source={collectionSource} />} />
        <Route path="docs/:id" element={<DetailPage />} />
        <Route path="docs/:id/lab" element={<LabReviewPage />} />
        <Route path="docs/:id/compare" element={<ComparePage />} />
        <Route path="health" element={<HealthPage />} />
        <Route path="health/report" element={<HealthReportPage />} />
        <Route path="scenes" element={<ScenesPage />} />
        <Route path="scenes/:id" element={<ScenePage />} />
        <Route path="visits/:id" element={<VisitPage />} />
        <Route path="asn/:n" element={<AsnPage />} />
        <Route path="directory" element={<DirectoryPage />} />
        <Route path="directory/:kind/:id" element={<EntryPage />} />
        <Route path="trash" element={<TrashPage />} />
        <Route path="cleanup" element={<CleanupPage />} />
        <Route path="duplicates" element={<DuplicatesPage />} />
        <Route path="tasks" element={<TasksPage />} />
        <Route path="highlights" element={<HighlightsPage />} />
        <Route path="book-lists" element={<BookListsPage />} />
        <Route path="book-lists/:id" element={<BookListPage />} />
        <Route path="ask" element={<AskPage />} />
        <Route path="checklists" element={<ChecklistsPage />} />
        <Route path="checklists/:id" element={<ChecklistPage />} />
        <Route path="ask/:id" element={<AskPage />} />
        <Route path="settings" element={<Navigate to="/settings/pair" replace />} />
        <Route path="settings/tasks" element={<OldTasksRedirect />} />
        <Route path="settings/:tab" element={<SettingsPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}

export default function App() {
  return <StoreProvider><Themed /></StoreProvider>;
}
