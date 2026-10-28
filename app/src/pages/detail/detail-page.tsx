// 详情页（UI 设计规范 5.6）：左边预览，右边信息栏；点任何一个值就能修改，有修改时底部出现“放弃 / 保存”。
// 上一份 / 下一份按来源列表的顺序；返回时回到来源列表并恢复滚动位置。抽屉随阶段 7（阶段 4 计划 Q1）。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { Accordion, AccordionDetails, AccordionSummary, Alert, Box, Button, Chip, Divider, IconButton, InputBase, Menu, MenuItem, Stack, Tooltip, Typography, useMediaQuery } from '@mui/material';
import { ArrowLeft, BookOpen, ChevronDown, ChevronLeft, ChevronRight, CircleCheck, Download, Ellipsis, Lock, Menu as MenuIcon, Plus, Printer, EyeOff } from 'lucide-react';
import { LockedPanel } from '../../components/verify-dialog';
import { printDocument } from '../../utils/print';
import { fileUrl, formatAsn, isPdfDocument, startDownload, useDeleteDocument, useDocument, useDocumentList, useFileInfo, useReprocess, useRestoreDocument, useUpdateDocument, withPurpose, type DocumentDetail, type DocumentPatch } from '../../api/documents';
import { useDocumentDuplicates } from '../../api/duplicates';
import { PageEditorDialog, SplitDialog } from '../../components/pdf-lazy';
import { EmailDialog } from '../../components/email-dialog';
import { ShareLinkDialog } from '../../components/share-link-dialog';
import { WatermarkDialog, eeBuild, AiProposals, LabSection, readingKind, SimilarDocuments, DocAsk } from '@ee';
import { canWatermark } from '../../utils/watermark';
import { Signatures, signaturesSummary } from './signatures';
import { useSignatures } from '../../api/formats';
import { RemovePasswordDialog, Versions } from './versions';
import { useMe } from '../../api/session';
import { ApiError } from '../../api/client';
import { rememberDoc } from '../../api/recent';
import { useTaxonomy, type CustomField } from '../../api/taxonomy';
import { useRole } from '../../api/workspaces';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { AsnField, ClassificationField, DrawerField, DateInput, CustomFieldInput, FieldRow, TagsField, type PickerHandle } from '../../components/doc-fields';
import { canRead, DocPreview, isEpub, isTextDocument, PreviewToggle, usePreviewMode } from '../../components/doc-preview';
import { previewKind } from '../../components/preview/kinds';
import { SW } from '../../components/icons';
import { useTaxonomyIndex } from '../../components/pickers';
import { useApp } from '../../store/app-store';
import { History } from './history';
import { Notes } from './notes';
import { BookDialog, BookSection } from './book';
import { MyReading } from './my-reading';
import { FindBar, useFind } from '../../components/find-bar';
import { filterText } from '../../api/filters';
import { LinkedFrom } from './linked-from';
import { useDeleteBook } from '../../api/books';
import { Sources } from './sources';
import { ContentEditor } from './content-editor';
import { dateTime, fileSize } from './time';
import { t, tc } from '../../i18n';

const SOURCE_LABEL: Record<string, string> = { web: t('网页上传'), api: t('通过 API 上传'), mobile: t('手机扫描'), watch: t('监听目录'), mail: t('邮件收取'), link: t('上传链接'), import: t('从 Paperless 导入'), remote: t('远程收件夹') };

interface Edits {
  title?: string; createdDate?: string; documentTypeId?: number; correspondentId?: number; personId?: number; locationId?: number; tagIds?: number[]; asn?: number;
  fields?: Record<number, unknown>;
}

const SPLIT_KEY = 'kept-detail-split';
const readSplit = () => { try { return Number(localStorage.getItem(SPLIT_KEY)) || 65; } catch { return 65; } };
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

/** 当前打开的是不是浮层（选择面板、菜单、对话框）：Esc 等快捷键先交给它们。 */
const overlayOpen = () => !!document.querySelector('.MuiPopover-root, .MuiDialog-root, .MuiMenu-root');

/** 能打印：电子书不打印，敏感文档确认密码之前不能打印 */
const printable = (d: DocumentDetail) => !isEpub(d.mimeType) && !d.locked;

