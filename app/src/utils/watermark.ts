// 能不能加水印（v1.11.2）：分享链接的水印选项和下载加水印的副本都用。
import type { DocumentDetail } from '../api/documents';

/** 能直接画水印的图片（和服务器一致） */
export const STAMPABLE_IMAGES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/bmp', 'image/tiff'];

type Doc = Pick<DocumentDetail, 'mimeType' | 'hasArchive'>;

export const isPicture = (d: Pick<Doc, 'mimeType'>) => STAMPABLE_IMAGES.includes(d.mimeType);

/** 这份文档能不能加水印：有 PDF（原件或可搜索版），或原件是图片 */
export const canWatermark = (d: Doc) => d.hasArchive || d.mimeType === 'application/pdf' || isPicture(d);
