// 待整理（UI 设计规范 5.4）：逐份整理——左边队列、中间预览、右边确认面板，改一项存一项，按 A 完成整理并进入下一份；
// 批量整理——复用文档列表，选中后一起完成整理或批量修改。抽屉随阶段 7（阶段 4 计划 Q1）。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AiProposals } from '@ee';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Box, Button, InputBase, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import { Check, Inbox as InboxIcon } from 'lucide-react';
import { INBOX_FILTER, useBulk, useDocument, useInboxDocuments, useUpdateDocument, type DocumentPatch, type DocumentSummary } from '../../api/documents';
import { basisSummary, isAuto, sourceLines } from '../../api/classifier';
import { useTaxonomy } from '../../api/taxonomy';
import { useRole } from '../../api/workspaces';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import DocList, { DocThumb } from '../../components/doc-list';
import { ClassificationField, CustomFieldInput, DrawerField, DateInput, FieldRow, TagsField, type PickerHandle } from '../../components/doc-fields';
import { DocPreview, PreviewToggle, usePreviewMode } from '../../components/doc-preview';
import { SW } from '../../components/icons';
import { useTaxonomyIndex } from '../../components/pickers';
import { useApp } from '../../store/app-store';
import { t, tc } from '../../i18n';

const reduceMotion = () => typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
const INBOX_COND = JSON.parse(INBOX_FILTER) as Record<string, unknown>;
/** 这些类型的文档通常有金额：金额空着时提示“建议填写”。 */
const NEEDS_AMOUNT = ['账单', '发票']; // i18n-ignore（和用户的类型名比较）
/** 确认面板里改过（还没存完）的字段，依据里不再提。 */
const touched = (field: string, local: DocumentPatch) => {
  if (field === t('类型')) return 'documentTypeId' in local;
  if (field === t('往来方')) return 'correspondentId' in local;
  if (field === t('家人')) return 'personId' in local;
  if (field === t('日期')) return 'createdDate' in local;
  if (field === t('抽屉')) return 'drawerId' in local || 'drawerAuto' in local;
  return 'tagIds' in local; // 其余的都是“标签“…””（sourceLines 只列这几种）
};

