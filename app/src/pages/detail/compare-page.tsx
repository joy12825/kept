// 版本对比（v1.9.2）：同一份文档的两个版本，逐行看文字差异，或者左右并排看文件。
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Alert, Box, Button, ButtonBase, Checkbox, FormControlLabel, IconButton, MenuItem, Skeleton, Stack, TextField, ToggleButton, ToggleButtonGroup, Typography, useMediaQuery } from '@mui/material';
import { alpha } from '@mui/material/styles';
import { ArrowLeft, ArrowRight, ChevronsUpDown } from 'lucide-react';
import { useDocument, startDownload } from '../../api/documents';
import { useDocumentVersions, useVersionPreview, useVersionText, versionFileUrl, type DocumentVersion } from '../../api/versions';
import { EmptyState, ErrorState, LoadingState } from '../../components/common';
import { SW } from '../../components/icons';
import { PdfPreview } from '../../components/pdf-lazy';
import { CodeView } from '../../components/preview/code-view';
import { ImageViewer } from '../../components/preview/image-viewer';
import { isTextKind, previewKind } from '../../components/preview/kinds';
import { MarkdownView } from '../../components/preview/markdown-view';
import { PreviewUnavailable } from '../../components/preview/unavailable';
import { diffStats, diffText, foldSame, type Row, type Seg } from '../../utils/diff';
import { t, tc } from '../../i18n';

const mono = 'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace';
const PANE_HEIGHT = 'calc(100vh - 56px - 56px)';

const versionName = (v: DocumentVersion) => {
  const name = `${t('第 {n} 版', { n: v.number })}${v.label ? ` · ${v.label}` : ''}`;
  return v.current ? t('{name}（当前）', { name }) : name;
};

export default function ComparePage() {
  const id = Number(useParams().id);
  const nav = useNavigate();
  const [params, setParams] = useSearchParams();
  const doc = useDocument(Number.isFinite(id) ? id : undefined);
  const list = useDocumentVersions(id);
  const narrow = useMediaQuery('(max-width:767px)');
  const versions = list.data ?? [];
  // 默认：新的一边是当前版本，旧的一边是它之外最新的那一版
  const current = versions.find((v) => v.current) ?? versions[0];
  const byNumber = (n: string | null) => versions.find((v) => String(v.number) === n);
  const b = byNumber(params.get('b')) ?? current;
  const a = byNumber(params.get('a')) ?? versions.find((v) => v !== b);
  const mode = params.get('view') === 'side' ? 'side' : 'text';
  const set = (patch: Record<string, string>) => setParams((p) => { const n = new URLSearchParams(p); for (const [k, v] of Object.entries(patch)) n.set(k, v); return n; }, { replace: true });
  const back = () => nav(`/docs/${id}`);

  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement;
      if (e.key === 'Escape' && !['INPUT', 'TEXTAREA'].includes(el.tagName) && !document.querySelector('.MuiPopover-root, .MuiDialog-root, .MuiMenu-root')) back();
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  });

  if (doc.isError || list.isError) return <ErrorState onRetry={() => { void doc.refetch(); void list.refetch(); }} />;
  if (doc.isPending || list.isPending) return <LoadingState />;
  if (versions.length < 2 || !a || !b) {
    return <EmptyState title={t('只有一个版本')} text={t('上传新版本之后，就可以在这里对比两个版本。')} action={t('回到文档')} onAction={back} />;
  }
  const picker = (label: string, v: DocumentVersion, key: 'a' | 'b') => (
    <TextField select size="small" label={label} value={v.number} onChange={(e) => set({ [key]: String(e.target.value) })} sx={{ width: narrow ? '100%' : 220 }}>
      {versions.map((x) => <MenuItem key={x.number} value={x.number}>{versionName(x)}</MenuItem>)}
    </TextField>
  );

  return (
    <Box>
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center', minHeight: 56, px: 2, borderBottom: 1, borderColor: 'divider', flexWrap: 'wrap', rowGap: 1, py: narrow ? 1 : 0 }}>
        <IconButton aria-label={t('返回')} onClick={back}><ArrowLeft size={18} strokeWidth={SW} /></IconButton>
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Typography noWrap sx={{ fontWeight: 500 }}>{t('对比版本')}</Typography>
          <Typography noWrap variant="caption" sx={{ color: 'text.secondary', display: 'block' }}>{doc.data.title}</Typography>
        </Box>
        <Stack direction={narrow ? 'column' : 'row'} spacing={1} sx={{ alignItems: narrow ? 'stretch' : 'center', width: narrow ? '100%' : 'auto' }}>
          {picker(tc('对比的一侧', '旧版本'), a, 'a')}
          {!narrow && <ArrowRight size={16} strokeWidth={SW} aria-hidden />}
          {picker(t('新版本'), b, 'b')}
          <ToggleButtonGroup size="small" exclusive value={mode} onChange={(_, v) => v && set({ view: v })} aria-label={t('对比方式')}>
            <ToggleButton value="text">{t('文字差异')}</ToggleButton>
            <ToggleButton value="side">{t('并排看')}</ToggleButton>
          </ToggleButtonGroup>
        </Stack>
      </Stack>
      {a.number === b.number
        ? <Alert severity="info" sx={{ m: 3 }}>{t('两边选的是同一个版本，换一个再对比。')}</Alert>
        : mode === 'text'
          ? <TextDiff docId={id} a={a} b={b} onSide={() => set({ view: 'side' })} />
          : <SideBySide docId={id} a={a} b={b} narrow={narrow} />}
    </Box>
  );
}

