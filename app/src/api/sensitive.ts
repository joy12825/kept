// 敏感文档（v1.6.4）：确认密码或两步验证码后，15 分钟内可以打开预览、原件和识别文字。
import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { useMe } from './session';

export function useVerifySession() {
  const qc = useQueryClient();
  return useMutation<{ verifiedUntil: string }, ApiError, { password?: string; code?: string }>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/auth/verify', { body })),
    // 内容、文件地址都变了：全部重新取
    onSuccess: () => qc.invalidateQueries(),
  });
}

/** 现在能不能打开敏感文档；到期时自动变回 false。 */
export function useUnlocked(): boolean {
  const until = useMe().data?.verifiedUntil;
  const left = until ? new Date(until).getTime() - Date.now() : 0;
  const [, tick] = useState(0);
  useEffect(() => {
    if (left <= 0) return;
    const t = window.setTimeout(() => tick((x) => x + 1), Math.min(left + 500, 2 ** 31 - 1));
    return () => window.clearTimeout(t);
  }, [left]);
  return left > 0;
}
