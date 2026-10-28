import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type EditionStatus = components['schemas']['EditionStatus'];
export type EditionName = EditionStatus['edition'];
export type Feature = EditionStatus['features'][number];

export const editionKey = ['edition'] as const;

/** 当前版本（v1.12）：试用或许可证、到期、用量与上限、包含的功能。 */
export function useEdition() {
  return useQuery<EditionStatus, ApiError>({
    queryKey: editionKey,
    queryFn: async () => unwrap(await api.GET('/api/v1/edition')),
    staleTime: 5 * 60_000,
  });
}

/** 每个收费功能属于哪个版本（和服务端 edition.featureEdition 一致）。 */
export const FEATURE_EDITION: Record<Feature, Exclude<EditionName, 'free'>> = {
  ai: 'pro', privacy: 'pro', scenes: 'pro', storage: 'pro', mail: 'pro', uploadLinks: 'pro', workflows: 'pro', sharing: 'pro',
  sso: 'team', quota: 'team', branding: 'team', postgres: 'team',
};

/** 当前版本是否包含这个功能；版本信息没取到时当作包含，不误拦。 */
export function hasFeature(ed: EditionStatus | undefined, f: Feature): boolean {
  return !ed || ed.features.includes(f);
}

export function editionLabel(e: EditionName): string {
  return e === 'team' ? t('企业版') : e === 'pro' ? t('收费版') : t('免费版');
}

export function useInstallLicense() {
  const qc = useQueryClient();
  return useMutation<EditionStatus, ApiError, string>({
    mutationFn: async (license) => unwrap(await api.PUT('/api/v1/admin/license', { body: { license } })),
    // 版本变了，功能开关、AI 设置等都要重新取
    onSuccess: (st) => { qc.setQueryData(editionKey, st); void qc.invalidateQueries(); },
  });
}

export function useRemoveLicense() {
  const qc = useQueryClient();
  return useMutation<EditionStatus, ApiError, void>({
    mutationFn: async () => unwrap(await api.DELETE('/api/v1/admin/license')),
    onSuccess: (st) => { qc.setQueryData(editionKey, st); void qc.invalidateQueries(); },
  });
}
