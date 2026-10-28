import { describe, expect, it } from 'vitest';
import { basisSummary, describeSource, isAuto, sourceLines, type FieldSources } from './classifier';

const fs: FieldSources = {
  type: { source: 'model', confidence: 0.83 },
  correspondent: { source: 'rule', basis: '关键词“国家电网”' },
  date: { source: 'text', basis: '2024年3月5日' },
  tags: { '100': { source: 'rule', basis: '关键词“电费”' }, '101': { source: 'user' } },
};
const names: Record<number, string> = { 100: '水电', 101: '税务' };

describe('字段来源', () => {
  it('说成人话', () => {
    expect(describeSource({ source: 'model', confidence: 0.834 })).toBe('自动学习，把握 83%');
    expect(describeSource({ source: 'rule', basis: '关键词“国家电网”' })).toBe('匹配规则：关键词“国家电网”');
    expect(describeSource({ source: 'text', basis: '2024年3月5日' })).toBe('从正文识别：“2024年3月5日”');
    expect(describeSource({ source: 'preset' })).toBe('上传时指定');
    expect(isAuto({ source: 'preset' })).toBe(false);
    expect(isAuto({ source: 'model' })).toBe(true);
  });

  it('列出各字段；用户去掉的标签不列', () => {
    const lines = sourceLines(fs, [100], (id) => names[id]);
    expect(lines.map((l) => l.field)).toEqual(['类型', '往来方', '日期', '标签“水电”']);
  });

  it('待整理的一句话依据', () => {
    const lines = sourceLines(fs, [100], (id) => names[id]);
    expect(basisSummary(lines)).toBe('依据：匹配规则：往来方（关键词“国家电网”）、标签“水电”（关键词“电费”）；类型由自动学习给出，把握 83%；日期从正文识别');
    expect(basisSummary([])).toBe('没有匹配的规则，自动学习也没有把握，请手动填写。');
  });
});

describe('抽屉的依据', () => {
  it('单独成句', () => {
    const lines = sourceLines({ drawer: { source: 'rule', basis: '收纳规则“证件”' }, type: { source: 'model', confidence: 0.9 } }, [], () => undefined);
    expect(basisSummary(lines)).toBe('依据：放入抽屉按收纳规则“证件”；类型由自动学习给出，把握 90%');
  });
});
