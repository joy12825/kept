// 指标（v1.8.2）：化验单识别出的检查结果、核对、健康页的总览和趋势。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import { documentKey } from './documents';
import type { components } from './schema';
import { t } from '../i18n';

export type Metric = components['schemas']['Metric'];
export type Lab = components['schemas']['Lab'];
export type LabRow = components['schemas']['LabRow'];
export type LabInput = components['schemas']['LabInput'];
export type HealthPoint = components['schemas']['HealthPoint'];
export type HealthOverview = components['schemas']['HealthOverview'];
export type PersonHealth = HealthOverview['persons'][number];
export type MetricSummary = PersonHealth['metrics'][number];
export type Visit = components['schemas']['Visit'];

export const FLAG_LABEL: Record<string, string> = { high: t('偏高'), low: t('偏低'), abnormal: t('异常') };
export const SPECIMEN_LABEL: Record<string, string> = { blood: t('血液'), urine: t('尿液'), stool: t('粪便'), other: t('其他') };

const HEALTH = ['health'] as const;
export const labKey = (id: number) => ['health', 'lab', id] as const;

/** 一个场景的总览；scene 省略时是“体检与化验”（v1.9.0）。 */
export function useHealthOverview(enabled = true, scene?: number) {
  return useQuery<HealthOverview, ApiError>({
    queryKey: [...HEALTH, 'overview', scene ?? 0],
    queryFn: async () => unwrap(await api.GET('/api/v1/health', { params: { query: scene ? { scene } : {} } })),
    enabled,
  });
}

export function useMetrics(enabled = true) {
  return useQuery<Metric[], ApiError>({ queryKey: [...HEALTH, 'metrics'], queryFn: async () => unwrap(await api.GET('/api/v1/metrics')), enabled, staleTime: 60_000 });
}

export function useEnableHealthTemplate() {
  const qc = useQueryClient();
  return useMutation<{ added: number }, ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/health/template')),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: HEALTH }); void qc.invalidateQueries({ queryKey: ['taxonomy'] }); void qc.invalidateQueries({ queryKey: ['drawers'] }); },
  });
}

/** 一份文档的检查结果；还没识别过时为 null。识别中每 2 秒看一次。enabled 为 false 时不请求（详情页只在 hasLab 时取，v1.9.11）。 */
export function useDocumentLab(id: number | undefined, enabled = true) {
  return useQuery<Lab | null, ApiError>({
    queryKey: labKey(id ?? 0),
    enabled: !!id && enabled,
    queryFn: async () => {
      const r = await api.GET('/api/v1/documents/{id}/lab', { params: { path: { id: id! } } });
      if (r.response.status === 404) return null;
      return unwrap(r);
    },
    refetchInterval: (q) => (q.state.data?.status === 'running' ? 2000 : false),
  });
}

export function useExtractLab() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; replace?: boolean }>({
    mutationFn: async ({ id, replace }) => { unwrap(await api.POST('/api/v1/documents/{id}/lab/extract', { params: { path: { id } }, body: replace ? { replace } : {} })); },
    // 识别后文档有了 hasLab，详情页才会去取结果
    onSuccess: (_, { id }) => { void qc.invalidateQueries({ queryKey: labKey(id) }); void qc.invalidateQueries({ queryKey: documentKey(id) }); },
  });
}

export function useSaveLab() {
  const qc = useQueryClient();
  return useMutation<Lab, ApiError, { id: number; body: LabInput }>({
    mutationFn: async ({ id, body }) => unwrap(await api.PUT('/api/v1/documents/{id}/lab', { params: { path: { id } }, body })),
    onSuccess: (lab, { id }) => {
      qc.setQueryData(labKey(id), lab);
      void qc.invalidateQueries({ queryKey: HEALTH });
      void qc.invalidateQueries({ queryKey: ['documents'] });
    },
  });
}

export function useHealthSeries(personId: number, metricId: number | undefined) {
  return useQuery<components['schemas']['HealthSeries'], ApiError>({
    queryKey: [...HEALTH, 'series', personId, metricId ?? 0],
    enabled: !!metricId,
    queryFn: async () => unwrap(await api.GET('/api/v1/health/series', { params: { query: { personId, metricId: metricId! } } })),
  });
}

