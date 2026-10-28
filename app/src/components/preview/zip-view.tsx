// 压缩包（v1.9.3）：包里的文件按目录排成树，可以展开收起；勾选后“导入选中的文件”，每个成为一份新文档进待整理。
import { useMemo, useState } from 'react';
import { Alert, Box, Button, ButtonBase, Checkbox, Chip, Skeleton, Stack, Typography } from '@mui/material';
import { ChevronDown, ChevronRight, FileText, Folder, FolderOpen } from 'lucide-react';
import { useImportZipEntries, useZipEntries, type ImportResult, type ZipEntry } from '../../api/formats';
import { useApp } from '../../store/app-store';
import { SW } from '../icons';
import { PreviewUnavailable } from './unavailable';
import { t } from '../../i18n';

const MAX_IMPORT = 200;

interface Node { name: string; path: string; dir: boolean; size: number; encrypted: boolean; kids: Node[] }

/** 由路径排成树：目录在前，同级按名称 */
export function buildTree(entries: ZipEntry[]): Node[] {
  const root: Node = { name: '', path: '', dir: true, size: 0, encrypted: false, kids: [] };
  const dirs = new Map<string, Node>([['', root]]);
  const dirOf = (p: string): Node => {
    const hit = dirs.get(p);
    if (hit) return hit;
    const i = p.lastIndexOf('/');
    const parent = dirOf(i < 0 ? '' : p.slice(0, i));
    const n: Node = { name: p.slice(i + 1), path: p, dir: true, size: 0, encrypted: false, kids: [] };
    parent.kids.push(n);
    dirs.set(p, n);
    return n;
  };
  for (const e of entries) {
    if (e.dir) { dirOf(e.path); continue; }
    const i = e.path.lastIndexOf('/');
    dirOf(i < 0 ? '' : e.path.slice(0, i)).kids.push({ name: e.path.slice(i + 1), path: e.path, dir: false, size: e.size, encrypted: e.encrypted, kids: [] });
  }
  const sort = (n: Node) => {
    n.kids.sort((a, b) => (a.dir === b.dir ? a.name.localeCompare(b.name, 'zh-CN') : a.dir ? -1 : 1));
    n.kids.forEach(sort);
    if (n.dir) n.size = n.kids.reduce((s, k) => s + k.size, 0);
  };
  sort(root);
  return root.kids;
}

const dirsUnder = (n: Node): string[] => (n.dir ? [n.path, ...n.kids.flatMap(dirsUnder)] : []);
const filesUnder = (n: Node): Node[] => (n.dir ? n.kids.flatMap(filesUnder) : n.encrypted ? [] : [n]);

