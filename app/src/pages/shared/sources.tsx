// 共享来的内容有两种：共享抽屉与共享集（v1.0 阶段 37）。两者的只读页面相同，这里给出各自的数据与地址。
import type { ReactNode } from 'react';
import { Box } from '@mui/material';
import { Layers } from 'lucide-react';
import type { UseInfiniteQueryResult, UseQueryResult, InfiniteData } from '@tanstack/react-query';
import { sharedFileUrl, useSharedDocument, useSharedDocuments, useSharedDrawer, type SharedDocument } from '../../api/drawers';
import { sharedCollectionFileUrl, useSharedCollection, useSharedCollectionDocument, useSharedCollectionDocuments } from '../../api/collections';
import type { ApiError } from '../../api/client';
import type { components } from '../../api/schema';
import { DrawerIcon, SW } from '../../components/icons';
import { t } from '../../i18n';

type Info = { name: string; documentCount: number; sourceWorkspaceName: string };
type List = components['schemas']['SharedDocumentList'];

export interface SharedSource {
  route: string;
  /** 放在句中的名称（英文小写） */
  noun: string;
  useInfo: (id: number) => UseQueryResult<Info, ApiError>;
  useDocuments: (id: number, q: string) => UseInfiniteQueryResult<InfiniteData<List>, ApiError>;
  useDocument: (id: number, docId: number) => UseQueryResult<SharedDocument, ApiError>;
  fileUrl: (id: number, docId: number, kind: 'original' | 'preview' | 'thumbnail') => string;
  icon: (info: Info, size: number) => ReactNode;
}

export const drawerSource: SharedSource = {
  route: '/shared',
  noun: t('抽屉').toLowerCase(),
  useInfo: useSharedDrawer,
  useDocuments: useSharedDocuments,
  useDocument: useSharedDocument,
  fileUrl: sharedFileUrl,
  icon: (info, size) => <DrawerIcon drawer={info as unknown as Parameters<typeof DrawerIcon>[0]["drawer"]} size={size} shared />,
};

/** 共享集的图标：一叠文件。 */
export function CollectionIcon({ size }: { size: number }) {
  return (
    <Box sx={{ width: size, height: size, borderRadius: size > 30 ? 2 : 1, bgcolor: 'surface.secondaryContainer', color: 'surface.onSecondaryContainer', display: 'flex', alignItems: 'center', justifyContent: 'center', flex: 'none' }}>
      <Layers size={Math.round(size * 0.6)} strokeWidth={SW} />
    </Box>
  );
}

export const collectionSource: SharedSource = {
  route: '/shared-collections',
  noun: t('共享集').toLowerCase(),
  useInfo: useSharedCollection,
  useDocuments: useSharedCollectionDocuments,
  useDocument: useSharedCollectionDocument,
  fileUrl: sharedCollectionFileUrl,
  icon: (_, size) => <CollectionIcon size={size} />,
};