export function useVisit(id: number | undefined) {
  return useQuery<Visit, ApiError>({
    queryKey: [...HEALTH, 'visit', id ?? 0],
    enabled: !!id,
    queryFn: async () => unwrap(await api.GET('/api/v1/visits/{id}', { params: { path: { id: id! } } })),
  });
}

/** 显示一个值：数值保留原样的小数位，定性结果和大小（5×4）用原文。 */
export function showValue(p: { value?: number; text: string }, unit?: string) {
  if (p.text && !/^[<>≤≥]?\s*-?\d/.test(p.text)) return p.text;
  if (/[×xX*]/.test(p.text)) return `${p.text}${unit ? ` ${unit}` : ''}`; // 大小：5×4 mm
  if (p.value === undefined) return p.text || '—';
  const v = Math.round(p.value * 100) / 100;
  return `${p.text.match(/^[<>≤≥]/)?.[0] ?? ''}${v}${unit ? ` ${unit}` : ''}`;
}

/** 参考范围的文字。 */
export function rangeText(lo?: number, hi?: number) {
  const r = (v: number) => Math.round(v * 100) / 100;
  if (lo !== undefined && hi !== undefined) return `${r(lo)}–${r(hi)}`;
  if (hi !== undefined) return `<${r(hi)}`;
  if (lo !== undefined) return `>${r(lo)}`;
  return '';
}

// ---- v1.8.3：提醒、给医生看、就诊 ----

export type HealthAlert = components['schemas']['HealthAlert'];
export type HealthSettings = components['schemas']['HealthSettings'];
export type HealthReport = components['schemas']['HealthReport'];
export type HealthReportShare = components['schemas']['HealthReportShare'];

export const ALERT_KIND_LABEL: Record<string, string> = { range: t('超出范围'), trend: t('连续变差'), change: t('变化较大'), recheck: t('该复查了'), service: t('保养') };

export function useHealthAlerts(enabled = true) {
  return useQuery<HealthAlert[], ApiError>({ queryKey: [...HEALTH, 'alerts'], queryFn: async () => unwrap(await api.GET('/api/v1/health/alerts')), enabled });
}

export function useSetHealthAlert() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: number; dismissed: boolean }>({
    mutationFn: async ({ id, dismissed }) => {
      const path = { params: { path: { id } } };
      unwrap(dismissed ? await api.POST('/api/v1/health/alerts/{id}/dismiss', path) : await api.POST('/api/v1/health/alerts/{id}/restore', path));
    },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: [...HEALTH, 'alerts'] }); },
  });
}

export function useHealthSettings(enabled = true) {
  return useQuery<HealthSettings, ApiError>({ queryKey: [...HEALTH, 'settings'], queryFn: async () => unwrap(await api.GET('/api/v1/health/settings')), enabled });
}

export function useSetHealthSettings() {
  const qc = useQueryClient();
  return useMutation<HealthSettings, ApiError, HealthSettings>({
    mutationFn: async (body) => unwrap(await api.PUT('/api/v1/health/settings', { body })),
    onSuccess: (s) => { qc.setQueryData([...HEALTH, 'settings'], s); void qc.invalidateQueries({ queryKey: [...HEALTH, 'alerts'] }); },
  });
}

export type ReportQuery = { personId: number; metricIds?: number[]; from?: string; to?: string };

export function useHealthReport(q: ReportQuery | null) {
  return useQuery<HealthReport, ApiError>({
    queryKey: [...HEALTH, 'report', q],
    enabled: !!q,
    queryFn: async () => unwrap(await api.GET('/api/v1/health/report', { params: { query: { personId: q!.personId, metricIds: q!.metricIds?.length ? q!.metricIds : undefined, from: q!.from || undefined, to: q!.to || undefined } } })),
  });
}

export function useShareHealthReport() {
  const qc = useQueryClient();
  return useMutation<components['schemas']['ActiveShare'], ApiError, HealthReportShare>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/health/report/share', { body })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: ['shares'] }); },
  });
}

export function useSetVisitDocuments() {
  const qc = useQueryClient();
  return useMutation<Visit, ApiError, { id: number; add?: number[]; remove?: number[] }>({
    mutationFn: async ({ id, add, remove }) => unwrap(await api.PUT('/api/v1/visits/{id}/documents', { params: { path: { id } }, body: { add, remove } })),
    onSuccess: (v) => { qc.setQueryData([...HEALTH, 'visit', v.id], v); void qc.invalidateQueries({ queryKey: ['documents'] }); },
  });
}