export default function InboxPage() {
  const app = useApp();
  const role = useRole();
  const nav = useNavigate();
  const [params, setParams] = useSearchParams();
  const [mode, setMode] = useState<'one' | 'batch'>('one');
  const list = useInboxDocuments();
  const [done, setDone] = useState(0);

  // 跳过（S）的放到本页队列末尾，不写库
  const [skipped, setSkipped] = useState<number[]>([]);
  const queue = useMemo(() => {
    const items = list.data?.items ?? [];
    const rest = items.filter((d) => !skipped.includes(d.id));
    const tail = skipped.map((id) => items.find((d) => d.id === id)).filter((d): d is DocumentSummary => !!d);
    return [...rest, ...tail];
  }, [list.data, skipped]);

  // 当前这份按 ID 记：队列刷新（别人也在整理、SSE 推来变化）时尽量停在同一份
  const [currentId, setCurrentId] = useState<number | undefined>(() => Number(params.get('doc')) || undefined);
  const index = Math.max(0, queue.findIndex((d) => d.id === currentId));
  const current = queue[index];
  useEffect(() => {
    const wanted = Number(params.get('doc'));
    if (wanted && queue.some((d) => d.id === wanted)) setCurrentId(wanted);
  }, [params, queue]);
  const select = useCallback((d?: DocumentSummary) => {
    setCurrentId(d?.id);
    if (d) setParams({ doc: String(d.id) }, { replace: true });
  }, [setParams]);

  if (list.isPending) return <LoadingState />;
  if (list.isError) return <ErrorState onRetry={() => void list.refetch()} />;

  const header = (
    <Stack direction="row" spacing={1} sx={{ alignItems: 'center', height: 52, px: 3, borderBottom: 1, borderColor: 'divider' }}>
      <ToggleButtonGroup size="small" exclusive value={mode} onChange={(_, v) => v && setMode(v)} aria-label={t('整理方式')}>
        <ToggleButton value="one">{t('逐份整理')}</ToggleButton>
        <ToggleButton value="batch">{tc('方式', '批量整理')}</ToggleButton>
      </ToggleButtonGroup>
      {mode === 'one' && current && <Typography variant="body2" sx={{ color: 'text.secondary', pl: 1 }}>{t('第 {i} 份，共 {n} 份', { i: index + 1, n: queue.length })}</Typography>}
    </Stack>
  );

  if (!queue.length && mode === 'one') {
    if (done) {
      return (
        <Stack spacing={1.5} sx={{ alignItems: 'center', justifyContent: 'center', minHeight: 'calc(100vh - 64px)', px: 3 }}>
          <Box sx={{ width: 72, height: 72, borderRadius: '50%', bgcolor: 'rgba(46,107,48,0.12)', color: 'success.main', display: 'flex', alignItems: 'center', justifyContent: 'center' }}><Check size={40} strokeWidth={SW} /></Box>
          <Typography sx={{ fontSize: 22, lineHeight: '28px' }}>{t('都整理完了，本次整理了 {n} 份。', { n: done })}</Typography>
          <Button variant="contained" onClick={() => nav('/')} sx={{ mt: 1 }}>{t('回到首页')}</Button>
        </Stack>
      );
    }
    return (
      <EmptyState icon={<InboxIcon size={40} strokeWidth={SW} />} title={t('没有待整理的文档')} text={t('拖入文件或点击上传，新文档会先进入这里。')}
        action={role.canWrite ? t('上传文档') : undefined} onAction={role.canWrite ? () => app.set({ uploadOpen: true }) : undefined} />
    );
  }

  return (
    <Box>
      {header}
      {mode === 'batch'
        ? <BatchMode onCompleted={(n) => setDone((d) => d + n)} />
        : current && (
          <OneByOne key={current.id} queue={queue} index={index} current={current} readOnly={!role.canWrite}
            onSelect={select}
            onSkip={() => { setSkipped((s) => [...s.filter((x) => x !== current.id), current.id]); select(queue[index + 1] ?? queue[0]); }}
            onCompleted={() => { setDone((d) => d + 1); select(queue[index + 1] ?? queue[index - 1]); }} />
        )}
    </Box>
  );
}

interface OneProps {
  queue: DocumentSummary[]; index: number; current: DocumentSummary; readOnly: boolean;
  onSelect: (d?: DocumentSummary) => void; onSkip: () => void; onCompleted: () => void;
}