function TextDiff({ docId, a, b, onSide }: { docId: number; a: DocumentVersion; b: DocumentVersion; onSide: () => void }) {
  const ta = useVersionText(docId, a.id), tb = useVersionText(docId, b.id);
  const rows = useMemo(() => (ta.data && tb.data && ta.data.source !== 'none' && tb.data.source !== 'none' ? diffText(ta.data.text, tb.data.text) : null), [ta.data, tb.data]);
  const blocks = useMemo(() => (rows ? foldSame(rows) : []), [rows]);
  const [open, setOpen] = useState<Set<number>>(new Set());
  useEffect(() => { setOpen(new Set()); }, [rows]);

  if (ta.isError || tb.isError) return <Box sx={{ p: 3 }}><Alert severity="error">{(ta.error ?? tb.error)!.message}</Alert></Box>;
  if (ta.isPending || tb.isPending) return <Stack spacing={1} sx={{ p: 3, maxWidth: 1000, mx: 'auto' }}>{[0, 1, 2, 3].map((i) => <Skeleton key={i} variant="rounded" height={20} />)}</Stack>;
  const none = [ta.data.source === 'none' ? a : null, tb.data.source === 'none' ? b : null].filter(Boolean) as DocumentVersion[];
  if (none.length || !rows) {
    return (
      <Box sx={{ p: 3, maxWidth: 1000, mx: 'auto' }}>
        <Alert severity="info" action={<Button color="inherit" size="small" onClick={onSide}>{t('并排看')}</Button>}>
          {none.length > 1
            ? t('第 {a} 版和第 {b} 版是扫描件或照片，读不出文字，只能并排看。', { a: none[0].number, b: none[1].number })
            : none.length === 1
              ? t('第 {n} 版是扫描件或照片，读不出文字，只能并排看。', { n: none[0].number })
              : t('是扫描件或照片，读不出文字，只能并排看。')}
        </Alert>
      </Box>
    );
  }
  const stats = diffStats(rows);
  return (
    <Box sx={{ p: { xs: 2, md: 3 }, maxWidth: 1000, mx: 'auto' }}>
      <Typography variant="body2" sx={{ color: 'text.secondary', mb: 1.5 }}>
        {(() => {
          const said = stats.added || stats.removed ? t('增加 {added} 行，删去 {removed} 行', { added: stats.added, removed: stats.removed }) : t('两个版本的文字一样');
          return ta.data.source !== tb.data.source ? t('{stats}。当前版本的文字含识别结果，旧版本的文字取自文件本身，空白和换行可能不同', { stats: said }) : said;
        })()}
      </Typography>
      <Box role="table" aria-label={t('文字差异')} sx={{ border: 1, borderColor: 'divider', borderRadius: 2, overflow: 'hidden', fontFamily: mono, fontSize: 13, lineHeight: '20px', bgcolor: 'background.paper' }}>
        {blocks.map((blk, i) => blk.t === 'fold' && !open.has(i)
          ? (
            <ButtonBase key={i} onClick={() => setOpen((s) => new Set(s).add(i))}
              sx={{ width: '100%', justifyContent: 'center', gap: 1, py: 0.75, fontSize: 12, color: 'text.secondary', bgcolor: 'surface.containerLow', borderTop: i ? 1 : 0, borderBottom: 1, borderColor: 'divider',
                transition: 'background-color 150ms ease-out', '&:hover': { bgcolor: 'action.hover' } }}>
              <ChevronsUpDown size={14} strokeWidth={SW} />{t('展开 {n} 行没有改动的内容', { n: blk.rows.length })}
            </ButtonBase>
          )
          : blk.rows.map((r, k) => <DiffRow key={`${i}-${k}`} r={r} />))}
      </Box>
    </Box>
  );
}

