// 办事材料清单的模板（v1.6.2）：常见的几件事要哪些材料。每项先按关键词找，用户可以改成类型、标签等条件。
import type { ChecklistInput } from '../api/checklists';
import { t } from '../i18n';

const kw = (...words: string[]) => (words.length === 1 ? { text: words[0] } : { any: words.map((w) => ({ text: w })) });

export const CHECKLIST_TEMPLATES: ChecklistInput[] = [
  {
    name: t('办签证'), note: t('各国要求不同，以使馆的最新要求为准'),
    items: [
      { label: t('护照'), filter: kw('护照') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('身份证'), filter: kw('身份证') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('户口本'), filter: kw('户口本', '户口簿') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('在职证明'), filter: kw('在职证明', '工作证明') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('近 6 个月银行流水'), filter: kw('流水', '对账单'), count: 6 }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('房产证或行驶证'), filter: kw('房产证', '不动产权', '行驶证'), note: t('资产证明，可选') }, // i18n-ignore（关键词按文档里的中文找）
    ],
  },
  {
    name: t('孩子入学'), note: '',
    items: [
      { label: t('出生医学证明'), filter: kw('出生医学证明', '出生证') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('户口本'), filter: kw('户口本', '户口簿') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('预防接种证'), filter: kw('接种') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('房产证或租房合同'), filter: kw('房产证', '不动产权', '租赁合同') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('父母身份证'), filter: kw('身份证'), count: 2 }, // i18n-ignore（关键词按文档里的中文找）
    ],
  },
  {
    name: t('报销'), note: '',
    items: [
      { label: t('发票'), filter: kw('发票'), count: 3 }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('行程单或订单'), filter: kw('行程单', '订单') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('付款记录'), filter: kw('支付', '付款', '账单') }, // i18n-ignore（关键词按文档里的中文找）
    ],
  },
  {
    name: t('租房'), note: t('中介或房东常要的材料'),
    items: [
      { label: t('身份证'), filter: kw('身份证') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('劳动合同或在职证明'), filter: kw('劳动合同', '在职证明') }, // i18n-ignore（关键词按文档里的中文找）
      { label: t('近 3 个月工资条或流水'), filter: kw('工资', '流水'), count: 3 }, // i18n-ignore（关键词按文档里的中文找）
    ],
  },
];