/** 金额相加（两位小数的字符串）。 */
export function sumAmounts(xs: (string | undefined)[]) {
  const fen = xs.reduce((s, a) => s + (a ? Math.round(Number(a) * 100) : 0), 0);
  return (fen / 100).toFixed(2);
}

// ---- v1.9.0：场景、记一笔、用药 ----

export type Scene = components['schemas']['Scene'];
export type SceneList = components['schemas']['SceneList'];
export type SceneTemplate = SceneList['templates'][number];
export type SceneInput = components['schemas']['SceneInput'];
export type Medication = components['schemas']['Medication'];
export type Med = components['schemas']['Med'];
export type ObservationInput = components['schemas']['ObservationInput'];
export type MedicationInput = components['schemas']['MedicationInput'];

const SCENES = [...HEALTH, 'scenes'] as const;

export function useScenes(enabled = true) {
  return useQuery<SceneList, ApiError>({ queryKey: SCENES, queryFn: async () => unwrap(await api.GET('/api/v1/scenes')), enabled, staleTime: 30_000 });
}

function useRefreshScenes() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: HEALTH });
    void qc.invalidateQueries({ queryKey: ['taxonomy'] });
    void qc.invalidateQueries({ queryKey: ['drawers'] });
  };
}

export function useEnableScene() {
  const refresh = useRefreshScenes();
  return useMutation<{ added: number; sceneId: number }, ApiError, SceneTemplate['key']>({
    mutationFn: async (key) => unwrap(await api.POST('/api/v1/scenes/templates/{key}', { params: { path: { key: key as 'health' } } })),
    onSuccess: refresh,
  });
}

export function useSaveScene() {
  const refresh = useRefreshScenes();
  return useMutation<Scene, ApiError, { id?: number; body: SceneInput }>({
    mutationFn: async ({ id, body }) => unwrap(id
      ? await api.PATCH('/api/v1/scenes/{id}', { params: { path: { id } }, body })
      : await api.POST('/api/v1/scenes', { body })),
    onSuccess: refresh,
  });
}

export function useDeleteScene() {
  const refresh = useRefreshScenes();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/scenes/{id}', { params: { path: { id } } })); },
    onSuccess: refresh,
  });
}

export function useAddEntry() {
  const qc = useQueryClient();
  return useMutation<HealthPoint, ApiError, ObservationInput>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/observations', { body })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: HEALTH }); },
  });
}

export function useDeleteEntry() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/observations/{id}', { params: { path: { id } } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: HEALTH }); },
  });
}

const medsKey = (personId: number) => [...HEALTH, 'medications', personId] as const;

export function useMedications(personId: number, enabled = true) {
  return useQuery<Medication[], ApiError>({
    queryKey: medsKey(personId), enabled,
    queryFn: async () => unwrap(await api.GET('/api/v1/medications', { params: { query: { personId } } })),
  });
}

export function useAddMedication() {
  const qc = useQueryClient();
  return useMutation<Medication, ApiError, MedicationInput>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/medications', { body })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: [...HEALTH, 'medications'] }); },
  });
}

export function useUpdateMedication() {
  const qc = useQueryClient();
  return useMutation<Medication, ApiError, { id: number; status?: 'active' | 'stopped'; note?: string }>({
    mutationFn: async ({ id, ...body }) => unwrap(await api.PATCH('/api/v1/medications/{id}', { params: { path: { id } }, body })),
    onSuccess: () => { void qc.invalidateQueries({ queryKey: [...HEALTH, 'medications'] }); },
  });
}

export function useDeleteMedication() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, number>({
    mutationFn: async (id) => { unwrap(await api.DELETE('/api/v1/medications/{id}', { params: { path: { id } } })); },
    onSuccess: () => { void qc.invalidateQueries({ queryKey: [...HEALTH, 'medications'] }); },
  });
}

/** 用法写成一句话：20mg，每晚一次，28 天。 */
export function usage(m: Pick<Med, 'dose' | 'frequency' | 'days'>) {
  return [m.dose, m.frequency, m.days ? t('{n} 天', { n: m.days }) : ''].filter(Boolean).join(t('，'));
}