function DiffRow({ r }: { r: Row }) {
  const marker = r.t === 'add' ? '+' : r.t === 'del' ? '−' : ' ';
  const segs = r.t !== 'same' ? r.segs : undefined;
  return (
    <Box role="row" data-kind={r.t} sx={(theme) => ({
      display: 'grid', gridTemplateColumns: '48px 48px 20px minmax(0,1fr)',
      bgcolor: r.t === 'add' ? alpha(theme.palette.success.main, 0.1) : r.t === 'del' ? alpha(theme.palette.error.main, 0.08) : 'transparent',
    })}>
      <Box role="cell" sx={{ textAlign: 'right', pr: 1, color: 'text.disabled', userSelect: 'none' }}>{r.t !== 'add' ? r.a + 1 : ''}</Box>
      <Box role="cell" sx={{ textAlign: 'right', pr: 1, color: 'text.disabled', userSelect: 'none' }}>{r.t !== 'del' ? r.b + 1 : ''}</Box>
      <Box role="cell" aria-label={r.t === 'add' ? t('增加') : r.t === 'del' ? t('删去') : undefined} sx={{ textAlign: 'center', color: r.t === 'add' ? 'success.main' : 'error.main', userSelect: 'none' }}>{marker}</Box>
      <Box role="cell" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', pr: 1.5 }}>
        {segs ? segs.map((s, i) => <Mark key={i} s={s} kind={r.t} />) : r.text || '​'}
      </Box>
    </Box>
  );
}

function Mark({ s, kind }: { s: Seg; kind: Row['t'] }) {
  if (!s.changed) return <>{s.text}</>;
  return (
    <Box component={kind === 'add' ? 'ins' : 'del'} sx={(theme) => ({ textDecoration: 'none', borderRadius: '2px', bgcolor: alpha(kind === 'add' ? theme.palette.success.main : theme.palette.error.main, 0.28) })}>{s.text}</Box>
  );
}

