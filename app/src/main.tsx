import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { queryClient } from './api/query-client';
import { startLanguage } from './i18n';

// 先定界面语言（v1.10.0），再加载界面：模块顶层的文字（状态名等）在加载时就按语言取好
void startLanguage().then(async () => {
  const { default: App } = await import('./app');
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </React.StrictMode>,
  );
});
