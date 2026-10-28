import { useEffect, useState, type ReactNode } from 'react';
import { Box } from '@mui/material';
import { useQueryClient } from '@tanstack/react-query';
import { ErrorState, LoadingState } from '../components/common';
import { setUnauthorizedHandler, setWorkspaceGoneHandler } from '../api/client';
import type { Me } from '../api/client';
import { meKey, resetUserState, useMe, useSetupStatus } from '../api/session';
import { reloadForWorkspace } from '../api/workspaces';
import { LoginPage } from './login-page';

/** 按初始化状态与登录态决定显示：创建管理员、登录，或应用本体。 */
export function AuthGate({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const setup = useSetupStatus();
  const me = useMe();
  const [expired, setExpired] = useState<{ login: string } | null>(null);

  // 会话在使用中过期：任意接口返回 401 时回到登录页。只有缓存里有已登录用户时才处理：
  // 记下用户名（登录页据此回填账号并提示），再清空状态。未登录时的 401（例如登录页的
  // 密码错误）不做任何事，以免清掉健康检查等与登录无关的缓存。
  useEffect(() => {
    setUnauthorizedHandler(() => {
      const current = qc.getQueryData<Me | null>(meKey);
      if (!current) return;
      setExpired({ login: current.user.username });
      resetUserState(qc, null);
    });
  }, [qc]);

  // 保存的空间已不可用（被移出空间，或空间被删除）：客户端已清除保存的空间，这里回到个人空间重新取数据。
  useEffect(() => {
    setWorkspaceGoneHandler(() => { void reloadForWorkspace(qc); });
  }, [qc]);

  // 再次登录成功后清除过期提示。
  useEffect(() => {
    if (me.data) setExpired(null);
  }, [me.data]);

  if (setup.isPending || me.isPending) {
    return <Box sx={{ p: 6 }}><LoadingState /></Box>;
  }
  if (setup.isError || me.isError) {
    return <Box sx={{ p: 6 }}><ErrorState onRetry={() => { setup.refetch(); me.refetch(); }} /></Box>;
  }
  if (setup.data.needsSetup) return <LoginPage initialStep="setup" />;
  if (!me.data) return <LoginPage initialStep="login" expired={expired} />;
  return <>{children}</>;
}
