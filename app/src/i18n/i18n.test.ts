import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyPreference, browserLang, resolve, setLanguageForTest, t, tc } from '.';
import { scan } from './source';

afterEach(() => { setLanguageForTest('zh'); localStorage.clear(); });

describe('t()', () => {
  it('中文时原样，填占位符', () => {
    expect(t('上传 {n} 个文件', { n: 3 })).toBe('上传 3 个文件');
  });

  it('英文时查对照表，缺了用中文；按 n 选单复数', () => {
    setLanguageForTest('en', { '上传': 'Upload', '{n} 份文档': { one: '# document', other: '# documents' }, '你好，{name}': 'Hello, {name}' });
    expect(t('上传')).toBe('Upload');
    expect(t('{n} 份文档', { n: 1 })).toBe('1 document');
    expect(t('{n} 份文档', { n: 4 })).toBe('4 documents');
    expect(t('你好，{name}', { name: 'Wang' })).toBe('Hello, Wang');
    expect(t('没有翻译的')).toBe('没有翻译的');
    setLanguageForTest('en', { '目录': 'Directory', '目录|书的大纲': 'Contents' });
    expect(tc('书的大纲', '目录')).toBe('Contents');
    expect(t('目录')).toBe('Directory');
    setLanguageForTest('zh');
    expect(tc('书的大纲', '目录')).toBe('目录');
  });
});

describe('语言', () => {
  it('浏览器是中文（任何地区）用中文，其他用英文；账号设置优先', () => {
    expect(browserLang(['zh-TW', 'en'])).toBe('zh');
    expect(browserLang(['en-US', 'zh-CN'])).toBe('en');
    expect(browserLang(['fr'])).toBe('en');
    expect(resolve('zh', ['en-US'])).toBe('zh');
    expect(resolve('', ['en-US'])).toBe('en');
  });

  it('账号上的设置和这台浏览器记下的不同：记下，要显示的语言变了才重新加载', () => {
    setLanguageForTest('zh');
    const reload = vi.fn();
    expect(applyPreference('zh', reload)).toBe(false); // 已经是中文：只记下
    expect(localStorage.getItem('kept.lang')).toBe('zh');
    expect(reload).not.toHaveBeenCalled();
    expect(applyPreference('en', reload)).toBe(true);
    expect(reload).toHaveBeenCalledOnce();
    expect(applyPreference('en', reload)).toBe(false); // 记过了
  });
});

describe('找界面文字', () => {
  it('t() 的键、漏网的中文、注释和 i18n-ignore', () => {
    const f = scan([
      "// 注释里的中文不算",
      "const a = t('上传');",
      "const b = '没翻译';",
      "const c = <Box title={t('标题 {n}', { n })}>正文</Box>;",
      "/* 块注释 */ const d = /^(\\d+)年$/; // i18n-ignore",
      "const e = t(`拼接 ${x}`);",
      "const f = `模板 ${x} 文字`;",
      "const g = x / 2 / 3; const h = '再一个';",
      "const i = <Icon size={1} />; const j = t('图标后面');",
      "const k = tc('书的大纲', '目录');",
    ].join('\n'));
    expect(f.keys.map((k) => k.key)).toEqual(['上传', '标题 {n}', '图标后面', '目录|书的大纲']);
    expect(f.bare.map((b) => `${b.line}:${b.text}`)).toEqual(['3:没翻译', '4:正文', '7:模板   文字', '8:再一个']);
    expect(f.dynamic).toEqual([6]);
  });
});