function SideBySide({ docId, a, b, narrow }: { docId: number; a: DocumentVersion; b: DocumentVersion; narrow: boolean }) {
  const [sync, setSync] = useState(true);
  const left = useRef<HTMLDivElement>(null), right = useRef<HTMLDivElement>(null);
  // 同步滚动：按滚动的比例跟着走；对方因此滚动时不再反过来带动
  useEffect(() => {
    const l = left.current, r = right.current;
    if (!sync || !l || !r) return;
    let lock: HTMLElement | null = null;
    const follow = (from: HTMLElement, to: HTMLElement) => () => {
      if (lock === to) { lock = null; return; }
      const ratio = from.scrollTop / Math.max(1, from.scrollHeight - from.clientHeight);
      lock = from;
      to.scrollTop = ratio * Math.max(0, to.scrollHeight - to.clientHeight);
    };
    const fl = follow(l, r), fr = follow(r, l);
    l.addEventListener('scroll', fl); r.addEventListener('scroll', fr);
    return () => { l.removeEventListener('scroll', fl); r.removeEventListener('scroll', fr); };
  }, [sync, a.id, b.id]);
  return (
    <Box>
      {!narrow && (
        <Stack direction="row" sx={{ px: 2, height: 0, position: 'relative', zIndex: 3, justifyContent: 'flex-end' }}>
          <FormControlLabel control={<Checkbox size="small" checked={sync} onChange={(e) => setSync(e.target.checked)} />} label={t('同步滚动')}
            sx={{ mt: 0.5, mr: 0, bgcolor: 'background.paper', borderRadius: 1, pr: 1, border: 1, borderColor: 'divider' }} />
        </Stack>
      )}
      <Box sx={{ display: 'grid', gridTemplateColumns: narrow ? '1fr' : '1fr 1fr' }}>
        {[a, b].map((v, i) => (
          <Box key={v.number} sx={{ minWidth: 0, borderLeft: i && !narrow ? 1 : 0, borderTop: i && narrow ? 1 : 0, borderColor: 'divider' }}>
            <Typography variant="body2" sx={{ px: 2, py: 1, fontWeight: 500, borderBottom: 1, borderColor: 'divider', bgcolor: 'background.paper' }} noWrap>
              {i ? t('新版本：{name}', { name: versionName(v) }) : t('旧版本：{name}', { name: versionName(v) })}
            </Typography>
            <Box ref={i ? right : left} sx={{ height: narrow ? 'auto' : PANE_HEIGHT, minHeight: narrow ? 420 : 0, overflow: narrow ? 'visible' : 'auto', bgcolor: 'surface.containerLow' }}>
              <VersionPane docId={docId} v={v} />
            </Box>
          </Box>
        ))}
      </Box>
    </Box>
  );
}

function VersionPane({ docId, v }: { docId: number; v: DocumentVersion }) {
  const kind = previewKind({ mimeType: v.mimeType, hasArchive: false, originalFilename: v.filename });
  const text = useVersionText(docId, isTextKind(kind) ? v.id : undefined);
  const url = versionFileUrl(docId, v.id, true);
  const download = () => startDownload(versionFileUrl(docId, v.id));
  if (v.encrypted) return <PreviewUnavailable reason={t('这个版本的 PDF 有密码，网页上打不开。')} onDownload={download} />;
  if (kind === 'pdf') return <PdfPreview key={url} url={url} />;
  if (kind === 'image') return <ImageViewer key={url} url={url} alt={versionName(v)} height="calc(100vh - 56px - 56px - 37px)" onDownload={download} />;
  if (isTextKind(kind)) {
    if (!text.data) return <Stack spacing={1} sx={{ p: 3 }}>{[0, 1, 2].map((i) => <Skeleton key={i} variant="rounded" height={18} />)}</Stack>;
    if (kind === 'markdown') return <MarkdownView source={text.data.text} />;
    if (kind === 'code') return <CodeView source={text.data.text} />;
    return <Box component="pre" sx={{ m: 0, p: 3, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', fontFamily: 'inherit', fontSize: 14, lineHeight: '24px' }}>{text.data.text}</Box>;
  }
  return <ConvertedPane docId={docId} v={v} onDownload={download} />;
}

/** Office、邮件、OFD 等浏览器显示不了的版本（v1.9.7）：看服务器转出的 PDF；第一次要现转，稍等一会儿。 */
function ConvertedPane({ docId, v, onDownload }: { docId: number; v: DocumentVersion; onDownload: () => void }) {
  const pdf = useVersionPreview(docId, v.id);
  if (pdf.isError) return <PreviewUnavailable reason={pdf.error.message} onDownload={onDownload} />;
  if (!pdf.data) {
    return (
      <Stack spacing={1.5} sx={{ p: 3 }} role="status" aria-label={t('正在转成 PDF')}>
        <Typography variant="body2" sx={{ color: 'text.secondary' }}>{t('正在把这个版本转成 PDF，第一次要等一会儿…')}</Typography>
        <Skeleton variant="rounded" height={420} />
      </Stack>
    );
  }
  return <PdfPreview key={pdf.data} url={pdf.data} />;
}
