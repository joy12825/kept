// 自动分类（设计文档 5.6）：模型状态、重新训练、试一试规则，以及把字段来源（field_sources）说成人话。
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, unwrap } from './client';
import type { components } from './schema';
import { t } from '../i18n';

export type FieldSources = components['schemas']['FieldSources'];
export type FieldSource = components['schemas']['FieldSource'];
export type ClassifierStatus = components['schemas']['ClassifierStatus'];
export type MatchTest = components['schemas']['MatchTest'];
export type MatchTestResult = components['schemas']['MatchTestResult'];
export type ClassifyMode = components['schemas']['BulkClassify']['mode'];

/** 由自动分类填入的来源：确认面板上打对勾。上传时指定的和用户改过的不算。 */
export const isAuto = (s?: FieldSource) => !!s && ['rule', 'model', 'text', 'workflow', 'agent', 'similar'].includes(s.source);

/** 一个字段的来源和依据，如“规则：关键词“国家电网””“自动学习，把握 83%”。 */
/** 把握的百分比：学出来的判断不会百分之百确定，最多显示 99%。 */
export const percent = (p: number) => Math.min(99, Math.round(p * 100));

export function describeSource(s: FieldSource): string {
  switch (s.source) {
    case 'rule': return t('匹配规则：{basis}', { basis: s.basis ?? '' });
    case 'model': return s.confidence ? t('自动学习，把握 {p}%', { p: percent(s.confidence) }) : t('自动学习');
    case 'text': return s.basis ? t('从正文识别：“{basis}”', { basis: s.basis }) : t('从正文识别');
    case 'file': return s.basis ? t('取自{basis}', { basis: s.basis }) : t('取自文件名');
    case 'preset': return t('上传时指定');
    case 'user': return t('手动修改');
    case 'workflow': return t('工作流“{name}”', { name: s.basis ?? '' });
    case 'agent': {
      const head = s.confidence ? t('AI 识别，把握 {p}%', { p: percent(s.confidence) }) : t('AI 识别');
      return s.basis ? t('{head}：“{basis}”', { head, basis: s.basis }) : head;
    }
    case 'similar': {
      const head = s.confidence ? t('相似文档，把握 {p}%', { p: percent(s.confidence) }) : t('相似文档');
      return s.basis ? t('{head}：{basis}', { head, basis: s.basis }) : head;
    }
  }
}

export interface SourceLine { field: string; source: FieldSource }

/**
 * 文档各字段的来源，按类型、往来方、日期、标签排列。tagName 给出标签名；
 * 文档已经没有的标签（用户去掉的）不列出。
 */
export function sourceLines(fs: FieldSources | undefined, tagIds: number[], tagName: (id: number) => string | undefined): SourceLine[] {
  if (!fs) return [];
  const out: SourceLine[] = [];
  if (fs.type) out.push({ field: t('类型'), source: fs.type });
  if (fs.correspondent) out.push({ field: t('往来方'), source: fs.correspondent });
  if (fs.person) out.push({ field: t('家人'), source: fs.person });
  if (fs.date) out.push({ field: t('日期'), source: fs.date });
  if (fs.drawer) out.push({ field: t('抽屉'), source: fs.drawer });
  for (const [id, s] of Object.entries(fs.tags ?? {})) {
    if (tagIds.includes(Number(id))) out.push({ field: t('标签“{name}”', { name: tagName(Number(id)) ?? t('已删除的标签') }), source: s });
  }
  return out;
}

/** 待整理面板底部的一句话依据，只说自动填入的部分。 */
export function basisSummary(lines: SourceLine[]): string {
  const auto = lines.filter((l) => isAuto(l.source));
  if (!auto.length) return t('没有匹配的规则，自动学习也没有把握，请手动填写。');
  const drawer = auto.find((l) => l.field === t('抽屉') && l.source.source === 'rule');
  const rule = auto.filter((l) => l.source.source === 'rule' && l !== drawer).map((l) => t('{field}（{basis}）', { field: l.field, basis: l.source.basis ?? '' }));
  const model = auto.filter((l) => l.source.source === 'model');
  const text = auto.filter((l) => l.source.source === 'text');
  const similar = auto.filter((l) => l.source.source === 'similar');
  const parts: string[] = [];
  if (drawer) parts.push(t('放入抽屉按{basis}', { basis: drawer.source.basis ?? '' }));
  if (rule.length) parts.push(t('匹配规则：{basis}', { basis: rule.join(t('、')) }));
  if (model.length) {
    const low = Math.min(...model.map((l) => l.source.confidence ?? 0));
    const fields = model.map((l) => l.field).join(t('、'));
    parts.push(low ? t('{fields}由自动学习给出，把握 {p}%', { fields, p: percent(low) }) : t('{fields}由自动学习给出', { fields }));
  }
  if (similar.length) {
    const low = Math.min(...similar.map((l) => l.source.confidence ?? 0));
    const fields = similar.map((l) => l.field).join(t('、'));
    parts.push(low ? t('{fields}参照相似的已整理文档，把握 {p}%', { fields, p: percent(low) }) : t('{fields}参照相似的已整理文档', { fields }));
  }
  if (text.length) parts.push(t('{fields}从正文识别', { fields: text.map((l) => l.field).join(t('、')) }));
  return t('依据：{list}', { list: parts.join(t('；')) });
}

export function useClassifier(enabled = true) {
  return useQuery<ClassifierStatus, ApiError>({
    queryKey: ['classifier'],
    queryFn: async () => unwrap(await api.GET('/api/v1/classifier')),
    enabled,
    // 训练或重新分类进行中时每 2 秒刷新一次
    refetchInterval: (q) => {
      const d = q.state.data;
      return d && (d.training || d.applying?.status === 'queued' || d.applying?.status === 'running') ? 2000 : false;
    },
  });
}

export function useTrainClassifier() {
  const qc = useQueryClient();
  return useMutation<components['schemas']['JobAccepted'], ApiError, void>({
    mutationFn: async () => unwrap(await api.POST('/api/v1/classifier/train')),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['classifier'] }),
  });
}

export function useMatchTest() {
  return useMutation<MatchTestResult, ApiError, MatchTest>({
    mutationFn: async (body) => unwrap(await api.POST('/api/v1/classifier/test', { body })),
  });
}

export const SKIP_REASONS: Record<NonNullable<ClassifierStatus['skipped']>, string> = {
  too_few_documents: t('已整理的文档还不够多'),
  nothing_to_learn: t('“自动学习”的往来方、家人、类型和标签都还没有足够的样本'),
  no_words: t('已整理的文档里没有可用的文字'),
};