export default function DetailPage() {
  const { id: idParam = '' } = useParams();
  const id = Number(idParam);
  const app = useApp();
  const nav = useNavigate();
  const role = useRole();
  const ix = useTaxonomyIndex();
  const fields = useTaxonomy('custom-fields');
  const detail = useDocument(Number.isFinite(id) ? id : undefined);
  // 家人的私密文档（v1.8.0）：只读授权和帮忙上传的人只能看
  const readOnly = !role.canWrite || detail.data?.privacy?.canEdit === false;
  const update = useUpdateDocument();
  const del = useDeleteDocument();
  const restore = useRestoreDocument();
  const reprocess = useReprocess();
  const unbook = useDeleteBook(id);
  const narrow = useMediaQuery('(max-width:767px)');
  const md = useMediaQuery('(min-width:1024px)');

  const [edits, setEdits] = useState<Edits>({});
  const [added, setAdded] = useState<number[]>([]); // 用“添加字段”加进来、还没有值的自定义字段
  const [conflict, setConflict] = useState(false);
  const preview = usePreviewMode(detail.data);
  const mode = preview.mode;
  const find = useFind();
  const findState = useLocation().state as { find?: string } | null;
  // 能查找：PDF 预览和文字；EPUB 的封面、图片原件和音视频不能
  const kind = detail.data ? previewKind(detail.data) : 'none';
  const findable = !!detail.data && !(mode === 'original' && (kind === 'epub' || kind === 'image' || kind === 'audio' || kind === 'video' || kind === 'sheet' || kind === 'slides' || kind === 'zip'));
  const [split, setSplit] = useState(readSplit);
  const [dlEl, setDlEl] = useState<HTMLElement | null>(null);
  const [moreEl, setMoreEl] = useState<HTMLElement | null>(null);
  const [pageTool, setPageTool] = useState<'edit' | 'split' | 'email' | 'share' | 'decrypt' | 'book' | null>(null);
  const smtp = useMe().data?.features?.smtp ?? false;
  const watermarkOn = useMe().data?.features?.watermark ?? false;
  const [stamping, setStamping] = useState(false);
  const [addEl, setAddEl] = useState<HTMLElement | null>(null);
  const typeRef = useRef<PickerHandle>(null);
  const corrRef = useRef<PickerHandle>(null);
  const tagsRef = useRef<PickerHandle>(null);
  // 开始修改时的版本：保存时带上它，期间别人改过就返回 409
  const base = useRef<string | undefined>(undefined);
  const dirty = Object.keys(edits).length > 0;
  useEffect(() => { setEdits({}); setAdded([]); setConflict(false); base.current = undefined; }, [id]);

  // 来源列表：上一份 / 下一份按它的顺序；直接打开链接时按全部文件的默认顺序
  const docNav = app.s.docNav;
  const query = docNav?.query ?? { filter: '', sort: 'created' as const, dir: 'desc' as const };
  const source = useDocumentList(query);
  const ids = useMemo(() => {
    const loaded = source.data?.pages.flatMap((p) => p.items.map((d) => d.id));
    return loaded?.length ? loaded : docNav?.ids ?? [];
  }, [source.data, docNav]);
  const idx = ids.indexOf(id);

  // 从搜索结果（列表里搜索过，或 ⌘K）打开时，预览里自动查找搜索词
  const searched = findState?.find || (docNav?.ids.includes(id) ? filterText(docNav.query?.filter ?? '') : '');
  useEffect(() => { if (searched) find.show(searched, false); }, [id, searched]); // eslint-disable-line react-hooks/exhaustive-deps

  const guard = (go: () => void) => {
    if (!dirty) return go();
    app.confirm({ title: t('放弃未保存的修改？'), body: t('离开这份文档后，刚才的修改不会保存。'), verb: t('放弃'), run: () => { app.confirm(null); go(); } });
  };
  const back = () => guard(() => nav(docNav?.from ?? '/files'));
  const step = (d: number) => guard(async () => {
    let next = ids[idx + d];
    if (next === undefined && d > 0 && source.hasNextPage) {
      const r = await source.fetchNextPage();
      const more = r.data?.pages.flatMap((p) => p.items.map((x) => x.id)) ?? [];
      next = more[more.indexOf(id) + 1];
    }
    if (next !== undefined) nav(`/docs/${next}`, { replace: true });
  });
  const canNext = idx >= 0 && (idx < ids.length - 1 || !!source.hasNextPage);

  useEffect(() => {
    if (!dirty) return;
    const h = (e: BeforeUnloadEvent) => { e.preventDefault(); };
    window.addEventListener('beforeunload', h);
    return () => window.removeEventListener('beforeunload', h);
  }, [dirty]);

  const print = (x: DocumentDetail) => printDocument(x).catch((e: Error) => app.toast(e.message));

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (app.s.cmdOpen || app.s.uploadOpen || app.s.confirm || overlayOpen()) return;
      const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable;
      if (e.key === 'Escape' && !typing) { back(); return; }
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'f' && findable && el.tagName !== 'TEXTAREA') {
        e.preventDefault(); find.show(); return;
      }
      // ⌘P / Ctrl+P（v1.9.7）：打印这份文档，而不是整个网页
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'p' && detail.data) {
        e.preventDefault();
        if (printable(detail.data)) print(detail.data);
        return;
      }
      if (typing || e.metaKey || e.ctrlKey || e.altKey) return;
      const k = e.key.toLowerCase();
      if (k === 'j') void step(1);
      else if (k === 'k') void step(-1);
      else if (k === 'v') preview.cycle();
      else if (k === 't') { e.preventDefault(); tagsRef.current?.open(); }
      else if (k === 'y') { e.preventDefault(); typeRef.current?.open(); }
      else if (k === 'c') { e.preventDefault(); corrRef.current?.open(); }
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  });

  // 记进“最近打开”（⌘K 没有输入时显示）
  useEffect(() => { if (detail.data) rememberDoc({ id: detail.data.id, title: detail.data.title }); }, [detail.data?.id, detail.data?.title]); // eslint-disable-line react-hooks/exhaustive-deps

  const d = detail.data;
  // 地址里的 ID 不是数字（手抄错的链接）
  if (!Number.isInteger(id) || id <= 0) return <EmptyState title={t('找不到这份文档')} text={t('链接里的文档编号不对。')} action={t('返回')} onAction={() => nav(-1)} />;
  if (detail.isPending) return <LoadingState />;
  if (detail.isError) {
    return detail.error.status === 404
      ? <EmptyState title={t('找不到这份文档')} text={t('它可能已被删除，可以到回收站看看。')} action={t('返回')} onAction={() => nav(docNav?.from ?? '/files')} />
      : <ErrorState onRetry={() => void detail.refetch()} />;
  }
  if (!d) return null;
  const place = d.locationId ? ix.locations.get(d.locationId) : undefined; // 原件在哪里（v1.6.3）

  const set = (p: Edits) => {
    if (!dirty) base.current = d.modifiedAt;
    setEdits((e) => ({ ...e, ...p }));
  };
  const setField = (fieldId: number, value: unknown) => set({ fields: { ...edits.fields, [fieldId]: value } });
  const v = { ...d, ...edits, tagIds: edits.tagIds ?? d.tagIds };
  const fieldValue = (fid: number) => (edits.fields && fid in edits.fields ? edits.fields[fid] : d.fields.find((f) => f.fieldId === fid)?.value ?? null);
  const titleInvalid = !v.title.trim();
  const dateInvalid = !DATE_RE.test(v.createdDate);

  const save = () => {
    const patch: DocumentPatch = { modifiedAt: base.current ?? d.modifiedAt };
    if (edits.title !== undefined) patch.title = edits.title.trim();
    if (edits.createdDate !== undefined) patch.createdDate = edits.createdDate;
    if (edits.documentTypeId !== undefined) patch.documentTypeId = edits.documentTypeId;
    if (edits.correspondentId !== undefined) patch.correspondentId = edits.correspondentId;
    if (edits.personId !== undefined) patch.personId = edits.personId;
    if (edits.locationId !== undefined) patch.locationId = edits.locationId;
    if (edits.tagIds !== undefined) patch.tagIds = edits.tagIds;
    if (edits.asn !== undefined) patch.asn = edits.asn;
    if (edits.fields) patch.customFields = Object.entries(edits.fields).map(([fid, value]) => ({ fieldId: Number(fid), value: value ?? null }));
    update.mutate({ id: d.id, patch }, {
      onSuccess: () => { setEdits({}); setAdded([]); base.current = undefined; app.toast(t('已保存')); },
      onError: (e) => {
        if (e instanceof ApiError && e.code === 'modified') setConflict(true);
        else if (e instanceof ApiError && e.code === 'became_private') { setEdits({}); app.toast(e.message); nav(docNav?.from ?? '/files'); }
        else app.toast(e.message);
      },
    });
  };
  const discard = () => { setEdits({}); setAdded([]); setConflict(false); base.current = undefined; };
  const reload = () => { discard(); void detail.refetch(); };

  const complete = () => update.mutate({ id: d.id, patch: { inbox: false } }, {
    onSuccess: () => app.toast(t('已整理：{title}', { title: d.title }), () => { update.mutate({ id: d.id, patch: { inbox: true } }); app.set({ toast: null }); }),
    onError: (e) => app.toast(e.message),
  });
  const remove = () => app.confirm({
    title: t('删除这份文档？'), body: t('文档会移到回收站，30 天后自动清除。在此之前可以从回收站恢复。'), verb: t('删除'),
    run: () => {
      app.confirm(null);
      del.mutate(d.id, {
        onSuccess: () => { app.toast(t('已删除：{title}', { title: d.title }), () => { restore.mutate(d.id); app.set({ toast: null }); }); nav(docNav?.from ?? '/files'); },
        onError: (e) => app.toast(e.message),
      });
    },
  });

  // 自定义字段：内置的金额、到期日一直显示；其他的有值、或刚添加的才显示
  const allFields = fields.data ?? [];
  const shownFields = allFields.filter((f) => f.builtIn || fieldValue(f.id) !== null || added.includes(f.id) || (edits.fields && f.id in edits.fields));
  // 内置字段在前：金额、到期日
  const rank = (f: CustomField) => (f.builtIn ? (f.dataType === 'monetary' ? 0 : 1) : 2);
  const builtInFirst = [...shownFields].sort((a, b) => rank(a) - rank(b));
  const addable = allFields.filter((f) => !shownFields.includes(f));
  const startDrag = (e: React.MouseEvent) => {
    e.preventDefault();
    const grid = (e.currentTarget.parentElement as HTMLElement).getBoundingClientRect();
    const move = (ev: MouseEvent) => setSplit(Math.min(80, Math.max(40, ((ev.clientX - grid.left) / grid.width) * 100)));
    const up = () => {
      window.removeEventListener('mousemove', move);
      window.removeEventListener('mouseup', up);
      setSplit((s) => { try { localStorage.setItem(SPLIT_KEY, String(Math.round(s))); } catch { /* 隐私模式 */ } return s; });
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
  };

  return (
    <Box>
      <Stack direction="row" spacing={narrow ? 0 : 1} sx={{ alignItems: 'center', position: 'sticky', top: 0, zIndex: 1000, height: 56, px: narrow ? 0.5 : 2, borderBottom: 1, borderColor: 'divider', bgcolor: 'background.default' }}>
        {!md && !narrow && <IconButton aria-label={t('打开菜单')} onClick={() => app.set({ mobileNav: true })}><MenuIcon size={20} strokeWidth={SW} /></IconButton>}
        {narrow ? (
          <>
            <IconButton aria-label={t('返回{label}', { label: docNav?.label ?? t('全部文件') })} onClick={back}><ArrowLeft size={20} strokeWidth={SW} /></IconButton>
            <Typography noWrap variant="h6" component="h1" sx={{ flex: 1, minWidth: 0 }}>{v.title}</Typography>
            <IconButton aria-label={t('上一份')} disabled={idx <= 0} onClick={() => void step(-1)}><ChevronLeft size={20} strokeWidth={SW} /></IconButton>
            <IconButton aria-label={t('下一份')} disabled={!canNext} onClick={() => void step(1)}><ChevronRight size={20} strokeWidth={SW} /></IconButton>
            {canRead(d) && !d.locked && <IconButton aria-label={t('阅读')} onClick={() => guard(() => nav(`/read/${d.id}`, { state: { from: `/docs/${d.id}` } }))}><BookOpen size={20} strokeWidth={SW} /></IconButton>}
            <IconButton aria-label={t('下载')} disabled={!!d.locked} onClick={(e) => setDlEl(e.currentTarget)}><Download size={20} strokeWidth={SW} /></IconButton>
            {printable(d) && (
              <Tooltip title={t('打印')}><IconButton aria-label={t('打印')} onClick={() => print(d)}><Printer size={20} strokeWidth={SW} /></IconButton></Tooltip>
            )}
          </>
        ) : (
          <>
            <Button startIcon={<ArrowLeft size={18} strokeWidth={SW} />} onClick={back} sx={{ color: 'text.secondary', flex: 'none' }}>{docNav?.label ?? t('全部文件')}</Button>
            <Typography noWrap variant="h6" component="h1" sx={{ flex: 1, minWidth: 0 }}>{v.title}</Typography>
            <Button size="small" disabled={idx <= 0} startIcon={<ChevronLeft size={16} strokeWidth={SW} />} onClick={() => void step(-1)}>{t('上一份')}</Button>
            <Button size="small" disabled={!canNext} endIcon={<ChevronRight size={16} strokeWidth={SW} />} onClick={() => void step(1)}>{t('下一份')}</Button>
            {canRead(d) && !d.locked && <Button variant="outlined" size="small" startIcon={<BookOpen size={16} strokeWidth={SW} />} onClick={() => guard(() => nav(`/read/${d.id}`, { state: { from: `/docs/${d.id}` } }))}>{t('阅读')}</Button>}
            <Button variant="outlined" size="small" disabled={!!d.locked} startIcon={<Download size={16} strokeWidth={SW} />} endIcon={<ChevronDown size={16} strokeWidth={SW} />} onClick={(e) => setDlEl(e.currentTarget)}>{t('下载')}</Button>
            {printable(d) && <Tooltip title={t('打印（⌘P）')}><IconButton aria-label={t('打印')} onClick={() => print(d)}><Printer size={20} strokeWidth={SW} /></IconButton></Tooltip>}
          </>
        )}
        <Tooltip title={t('更多')}><IconButton aria-label={t('更多')} onClick={(e) => setMoreEl(e.currentTarget)}><Ellipsis size={20} strokeWidth={SW} /></IconButton></Tooltip>
        <Menu anchorEl={dlEl} open={!!dlEl} onClose={() => setDlEl(null)}>
          <MenuItem sx={{ minWidth: 160 }} onClick={() => { setDlEl(null); startDownload(withPurpose(fileUrl(d.id, 'original'), 'download')); }}>{t('原件')}</MenuItem>
          <MenuItem disabled={!d.hasArchive} onClick={() => { setDlEl(null); startDownload(withPurpose(fileUrl(d.id, 'preview'), 'download')); }}>{d.hasArchive ? t('可搜索版') : t('可搜索版（没有）')}</MenuItem>
          {eeBuild && watermarkOn && canWatermark(d) && !d.locked && (
            <MenuItem onClick={() => { setDlEl(null); setStamping(true); }}>{t('加水印打印或下载…')}</MenuItem>
          )}
        </Menu>
        <Menu anchorEl={moreEl} open={!!moreEl} onClose={() => setMoreEl(null)}>
          <MenuItem sx={{ minWidth: 160 }} disabled={readOnly} onClick={() => {
            setMoreEl(null);
            const go = () => reprocess.mutate(d.id, { onSuccess: () => app.toast(t('已加入处理队列，完成后会更新识别文字')), onError: (e) => app.toast(e.message) });
            if (!d.contentEditedAt) { go(); return; }
            app.confirm({
              title: t('重新处理会覆盖修改过的识别文字'), body: t('这份文档的识别文字有人工修改。重新处理后会换成新识别的文字，之前的修改不会保留。'), verb: t('重新处理'),
              run: () => { app.confirm(null); go(); },
            });
          }}>{t('重新处理')}</MenuItem>
          <MenuItem disabled={readOnly || !!d.sensitive} title={d.sensitive ? t('敏感文档不能分享') : undefined} onClick={() => { setMoreEl(null); setPageTool('share'); }}>{t('分享链接')}</MenuItem>
          <MenuItem disabled={readOnly} onClick={() => {
            setMoreEl(null);
            if (dirty) { app.toast(t('请先保存或放弃修改，再移到其他空间')); return; }
            app.set({ moveDocs: [{ id: d.id, title: d.title }] });
          }}>{t('移到其他空间…')}</MenuItem>
          {d.originalEncrypted && <MenuItem disabled={readOnly} onClick={() => { setMoreEl(null); setPageTool('decrypt'); }}>{t('移除密码')}</MenuItem>}
          {smtp && <MenuItem disabled={readOnly || !!d.sensitive} onClick={() => { setMoreEl(null); setPageTool('email'); }}>{t('通过邮件发送')}</MenuItem>}
          {isPdfDocument(d) && <MenuItem disabled={readOnly} onClick={() => { setMoreEl(null); setPageTool('edit'); }}>{t('编辑页面')}</MenuItem>}
          {isPdfDocument(d) && d.pageCount > 1 && <MenuItem disabled={readOnly} onClick={() => { setMoreEl(null); setPageTool('split'); }}>{t('拆分')}</MenuItem>}
          <MenuItem disabled={readOnly} onClick={() => {
            setMoreEl(null);
            const on = !d.isMilestone;
            update.mutate({ id: d.id, patch: { isMilestone: on } }, { onSuccess: () => app.toast(on ? t('已标记为大事，会出现在年度回顾的大事记里') : t('已取消大事标记')), onError: (e) => app.toast(e.message) });
          }}>{d.isMilestone ? t('取消大事标记') : t('标记为大事')}</MenuItem>
          <MenuItem disabled={readOnly} onClick={() => {
            setMoreEl(null);
            const on = !d.sensitiveMarked;
            update.mutate({ id: d.id, patch: { sensitive: on } }, {
              onSuccess: () => app.toast(on ? t('已标为敏感文档：打开前要确认密码，不能分享，也不交给 AI') : t('已取消敏感标记')),
              onError: (e) => app.toast(e.message),
            });
          }}>{d.sensitiveMarked ? t('取消敏感标记') : t('标为敏感文档')}</MenuItem>
          {d.privacy?.canSetPrivate && (
            <MenuItem disabled={dirty || (!d.private && !d.personId)} title={!d.private && !d.personId ? t('先选择这份文档属于哪位家人') : undefined} onClick={() => {
              setMoreEl(null);
              const on = !d.private;
              const who = d.personId ? ix.persons.get(d.personId)?.name : '';
              update.mutate({ id: d.id, patch: { private: on } }, {
                onSuccess: () => app.toast(on ? t('已设为{who}的私密文档：其他家人和空间管理员都看不到', { who: who ?? '' }) : t('已取消私密，空间里的成员都能看到')),
                onError: (e) => app.toast(e.message),
              });
            }}>{d.private ? t('取消私密') : t('设为家人的私密文档')}</MenuItem>
          )}
          <MenuItem disabled={readOnly} onClick={() => {
            setMoreEl(null);
            if (!d.isBook) { setPageTool('book'); return; }
            app.confirm({
              title: t('不再作为书籍？'), body: t('书目信息会删除，文档本身保留；它会离开书房。'), verb: t('确定'),
              run: () => { app.confirm(null); unbook.mutate(undefined, { onSuccess: () => app.toast(t('已不再作为书籍')), onError: (e) => app.toast(e.message) }); },
            });
          }}>{d.isBook ? t('不再作为书籍') : t('设为书籍')}</MenuItem>
          <MenuItem disabled={readOnly} sx={{ color: 'error.main' }} onClick={() => { setMoreEl(null); remove(); }}>{t('删除')}</MenuItem>
        </Menu>
        <PageEditorDialog d={d} open={pageTool === 'edit'} onClose={() => setPageTool(null)} />
        <SplitDialog d={d} open={pageTool === 'split'} onClose={() => setPageTool(null)} />
        {pageTool === 'share' && <ShareLinkDialog doc={d} open onClose={() => setPageTool(null)} />}
        {stamping && <WatermarkDialog doc={d} onClose={() => setStamping(false)} />}
        <BookDialog docId={d.id} docTitle={d.title} open={pageTool === 'book'} onClose={() => setPageTool(null)} />
        <RemovePasswordDialog docId={d.id} open={pageTool === 'decrypt'} onClose={() => setPageTool(null)} />
        {smtp && <EmailDialog docs={[{ id: d.id, title: d.title }]} open={pageTool === 'email'} onClose={() => setPageTool(null)} />}
      </Stack>

      <Box sx={{ display: 'grid', gridTemplateColumns: narrow ? '1fr' : `minmax(0,${split}fr) 6px minmax(320px,${100 - split}fr)`, height: narrow ? 'auto' : 'calc(100vh - 56px)' }}>
        {/* 窄屏时整页滚动：这里不设 overflow，预览底部的缩放浮条才能贴着屏幕底部 */}
        <Box sx={{ bgcolor: 'surface.containerLow', overflow: narrow ? 'visible' : 'auto', overflowX: narrow ? 'clip' : undefined, minHeight: narrow ? 480 : 0, position: 'relative' }}>
          <Box sx={{ position: 'sticky', top: 0, zIndex: 3, display: 'flex', justifyContent: 'center', pt: 1.5, height: 0, overflow: 'visible' }}>
            {!d.locked && <PreviewToggle doc={d} mode={mode} onMode={preview.setMode} />}
          </Box>
          {findable && !d.locked && (
            // 查找按钮在右上角；打开的查找框放在“原件 / 识别文字”下面，预览区窄时也不会叠在一起
            <Box sx={{ position: 'sticky', top: 0, zIndex: 4, display: 'flex', justifyContent: 'flex-end', pt: find.open ? 7 : 1.5, pr: 2, height: 0, overflow: 'visible' }}>
              <Box><FindBar find={find} /></Box>
            </Box>
          )}
          {d.locked ? <LockedPanel /> : <Box sx={{ pt: preview.modes.length > 1 ? 6 : 0 }}>
            {mode === 'text' && !isTextDocument(d.mimeType) && !isEpub(d.mimeType)
              ? <ContentEditor d={d} readOnly={readOnly} blocked={dirty} find={find.state} />
              : <DocPreview doc={d} mode={mode} content={d.content} find={find.state} shortcuts canImport={!readOnly} />}
          </Box>}
        </Box>
        {!narrow && (
          <Box role="separator" aria-orientation="vertical" aria-label={t('调整预览区宽度')} onMouseDown={startDrag}
            sx={{ cursor: 'col-resize', bgcolor: 'divider', width: '1px', mx: 'auto', '&:hover': { bgcolor: 'primary.main', width: '3px' } }} />
        )}
        <Box component="aside" aria-label={t('文档信息')} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0 }}>
          <Box sx={{ flex: 1, overflow: 'auto', p: 2.5 }}>
            <DuplicateHint docId={d.id} />
            {readOnly
              ? <Typography sx={{ fontSize: 18, lineHeight: '26px', fontWeight: 500, overflowWrap: 'anywhere' }}>{v.title}</Typography>
              : <InputBase multiline fullWidth value={v.title} onChange={(e) => set({ title: e.target.value })} inputProps={{ 'aria-label': t('标题'), maxLength: 200 }}
                sx={{ fontSize: 18, lineHeight: '26px', fontWeight: 500, px: 0.75, mx: -0.75, borderRadius: 1, border: '1px solid', borderColor: titleInvalid ? 'error.main' : 'transparent', '&:hover': { borderColor: titleInvalid ? 'error.main' : 'divider' }, '&.Mui-focused': { borderColor: 'primary.main' } }} />}
            <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap', rowGap: 0.5, mt: 0.5, color: 'text.secondary', fontSize: 13, '& > *': { whiteSpace: 'nowrap' } }}>
              {d.asn ? <Box component="span" sx={{ fontFamily: 'monospace', fontSize: 12 }}>{formatAsn(d.asn)}</Box> : null}
              {d.asn ? <span>·</span> : null}
              {place ? <Box component="span" title={place.note || undefined}>{t('原件在：{name}', { name: place.name })}</Box> : null}
              {place ? <span>·</span> : null}
              {d.inbox
                ? <><Chip size="small" label={t('待整理')} />{!readOnly && <Button size="small" onClick={complete} disabled={update.isPending}>{t('完成整理')}</Button>}</>
                : <Box component="span" sx={{ display: 'flex', alignItems: 'center', gap: 0.5, color: 'success.main' }}><CircleCheck size={16} strokeWidth={SW} />{t('已整理')}</Box>}
              {d.private && (
                <Tooltip title={t('{who}的私密文档：只有本人和本人允许的人看得到，空间管理员也看不到', { who: d.personId ? ix.persons.get(d.personId)?.name ?? t('家人') : t('家人') })}>
                  <Chip size="small" icon={<EyeOff size={14} strokeWidth={SW} />} label={t('私密')} />
                </Tooltip>
              )}
              {d.sensitive && (
                <Tooltip title={d.sensitiveMarked ? t('打开前要确认密码，不能分享，也不交给 AI') : t('所在抽屉是敏感抽屉：打开前要确认密码，不能分享，也不交给 AI')}>
                  <Chip size="small" icon={<Lock size={14} strokeWidth={SW} />} label={t('敏感')} />
                </Tooltip>
              )}
            </Stack>

            {d.privacy?.asUploader && (
              <Alert severity="info" sx={{ mt: 1.5 }}>
                {t('这是{who}的私密文档。你上传了它，所以能看到；对方可以收回你的查看权限。', { who: d.personId ? ix.persons.get(d.personId)?.name ?? t('家人') : t('家人') })}
              </Alert>
            )}
            <Stack sx={{ mt: 2 }}>
              <FieldRow label={t('抽屉')} top>
                <DrawerField value={d.drawerId} manual={d.drawerManual} ruleDrawerId={d.ruleDrawerId} ix={ix} readOnly={readOnly}
                  onPick={(x) => dirty ? app.toast(t('请先保存或放弃其他修改，再换抽屉')) : update.mutate({ id: d.id, patch: { drawerId: x } }, { onSuccess: () => app.toast(x ? t('已放入“{name}”', { name: ix.drawers.get(x)?.name ?? '' }) : t('已设为未入抽屉')), onError: (e) => app.toast(e.message) })}
                  onAuto={() => dirty ? app.toast(t('请先保存或放弃其他修改，再换抽屉')) : update.mutate({ id: d.id, patch: { drawerAuto: true } }, { onSuccess: () => app.toast(t('已交还给收纳规则')), onError: (e) => app.toast(e.message) })} />
              </FieldRow>
              <FieldRow label={t('类型')}><ClassificationField ref={typeRef} kind="type" value={v.documentTypeId} ix={ix} readOnly={readOnly} onChange={(x) => set({ documentTypeId: x })} onBrowse={(x) => guard(() => nav(`/directory/document-types/${x}`))} /></FieldRow>
              <FieldRow label={t('往来方')}><ClassificationField ref={corrRef} kind="correspondent" value={v.correspondentId} ix={ix} readOnly={readOnly} onChange={(x) => set({ correspondentId: x })} onBrowse={(x) => guard(() => nav(`/directory/correspondents/${x}`))} /></FieldRow>
              <FieldRow label={t('家人')}><ClassificationField kind="person" value={v.personId} ix={ix} readOnly={readOnly} onChange={(x) => set({ personId: x })} onBrowse={(x) => guard(() => nav(`/directory/persons/${x}`))} /></FieldRow>
              <FieldRow label={t('日期')} top>
                <Box sx={{ minWidth: 0 }}>
                  <DateInput key={d.id} label={t('日期')} value={v.createdDate} readOnly={readOnly} onChange={(x) => x && set({ createdDate: x })} />
                  <OtherDates d={d} current={v.createdDate} onPick={readOnly ? undefined : (x) => set({ createdDate: x })} />
                </Box>
              </FieldRow>
              <FieldRow label={tc('复数', '标签')} top><TagsField ref={tagsRef} value={v.tagIds} ix={ix} readOnly={readOnly} onChange={(x) => set({ tagIds: x })} onBrowse={(x) => guard(() => nav(`/directory/tags/${x}`))} /></FieldRow>
              <FieldRow label={t('原件编号')}><AsnField value={v.asn} readOnly={readOnly} onChange={(x) => set({ asn: x })} /></FieldRow>
              <FieldRow label={t('存放位置')}><ClassificationField kind="location" value={v.locationId} ix={ix} readOnly={readOnly} onChange={(x) => set({ locationId: x })} /></FieldRow>
            </Stack>
            <Divider sx={{ my: 2 }} />
            <Stack>
              {builtInFirst.map((f) => (
                <FieldRow key={f.id} label={<span title={f.name}>{f.name}</span>} top={f.dataType === 'document_link' || !!f.extra.multiline}>
                  <CustomFieldInput field={f} value={fieldValue(f.id)} readOnly={readOnly} selfId={d.id} onChange={(x) => setField(f.id, x)} />
                </FieldRow>
              ))}
              {!readOnly && addable.length > 0 && (
                <>
                  <Button size="small" startIcon={<Plus size={16} strokeWidth={SW} />} onClick={(e) => setAddEl(e.currentTarget)} sx={{ alignSelf: 'flex-start', mt: 0.5 }}>{t('添加字段')}</Button>
                  <Menu anchorEl={addEl} open={!!addEl} onClose={() => setAddEl(null)}>
                    {addable.map((f: CustomField) => <MenuItem key={f.id} sx={{ minWidth: 160 }} onClick={() => { setAddEl(null); setAdded((a) => [...a, f.id]); }}>{f.name}</MenuItem>)}
                  </Menu>
                </>
              )}
            </Stack>
            <Divider sx={{ my: 2 }} />
            {d.book && <><BookSection docId={d.id} docTitle={d.title} book={d.book} readOnly={readOnly} /><Divider sx={{ my: 2 }} /><MyReading docId={d.id} readable={canRead(d)} /><Divider sx={{ my: 2 }} /></>}
            <LabSection id={d.id} canEdit={!readOnly} hasLab={!!d.hasLab} reading={readingKind(d.title, d.content, d.documentTypeId ? ix.types.get(d.documentTypeId)?.name : undefined)} />
            <Notes docId={d.id} />
            <DocAsk docId={d.id} />
            <AiProposals docId={d.id} readOnly={readOnly} />
            <Box sx={{ mt: 2 }}><LinkedFrom items={d.linkedFrom} /><SimilarDocuments docId={d.id} /></Box>
            {d.mimeType === 'application/ofd' && !d.locked && <SignaturesSection docId={d.id} />}
            <Accordion disableGutters>
              <AccordionSummary expandIcon={<ChevronDown size={18} strokeWidth={SW} />} sx={{ px: 0, minHeight: 44 }}><Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('分类依据')}</Typography></AccordionSummary>
              <AccordionDetails sx={{ px: 0, pt: 0 }}><Sources d={d} ix={ix} /></AccordionDetails>
            </Accordion>
            <Accordion disableGutters>
              <AccordionSummary expandIcon={<ChevronDown size={18} strokeWidth={SW} />} sx={{ px: 0, minHeight: 44 }}><Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('修改历史')}</Typography></AccordionSummary>
              <AccordionDetails sx={{ px: 0, pt: 0 }}><History docId={d.id} /></AccordionDetails>
            </Accordion>
            <Accordion disableGutters>
              <AccordionSummary expandIcon={<ChevronDown size={18} strokeWidth={SW} />} sx={{ px: 0, minHeight: 44 }}><Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('版本')}</Typography></AccordionSummary>
              <AccordionDetails sx={{ px: 0, pt: 0 }}><Versions docId={d.id} readOnly={readOnly} /></AccordionDetails>
            </Accordion>
            <Accordion disableGutters slotProps={{ transition: { unmountOnExit: true } }}>
              <AccordionSummary expandIcon={<ChevronDown size={18} strokeWidth={SW} />} sx={{ px: 0, minHeight: 44 }}><Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('文件信息')}</Typography></AccordionSummary>
              <AccordionDetails sx={{ px: 0, pt: 0 }}>
                <FileInfo d={d} date={v.createdDate} onUseDate={readOnly ? undefined : (x) => set({ createdDate: x })} />
              </AccordionDetails>
            </Accordion>
          </Box>
          {(dirty || conflict) && (
            <Box sx={{ borderTop: 1, borderColor: 'divider', px: 2, py: 1.5 }}>
              {conflict && (
                <Alert severity="warning" sx={{ mb: 1 }} action={<Button size="small" onClick={reload}>{t('刷新')}</Button>}>{t('这份文档刚被其他人修改过。刷新后会丢掉你的修改。')}</Alert>
              )}
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                <Typography variant="body2" sx={{ color: 'text.secondary', flex: 1 }}>{t('有未保存的修改')}</Typography>
                <Button onClick={discard}>{t('放弃')}</Button>
                <Button variant="contained" disabled={update.isPending || titleInvalid || dateInvalid || conflict} onClick={save}>{t('保存')}</Button>
              </Stack>
            </Box>
          )}
        </Box>
      </Box>
    </Box>
  );
}

