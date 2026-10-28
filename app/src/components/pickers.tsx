// 选择类型、往来方、标签的下拉面板（带搜索），以及按 ID 查分类名称的索引。
import { useMemo, useState, type ReactNode } from 'react';
import { Box, Checkbox, InputBase, MenuItem, MenuList, Popover, Typography } from '@mui/material';
import { Check, Plus } from 'lucide-react';
import { KIND_LABEL, tagPath, useCreateTaxonomy, useTaxonomy, type Tag, type TaxonomyItem } from '../api/taxonomy';
import { useApp } from '../store/app-store';
import { useDrawers, type Drawer } from '../api/drawers';
import { useLocations, useSaveLocation, type Location } from '../api/locations';
import { SW } from './icons';
import { t } from '../i18n';

export interface TaxonomyIndex {
  types: Map<number, TaxonomyItem>;
  correspondents: Map<number, TaxonomyItem>;
  tags: Map<number, Tag>;
  /** 标签按树形排列（父在前，子缩进） */
  tagTree: { tag: Tag; depth: number; path: string }[];
  typeList: TaxonomyItem[];
  correspondentList: TaxonomyItem[];
  /** 家人、存放位置（v1.6.3） */
  persons: Map<number, TaxonomyItem>;
  personList: TaxonomyItem[];
  locations: Map<number, Location>;
  locationList: Location[];
  /** 抽屉（按顺序） */
  drawers: Map<number, Drawer>;
  drawerList: Drawer[];
  ready: boolean;
}

export function useTaxonomyIndex(): TaxonomyIndex {
  const types = useTaxonomy('document-types');
  const corrs = useTaxonomy('correspondents');
  const tags = useTaxonomy('tags');
  const persons = useTaxonomy('persons');
  const locations = useLocations();
  const drawers = useDrawers();
  return useMemo(() => {
    const tagMap = new Map((tags.data ?? []).map((t) => [t.id, t]));
    const children = new Map<number, Tag[]>();
    for (const t of tags.data ?? []) {
      const p = t.parentId && tagMap.has(t.parentId) ? t.parentId : 0;
      children.set(p, [...(children.get(p) ?? []), t]);
    }
    const tagTree: TaxonomyIndex['tagTree'] = [];
    const walk = (parent: number, depth: number) => {
      for (const t of children.get(parent) ?? []) {
        tagTree.push({ tag: t, depth, path: tagPath(t, tagMap) });
        if (depth < 10) walk(t.id, depth + 1);
      }
    };
    walk(0, 0);
    return {
      types: new Map((types.data ?? []).map((t) => [t.id, t])),
      correspondents: new Map((corrs.data ?? []).map((t) => [t.id, t])),
      tags: tagMap,
      tagTree,
      typeList: types.data ?? [],
      correspondentList: corrs.data ?? [],
      persons: new Map((persons.data ?? []).map((t) => [t.id, t])),
      personList: persons.data ?? [],
      locations: new Map((locations.data ?? []).map((l) => [l.id, l])),
      locationList: locations.data ?? [],
      drawers: new Map((drawers.data?.items ?? []).map((d) => [d.id, d])),
      drawerList: drawers.data?.items ?? [],
      ready: !!(types.data && corrs.data && tags.data),
    };
  }, [types.data, corrs.data, tags.data, persons.data, locations.data, drawers.data]);
}

/** sortKey 是拼音（连写），用来按拼音查找 */
export interface PickerOption { id: number; label: string; depth?: number; title?: string; color?: string; count?: number; sortKey?: string }

/** 查找：名称或完整路径包含输入（不分大小写），或拼音包含输入（去掉空格），同 ⌘K。 */
export function optionMatches(o: PickerOption, q: string): boolean {
  const query = q.trim().toLowerCase();
  if (!query) return true;
  return o.label.toLowerCase().includes(query) || !!o.title?.toLowerCase().includes(query) || !!o.sortKey?.includes(query.replace(/\s+/g, ''));
}

interface PickerProps {
  anchor: HTMLElement | null;
  onClose: () => void;
  options: PickerOption[];
  /** 多选时为已选的 ID；单选时为当前值 */
  selected: number[];
  multi?: boolean;
  onPick: (id: number) => void;
  placeholder: string;
  /** 单选时的“清空”项，例如“没有类型” */
  clearLabel?: string;
  /** 输入的名称不存在时可以新建（v1.6.0）：返回新建的 ID，随后选上；未提供时不显示 */
  onCreate?: (name: string) => Promise<number>;
  /** 列表上方的额外内容（v1.6.1：标签的“任一 / 全部 / 不含”，类型的“是 / 不是”） */
  header?: ReactNode;
}

