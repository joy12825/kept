// 开源构建的 '@ee'（v1.13）：收费模块的页面和组件都不在这个构建里。
// 页面显示“不包含收费功能”，嵌在核心页面里的组件不显示；设置里的收费分项由
// 核心的 EditionNotice 说明，这里也不显示。
// 导出的名字要和 src/ee/index.ts 一一对应（ee.test.ts 检查）。
import { EmptyState } from '../components/common';
import { t } from '../i18n';
import type { Catalog } from '../i18n';

function NotInBuild(_props: Record<string, unknown>) {
  return <EmptyState title={t('这是开源的免费版，不包含收费功能')} />;
}

function Nothing(_props: Record<string, unknown>): null {
  return null;
}

export const AiProposals = Nothing;
export const CollectionDialog = Nothing;
export const EmergencyNotice = Nothing;
export const AskPage = NotInBuild;
export const AssistCatalogButton = Nothing;
export const BookSummarySection = Nothing;
export const DocAsk = Nothing;
export const LabReviewPage = NotInBuild;
export const LabSection = Nothing;
export const readingKind = (..._args: unknown[]): undefined => undefined;
export const SimilarDocuments = Nothing;
export const OrganizeSuggestions = Nothing;
export const FactSuggestions = Nothing;
export const HealthPage = NotInBuild;
export const ScenePage = NotInBuild;
export const HealthReportPage = NotInBuild;
export const HealthReportView = Nothing;
export const ScenesPage = NotInBuild;
export const VisitPage = NotInBuild;
export const MakeCardsButton = Nothing;
export const ReviewBar = Nothing;
export const PublicUploadPage = NotInBuild;
export const AiPanel = Nothing;
export const BrandingPanel = Nothing;
export const CollectionsPanel = Nothing;
export const EmergencyContactsPanel = Nothing;
export const MyEmergencyAccessPanel = NotInBuild;
export const MailPanel = Nothing;
export const RemoteInboxPanel = Nothing;
export const ScenesPanel = Nothing;
export const SSOStatus = Nothing;
export const UploadLinksPanel = Nothing;
export const WorkflowsPanel = Nothing;

// v1.13.2：收费功能的新建与配置
export const ShareBundleDialog = Nothing;
export const ShareDialog = Nothing;
export const WatermarkDialog = Nothing;
export const PersonPrivacySection = Nothing;
export const BackupSection = Nothing;
export const ExportSection = Nothing;
export const ColdDialog = Nothing;
export const QuotaDialog = Nothing;
export const UploadQuotaDialog = Nothing;
/** 开关存储加密：开源构建没有，按钮也不显示。 */
export const useEncryptionSwitch = () => ({ ask: (_st: unknown) => {}, turn: (_name: string, _enabled: boolean) => {}, dialog: null });

/** 收费模块的英文对照：开源构建没有。 */
export const eeCatalog: Catalog = {};

/** 这个构建是否包含收费模块。 */
export const eeBuild = false;