function size(n: number) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10240 ? 1 : 0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function ZipView({ docId, canImport, onDownload }: { docId: number; canImport: boolean; onDownload?: () => void }) {
  const app = useApp();
  const q = useZipEntries(docId);
  const imp = useImportZipEntries(docId);
  const tree = useMemo(() => buildTree(q.data?.entries ?? []), [q.data]);
  const total = (q.data?.entries ?? []).filter((e) => !e.dir).length;
  // 文件不多时全部展开，多时只展开第一层
  const [open, setOpen] = useState<Set<string> | null>(null);
  const opened = open ?? new Set(total <= 200 ? tree.flatMap(dirsUnder) : tree.filter((n) => n.dir).map((n) => n.path));
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [skipped, setSkipped] = useState<ImportResult[]>([]);

  if (q.isPending) return <Stack spacing={1} sx={{ p: 3 }}>{[0, 1, 2, 3].map((i) => <Skeleton key={i} variant="rounded" height={28} />)}</Stack>;
  if (q.error) return <PreviewUnavailable reason={q.error.message} onDownload={onDownload} />;
  if (!total) return <PreviewUnavailable title={t('压缩包是空的')} reason={t('里面没有文件。')} onDownload={onDownload} />;

  const all = tree.flatMap(filesUnder);
  const toggleDir = (p: string) => setOpen(() => { const n = new Set(opened); if (n.has(p)) n.delete(p); else n.add(p); return n; });
  const pick = (paths: string[], on: boolean) => setPicked((s) => { const n = new Set(s); paths.forEach((p) => (on ? n.add(p) : n.delete(p))); return n; });
  const run = () => imp.mutate([...picked], {
    onSuccess: (res) => {
      const ok = res.filter((r) => r.jobId).length;
      setSkipped(res.filter((r) => !r.jobId));
      setPicked(new Set());
      app.toast(ok ? t('已导入 {n} 个文件，正在处理，完成后在待整理里', { n: ok }) : t('没有导入文件'));
    },
    onError: (e) => app.toast(e.message),
  });

  const row = (n: Node, depth: number): React.ReactNode => {
    const files = filesUnder(n);
    const on = files.length > 0 && files.every((f) => picked.has(f.path));
    const some = !on && files.some((f) => picked.has(f.path));
    const isOpen = opened.has(n.path);
    return (
      <Box key={n.path} role="treeitem" aria-expanded={n.dir ? isOpen : undefined} aria-label={n.name}>
        <Stack direction="row" sx={{ alignItems: 'center', height: 36, pl: 1 + depth * 2.5, pr: 2, borderBottom: 1, borderColor: 'divider', gap: 0.5, '&:hover': { bgcolor: 'action.hover' }, transition: 'background-color 150ms ease-out' }}>
          {canImport && (
            <Checkbox size="small" checked={on} indeterminate={some} disabled={!files.length} sx={{ p: 0.5 }}
              slotProps={{ input: { 'aria-label': t('选择“{name}”', { name: n.name }) } }} onChange={(e) => pick(files.map((f) => f.path), e.target.checked)} />
          )}
          {n.dir
            ? <ButtonBase onClick={() => toggleDir(n.path)} sx={{ flex: 1, minWidth: 0, justifyContent: 'flex-start', gap: 0.75, height: '100%', borderRadius: 1 }} aria-label={isOpen ? t('收起“{name}”', { name: n.name }) : t('展开“{name}”', { name: n.name })}>
              {isOpen ? <ChevronDown size={16} strokeWidth={SW} /> : <ChevronRight size={16} strokeWidth={SW} />}
              {isOpen ? <FolderOpen size={16} strokeWidth={SW} /> : <Folder size={16} strokeWidth={SW} />}
              <Typography noWrap sx={{ fontSize: 14 }}>{n.name}</Typography>
              <Typography sx={{ fontSize: 12, color: 'text.secondary', ml: 'auto', pl: 2 }}>{t('{n} 个文件', { n: files.length })}</Typography>
            </ButtonBase>
            : <Stack direction="row" sx={{ flex: 1, minWidth: 0, alignItems: 'center', gap: 0.75, pl: '22px' }}>
              <FileText size={16} strokeWidth={SW} style={{ flex: 'none' }} />
              <Typography noWrap sx={{ fontSize: 14 }}>{n.name}</Typography>
              {n.encrypted && <Chip size="small" label={t('有密码')} />}
              <Typography sx={{ fontSize: 12, color: 'text.secondary', ml: 'auto', pl: 2, fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>{size(n.size)}</Typography>
            </Stack>}
        </Stack>
        {n.dir && isOpen && n.kids.map((k) => row(k, depth + 1))}
      </Box>
    );
  };

  return (
    <Box sx={{ maxWidth: 880, mx: 'auto', p: { xs: 2, md: 3 } }}>
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center', mb: 1.5, flexWrap: 'wrap', rowGap: 1 }}>
        <Typography variant="body2" sx={{ color: 'text.secondary', flex: 1 }}>
          {q.data!.truncated
            ? (canImport ? t('{n} 个文件（只列出前 5000 个），勾选后可以导入为单独的文档', { n: total }) : t('{n} 个文件（只列出前 5000 个）', { n: total }))
            : (canImport ? t('{n} 个文件，勾选后可以导入为单独的文档', { n: total }) : t('{n} 个文件', { n: total }))}
        </Typography>
        {canImport && (
          <Button variant="contained" disabled={!picked.size || picked.size > MAX_IMPORT || imp.isPending} onClick={run}>
            {picked.size ? t('导入选中的 {n} 个文件', { n: picked.size }) : t('导入选中的文件')}
          </Button>
        )}
      </Stack>
      {picked.size > MAX_IMPORT && <Alert severity="warning" sx={{ mb: 1.5 }}>{t('一次最多导入 {n} 个文件，请少选一些。', { n: MAX_IMPORT })}</Alert>}
      {skipped.length > 0 && (
        <Alert severity="info" sx={{ mb: 1.5 }} onClose={() => setSkipped([])}>
          {t('{n} 个文件没有导入：', { n: skipped.length })}
          <Box component="ul" sx={{ m: 0, mt: 0.5, pl: 2.5 }}>
            {skipped.slice(0, 20).map((r) => <li key={r.path}>{t('{path}：{reason}', { path: r.path, reason: r.reason ?? '' })}</li>)}
          </Box>
        </Alert>
      )}
      <Box role="tree" aria-label={t('压缩包里的文件')} aria-multiselectable={canImport} sx={{ border: 1, borderColor: 'divider', borderRadius: 2, overflow: 'hidden', bgcolor: 'background.paper', '& [role=treeitem]:last-of-type > div': { borderBottom: 0 } }}>
        {canImport && all.length > 1 && (
          <Stack direction="row" sx={{ alignItems: 'center', height: 36, pl: 1, borderBottom: 1, borderColor: 'divider', bgcolor: 'surface.containerLow' }}>
            <Checkbox size="small" sx={{ p: 0.5 }} checked={all.every((f) => picked.has(f.path))} indeterminate={picked.size > 0 && !all.every((f) => picked.has(f.path))}
              slotProps={{ input: { 'aria-label': t('全选') } }} onChange={(e) => pick(all.map((f) => f.path), e.target.checked)} />
            <Typography sx={{ fontSize: 13, color: 'text.secondary', ml: 0.5 }}>{t('全选')}</Typography>
          </Stack>
        )}
        {tree.map((n) => row(n, 0))}
      </Box>
    </Box>
  );
}