function OneByOne({ queue, index, current, readOnly, onSelect, onSkip, onCompleted }: OneProps) {
  const app = useApp();
  const ix = useTaxonomyIndex();
  const fields = useTaxonomy('custom-fields');
  const detail = useDocument(current.id);
  const update = useUpdateDocument();
  const preview = usePreviewMode(current);
  const [phase, setPhase] = useState<null | 'check' | 'slide'>(null);
  const typeRef = useRef<PickerHandle>(null);
  const corrRef = useRef<PickerHandle>(null);
  const tagsRef = useRef<PickerHandle>(null);

  // 改一项存一项；文字类的输入停顿 600ms 再存，避免每个字都请求一次
  const [local, setLocal] = useState<DocumentPatch>({});
  const pending = useRef<DocumentPatch>({});
  const timer = useRef<number>(0);
  const flush = useCallback(() => {
    window.clearTimeout(timer.current);
    const patch = pending.current;
    pending.current = {};
    if (!Object.keys(patch).length) return Promise.resolve();
    return new Promise<void>((resolve) => update.mutate({ id: current.id, patch }, {
      onSettled: () => resolve(),
      onError: (e) => { app.toast(t('没有保存：{error}', { error: e.message })); setLocal({}); },
    }));
  }, [update, current.id, app]);
  const save = (patch: DocumentPatch, delay = 0) => {
    setLocal((l) => ({ ...l, ...patch, customFields: patch.customFields ? [...(l.customFields ?? []).filter((f) => !patch.customFields!.some((p) => p.fieldId === f.fieldId)), ...patch.customFields] : l.customFields }));
    pending.current = { ...pending.current, ...patch, customFields: patch.customFields ? [...(pending.current.customFields ?? []), ...patch.customFields] : pending.current.customFields };
    window.clearTimeout(timer.current);
    if (delay) timer.current = window.setTimeout(() => void flush(), delay);
    else void flush();
  };
  useEffect(() => () => { void flush(); }, []); // eslint-disable-line react-hooks/exhaustive-deps -- 换到下一份前把没存的存掉

  const d = detail.data;
  const v = { ...current, ...d, ...local, tagIds: local.tagIds ?? d?.tagIds ?? current.tagIds };
  // 自动填入的对勾：改过的字段（本地还没存完的也算）不再打勾
  const fs = d?.fieldSources;
  const autoTag = Object.entries(fs?.tags ?? {}).find(([id, s]) => isAuto(s) && v.tagIds.includes(Number(id)))?.[1];
  const lines = sourceLines(fs, v.tagIds, (id) => ix.tags.get(id)?.name);
  const amountField = (fields.data ?? []).find((f) => f.builtIn && f.dataType === 'monetary');
  const amount = local.customFields?.find((f) => f.fieldId === amountField?.id)?.value ?? (d?.fields.find((f) => f.fieldId === amountField?.id)?.value ?? null);
  const typeName = v.documentTypeId ? ix.types.get(v.documentTypeId)?.name : undefined;
  const needAmount = !!typeName && NEEDS_AMOUNT.includes(typeName) && !amount;

  const complete = async () => {
    if (phase || readOnly) return;
    await flush();
    const r = reduceMotion();
    setPhase('check');
    window.setTimeout(() => setPhase('slide'), r ? 0 : 300);
    window.setTimeout(() => {
      update.mutate({ id: current.id, patch: { inbox: false } }, {
        onSuccess: () => {
          app.toast(t('已整理：{title}', { title: v.title }), () => { update.mutate({ id: current.id, patch: { inbox: true } }, { onError: (e) => app.toast(t('没能撤销：{error}', { error: e.message })) }); app.set({ toast: null }); });
          onCompleted();
        },
        onError: (e) => { setPhase(null); app.toast(e.message); },
      });
    }, r ? 0 : 520);
  };

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable || e.metaKey || e.ctrlKey || e.altKey) return;
      if (app.s.cmdOpen || app.s.uploadOpen || app.s.confirm || document.querySelector('.MuiPopover-root, .MuiDialog-root, .MuiMenu-root')) return;
      const k = e.key.toLowerCase();
      if (k === 'a') { e.preventDefault(); void complete(); }
      else if (k === 's') { e.preventDefault(); onSkip(); }
      else if (k === 'j') onSelect(queue[Math.min(queue.length - 1, index + 1)]);
      else if (k === 'k') onSelect(queue[Math.max(0, index - 1)]);
      else if (k === 'v') preview.cycle();
      else if (k === 't') { e.preventDefault(); tagsRef.current?.open(); }
      else if (k === 'y') { e.preventDefault(); typeRef.current?.open(); }
      else if (k === 'c') { e.preventDefault(); corrRef.current?.open(); }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  });

  const leaving = phase === 'slide';
  const pane = { opacity: leaving ? 0 : 1, transform: leaving ? 'translateX(-24px)' : 'none', transition: 'opacity 200ms ease-out, transform 200ms ease-out' };
  return (
    <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '240px minmax(0,1fr) 320px' }, height: { md: 'calc(100vh - 116px)' } }}>
      <Box component="nav" aria-label={t('待整理队列')} sx={{ display: { xs: 'none', md: 'block' }, borderRight: 1, borderColor: 'divider', overflow: 'auto', p: 1 }}>
        {queue.map((x, k) => {
          const cur = k === index;
          return (
            <Box key={x.id} component="button" aria-current={cur || undefined} onClick={() => onSelect(x)}
              sx={{ width: '100%', display: 'flex', alignItems: 'center', gap: 1.25, p: 1, border: 0, borderRadius: 3, textAlign: 'left', font: 'inherit', cursor: 'pointer',
                bgcolor: cur ? 'surface.secondaryContainer' : 'transparent', color: cur ? 'surface.onSecondaryContainer' : 'text.primary',
                transform: cur && leaving ? 'translateX(-24px)' : 'none', opacity: cur && leaving ? 0 : 1, transition: 'transform 200ms ease-out, opacity 200ms ease-out, background 150ms',
                '&:hover': { bgcolor: cur ? 'surface.secondaryContainer' : 'action.hover' } }}>
              <DocThumb d={x} w={28} h={38} />
              <Box sx={{ minWidth: 0 }}>
                <Typography noWrap sx={{ fontSize: 13, fontWeight: cur ? 500 : 400 }}>{x.title}</Typography>
                <Typography noWrap variant="caption" sx={{ color: 'text.secondary', display: 'block' }}>{x.correspondentId ? ix.correspondents.get(x.correspondentId)?.name : ' '}</Typography>
              </Box>
            </Box>
          );
        })}
      </Box>

      <Box sx={{ bgcolor: 'surface.containerLow', overflow: 'auto', minHeight: { xs: 480, md: 0 }, position: 'relative' }}>
        <Box sx={{ position: 'sticky', top: 0, zIndex: 3, display: 'flex', justifyContent: 'center', gap: 1, pt: 1.5, height: 0, overflow: 'visible' }}>
          <PreviewToggle doc={current} mode={preview.mode} onMode={preview.setMode} />
        </Box>
        <Box sx={{ pt: preview.modes.length > 1 ? 6 : 0, ...pane }}><DocPreview doc={{ ...current, conversionPending: d?.conversionPending }} mode={preview.mode} content={d?.content} loading={detail.isPending} /></Box>
      </Box>

      <Box component="aside" aria-label={t('确认信息')} sx={{ borderLeft: { md: 1 }, borderColor: 'divider', display: 'flex', flexDirection: 'column', minHeight: 0 }}>
        <Box sx={{ flex: 1, overflow: 'auto', p: 2, ...pane }}>
          <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('标题')}</Typography>
          {readOnly
            ? <Typography sx={{ fontSize: 16, fontWeight: 500, mb: 1.5 }}>{v.title}</Typography>
            : <InputBase multiline fullWidth defaultValue={v.title} inputProps={{ 'aria-label': t('标题'), maxLength: 200 }}
              onChange={(e) => { if (e.target.value.trim()) save({ title: e.target.value.trim() }, 600); }}
              sx={{ fontSize: 16, fontWeight: 500, lineHeight: '24px', mb: 1.5, px: 0.75, mx: -0.75, borderRadius: 1, border: '1px solid transparent', '&:hover': { borderColor: 'divider' }, '&.Mui-focused': { borderColor: 'primary.main' } }} />}
          <FieldRow label={t('抽屉')} top checks source={'drawerId' in local || 'drawerAuto' in local ? undefined : fs?.drawer}>
            <DrawerField value={v.drawerId} manual={!!(local.drawerId !== undefined || (!('drawerAuto' in local) && d?.drawerManual))} ruleDrawerId={d?.ruleDrawerId} ix={ix} readOnly={readOnly}
              onPick={(x) => save({ drawerId: x })} onAuto={() => save({ drawerAuto: true })} />
          </FieldRow>
          <FieldRow label={t('类型')} checks source={'documentTypeId' in local ? undefined : fs?.type}><ClassificationField ref={typeRef} kind="type" value={v.documentTypeId} ix={ix} readOnly={readOnly} onChange={(x) => save({ documentTypeId: x })} /></FieldRow>
          <FieldRow label={t('往来方')} checks source={'correspondentId' in local ? undefined : fs?.correspondent}><ClassificationField ref={corrRef} kind="correspondent" value={v.correspondentId} ix={ix} readOnly={readOnly} onChange={(x) => save({ correspondentId: x })} /></FieldRow>
          {(ix.personList.length > 0 || !!v.personId) && (
            <FieldRow label={t('家人')} checks source={'personId' in local ? undefined : fs?.person}><ClassificationField kind="person" value={v.personId} ix={ix} readOnly={readOnly} onChange={(x) => save({ personId: x })} /></FieldRow>
          )}
          <FieldRow label={t('日期')} top checks source={'createdDate' in local ? undefined : fs?.date}><DateInput label={t('日期')} value={v.createdDate} readOnly={readOnly} onChange={(x) => x && save({ createdDate: x }, 600)} /></FieldRow>
          <FieldRow label={tc('复数', '标签')} top checks source={'tagIds' in local ? undefined : autoTag}><TagsField ref={tagsRef} value={v.tagIds} ix={ix} readOnly={readOnly} onChange={(x) => save({ tagIds: x })} /></FieldRow>
          {amountField && (
            <FieldRow label={t('金额')} checks hint={needAmount && !readOnly ? <Box component="span" sx={{ color: 'warning.main' }}>{t('建议填写：{type}通常有金额', { type: typeName ?? '' })}</Box> : undefined}>
              <CustomFieldInput field={amountField} value={amount} readOnly={readOnly} selfId={current.id} onChange={(x) => save({ customFields: [{ fieldId: amountField.id, value: x ?? null }] }, 600)} />
            </FieldRow>
          )}
          {d && <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mt: 2 }}>{basisSummary(lines.filter((l) => !touched(l.field, local)))}</Typography>}
          {d && <AiProposals docId={d.id} readOnly={readOnly} />}
          <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mt: 0.5 }}>{t('修改会自动保存。')}</Typography>
        </Box>
        <Box sx={{ p: 2, borderTop: 1, borderColor: 'divider' }}>
          <Stack direction="row" spacing={1} sx={{ mb: 1 }}>
            <Button fullWidth variant="outlined" size="small" onClick={onSkip} disabled={queue.length < 2}>{t('跳过')}<Box component="span" sx={{ ml: 1, fontSize: 11, color: 'text.secondary' }}>S</Box></Button>
            <Button fullWidth variant="outlined" size="small" onClick={() => onSelect(queue[index - 1])} disabled={index === 0}>{t('上一份')}<Box component="span" sx={{ ml: 1, fontSize: 11, color: 'text.secondary' }}>K</Box></Button>
          </Stack>
          {!readOnly && (
            <Button fullWidth variant="contained" color={phase ? 'success' : 'primary'} onClick={() => void complete()} sx={{ transition: 'background-color 150ms ease-out' }}
              startIcon={phase ? <Check size={18} strokeWidth={2} /> : undefined}>
              {phase ? t('已整理') : <>{t('完成整理')}<Box component="span" sx={{ ml: 1, fontSize: 11, px: 0.6, border: '1px solid rgba(255,255,255,0.5)', borderRadius: 0.5 }}>A</Box></>}
            </Button>
          )}
        </Box>
      </Box>
    </Box>
  );
}

/** 批量整理：待整理的文档列表，选中后在批量操作栏里“完成整理”。 */
function BatchMode({ onCompleted }: { onCompleted: (n: number) => void }) {
  const app = useApp();
  const bulk = useBulk();
  const role = useRole();
  return (
    <Box sx={{ px: 3, pt: 1.5 }}>
      <DocList listKey="inbox" base={[INBOX_COND]} placeholder={t('在待整理中搜索…')} backLabel={t('待整理')} emptyTitle={t('没有待整理的文档')} emptyText={t('拖入文件或点击上传，新文档会先进入这里。')}
        batchExtra={(sel, clear) => role.canWrite && (
          <Button variant="contained" size="small" disabled={bulk.isPending} onClick={() => {
            const body = 'ids' in sel ? { ids: sel.ids } : { filter: JSON.parse(sel.filter) as Record<string, unknown> };
            bulk.mutate({ ...body, edit: { inbox: false } }, {
              onSuccess: (r) => { app.toast(t('已整理，共 {n} 份。', { n: r.count })); onCompleted(r.count); clear(); },
              onError: (e) => app.toast(e.message),
            });
          }}>{t('完成整理')}</Button>
        )} />
    </Box>
  );
}