/** 带搜索的选择面板：单选点一下就关闭，多选可以连续勾选。 */
export function Picker({ anchor, onClose, options, selected, multi, onPick, placeholder, clearLabel, onCreate, header }: PickerProps) {
  const [q, setQ] = useState('');
  const [creating, setCreating] = useState(false);
  const shown = options.filter((o) => optionMatches(o, q));
  const pick = (id: number) => { onPick(id); if (!multi) { setQ(''); onClose(); } };
  const name = q.trim();
  const canCreate = !!onCreate && !!name && !options.some((o) => o.label.toLowerCase() === name.toLowerCase());
  const create = async () => {
    if (!onCreate || creating) return;
    setCreating(true);
    try {
      const id = await onCreate(name);
      setQ('');
      pick(id);
    } catch { /* 出错时由调用方提示，面板保持打开 */ } finally { setCreating(false); }
  };
  return (
    <Popover open={!!anchor} anchorEl={anchor} onClose={() => { setQ(''); onClose(); }} anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
      slotProps={{ paper: { sx: { width: 280, p: 1, mt: 0.5 } } }}>
      <InputBase autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder={placeholder} inputProps={{ 'aria-label': placeholder }}
        onKeyDown={(e) => { if (e.key === 'Enter' && !e.nativeEvent.isComposing && canCreate && !shown.length) { e.preventDefault(); void create(); } }}
        sx={{ width: '100%', height: 36, px: 1.5, mb: 0.5, borderRadius: 1, bgcolor: 'surface.containerHigh', fontSize: 13 }} />
      {header}
      <MenuList role="listbox" aria-label={placeholder} aria-multiselectable={multi || undefined} autoFocusItem={false} dense disablePadding sx={{ maxHeight: 320, overflow: 'auto' }}>
        {clearLabel && !q && (
          <MenuItem dense onClick={() => pick(0)} sx={{ gap: 1, borderRadius: 1, color: 'text.secondary' }}>{clearLabel}</MenuItem>
        )}
        {shown.map((o) => {
          const on = selected.includes(o.id);
          return (
            <MenuItem key={o.id} dense role="option" aria-selected={on} title={o.title} onClick={() => pick(o.id)}
              sx={{ gap: 1, borderRadius: 1, pl: 1 + (q ? 0 : (o.depth ?? 0) * 2), opacity: o.count === 0 && !on ? 0.45 : 1, transition: 'opacity 150ms ease-out' }}>
              {multi ? <Checkbox size="small" checked={on} sx={{ p: 0.25 }} tabIndex={-1} /> : <Box sx={{ width: 18, display: 'flex', color: 'primary.main' }}>{on && <Check size={16} strokeWidth={SW} />}</Box>}
              {o.color && <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: o.color, flex: 'none' }} />}
              <Typography noWrap sx={{ flex: 1, fontSize: 13 }}>{q && o.title ? o.title : o.label}</Typography>
              {o.count !== undefined && <Typography sx={{ fontSize: 12, color: 'text.secondary' }}>{o.count}</Typography>}
            </MenuItem>
          );
        })}
        {canCreate && (
          <MenuItem dense disabled={creating} onClick={() => void create()} sx={{ gap: 1, borderRadius: 1, color: 'primary.main' }}>
            <Box sx={{ width: 18, display: 'flex' }}><Plus size={16} strokeWidth={SW} /></Box>
            <Typography noWrap sx={{ flex: 1, fontSize: 13 }}>{t('新建“{name}”', { name })}</Typography>
          </MenuItem>
        )}
        {!shown.length && !canCreate && <Typography component="li" sx={{ p: 1.5, fontSize: 13, color: 'text.secondary' }}>{t('没有符合的选项')}</Typography>}
      </MenuList>
    </Popover>
  );
}

export const typeOptions = (ix: TaxonomyIndex): PickerOption[] => ix.typeList.map((t) => ({ id: t.id, label: t.name, sortKey: t.sortKey }));
export const correspondentOptions = (ix: TaxonomyIndex): PickerOption[] => ix.correspondentList.map((t) => ({ id: t.id, label: t.name, sortKey: t.sortKey }));
export const personOptions = (ix: TaxonomyIndex): PickerOption[] => ix.personList.map((t) => ({ id: t.id, label: t.name, sortKey: t.sortKey }));
export const locationOptions = (ix: TaxonomyIndex): PickerOption[] => ix.locationList.map((l) => ({ id: l.id, label: l.name }));
export const tagOptions = (ix: TaxonomyIndex): PickerOption[] => ix.tagTree.map(({ tag, depth, path }) => ({ id: tag.id, label: tag.name, depth, title: path, color: tag.color, sortKey: tag.sortKey }));
export const drawerOptions = (ix: TaxonomyIndex): PickerOption[] => ix.drawerList.map((d) => ({ id: d.id, label: d.name }));

/** 选择面板里新建（v1.6.0）：建好返回 ID；失败时提示并保持面板打开。 */
export function usePickerCreate(kind: 'correspondents' | 'document-types' | 'tags' | 'persons') {
  const app = useApp();
  const create = useCreateTaxonomy(kind);
  return async (name: string): Promise<number> => {
    try {
      const made = await create.mutateAsync({ name } as never);
      app.toast(t('已新建{kind}“{name}”', { kind: KIND_LABEL[kind], name: made.name }));
      return made.id;
    } catch (e) {
      app.toast((e as Error).message);
      throw e;
    }
  };
}

/** 选择面板里新建存放位置（v1.6.3）。 */
export function usePickerCreateLocation() {
  const app = useApp();
  const save = useSaveLocation();
  return async (name: string): Promise<number> => {
    try {
      const made = await save.mutateAsync({ body: { name } });
      app.toast(t('已新建存放位置“{name}”', { name: made.name }));
      return made.id;
    } catch (e) {
      app.toast((e as Error).message);
      throw e;
    }
  };
}
