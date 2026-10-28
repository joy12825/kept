import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap, type ApiToken, type CreatedApiToken } from './client';

export const tokensKey = ['tokens'] as const;

export function useTokens() {
  return useQuery<ApiToken[], ApiError>({
    queryKey: tokensKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/me/tokens')),
  });
}

export function useCreateToken() {
  const qc = useQueryClient();
  return useMutation<CreatedApiToken, ApiError, string>({
    mutationFn: async (name) => unwrap(await api.POST('/api/v1/me/tokens', { body: { name } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: tokensKey }),
  });
}

export function useRevokeToken() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/me/tokens/{id}', { params: { path: { id } } })); },
    onSuccess: () => qc.invalidateQueries({ queryKey: tokensKey }),
  });
}