// 文件信息（v1.3.7 补全）：基本信息随文档返回；文件自带的属性（PDF 属性、照片 EXIF）展开时才读取
/** 电子签章（v1.11.3）：有签章时默认展开，标题旁说明结论 */
function SignaturesSection({ docId }: { docId: number }) {
  const list = useSignatures(docId);
  const sum = signaturesSummary(list.data);
  return (
    <Accordion disableGutters defaultExpanded>
      <AccordionSummary expandIcon={<ChevronDown size={18} strokeWidth={SW} />} sx={{ px: 0, minHeight: 44 }}>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('电子签章')}</Typography>
          {sum && <Chip size="small" color={sum.color} variant="outlined" label={sum.text} />}
        </Stack>
      </AccordionSummary>
      <AccordionDetails sx={{ px: 0, pt: 0 }}><Signatures docId={docId} /></AccordionDetails>
    </Accordion>
  );
}

function FileInfo({ d, date, onUseDate }: { d: DocumentDetail; date: string; onUseDate?: (date: string) => void }) {
  const info = useFileInfo(d, true);
  const x = info.data;
  const rows: [string, string, string?][] = [
    [t('原件'), d.originalFilename], [t('格式'), x?.mimeType ?? d.mimeType], [t('大小'), fileSize(d.originalSize)], [t('页数'), d.pageCount ? String(d.pageCount) : '—'],
    [t('可搜索版'), d.hasArchive ? t('已生成（{size}）', { size: fileSize(d.archiveSize) }) : isTextDocument(d.mimeType) ? t('文本文件，不需要') : isEpub(d.mimeType) ? t('电子书，不需要') : t('原件已有文字层')],
    [t('校验值'), t('原件 SHA-256 {sum}…', { sum: d.checksum.slice(0, 16) }), d.checksum],
    ...(x?.archiveChecksum ? [['', t('可搜索版 {sum}…', { sum: x.archiveChecksum.slice(0, 16) }), x.archiveChecksum] as [string, string, string]] : []),
    [t('添加时间'), `${dateTime(d.addedAt)} · ${SOURCE_LABEL[d.source] ?? t('网页上传')}`], [t('修改时间'), dateTime(d.modifiedAt)],
  ];
  const row = (k: string, val: string, title?: string) => (
    <Stack key={k + val} direction="row" spacing={1.25} sx={{ fontSize: 12, lineHeight: '22px' }}>
      <Box sx={{ width: 56, color: 'text.secondary', flex: 'none' }}>{k}</Box>
      <Box sx={{ overflowWrap: 'anywhere', minWidth: 0 }} title={title}>{val}</Box>
    </Stack>
  );
  return (
    <Box>
      {rows.map(([k, val, title]) => row(k, val, title))}
      {x && x.props.length > 0 && (
        <Box sx={{ mt: 1.5 }}>
          <Typography variant="caption" sx={{ color: 'text.secondary', display: 'block', mb: 0.25 }}>{t('文件自带的信息')}</Typography>
          {x.props.map((p) => row(p.label, p.value))}
        </Box>
      )}
      {x?.takenOn && x.takenOn !== date && onUseDate && (
        <Button size="small" sx={{ mt: 0.5, ml: -0.75 }} onClick={() => onUseDate(x.takenOn!)}>{t('用拍摄日期 {date} 作为文档日期', { date: x.takenOn })}</Button>
      )}
      {info.isPending && <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('正在读取文件自带的信息…')}</Typography>}
    </Box>
  );
}

/** 疑似重复的提示（v1.3.7）：这份文档在“重复文档”里还有没处理的配对时显示。 */
function DuplicateHint({ docId }: { docId: number }) {
  const nav = useNavigate();
  const pairs = useDocumentDuplicates(docId).data?.pairs ?? [];
  if (!pairs.length) return null;
  const other = (p: (typeof pairs)[number]) => (p.a.id === docId ? p.b : p.a);
  const top = pairs[0];
  return (
    <Alert severity="info" sx={{ mb: 2 }} action={<Button size="small" color="inherit" onClick={() => nav('/duplicates')}>{t('去处理')}</Button>}>
      {pairs.length > 1
        ? t('可能和《{title}》重复（相似度 {pct}%），另有 {n} 份', { title: other(top).title, pct: top.similarity, n: pairs.length - 1 })
        : t('可能和《{title}》重复（相似度 {pct}%）', { title: other(top).title, pct: top.similarity })}
    </Alert>
  );
}

/** 识别到的其他日期（v1.4.4）：证件的签发日期、账单日……点一下换用，保存后生效。 */
function OtherDates({ d, current, onPick }: { d: DocumentDetail; current: string; onPick?: (date: string) => void }) {
  const alts = (d.fieldSources?.date?.alternatives ?? []).filter((a) => a.date !== current);
  if (!alts.length || !onPick) return null;
  return (
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', alignItems: 'center', mt: 0.5 }}>
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>{t('其他日期')}</Typography>
      {alts.map((a) => (
        <Tooltip key={a.date} title={a.basis}>
          <Chip size="small" variant="outlined" label={a.date} onClick={() => onPick(a.date)} aria-label={t('改用 {date}（{basis}）', { date: a.date, basis: a.basis })} />
        </Tooltip>
      ))}
    </Stack>
  );
}
