import { describe, expect, it } from 'vitest';
import { parseMarkdown, safeHref, type Block, type Inline } from './markdown';

const plain = (xs: Inline[]): string => xs.map((x) => ('text' in x ? x.text : 'children' in x ? plain(x.children) : x.t === 'image' ? `[${x.alt}]` : '\n')).join('');

describe('Markdown 解析', () => {
  it('标题、段落、分隔线、引用', () => {
    const { blocks } = parseMarkdown('# 家里的网络\n\n第一段\n接着写\n\n---\n\n> 引用的话\n\n副标题\n---\n');
    expect(blocks.map((b) => b.t)).toEqual(['heading', 'paragraph', 'hr', 'quote', 'heading']);
    expect((blocks[0] as Extract<Block, { t: 'heading' }>).level).toBe(1);
    expect(plain((blocks[1] as Extract<Block, { t: 'paragraph' }>).children)).toBe('第一段\n接着写');
    expect((blocks[4] as Extract<Block, { t: 'heading' }>).level).toBe(2);
  });

  it('列表：有序、无序、任务、嵌套', () => {
    const { blocks } = parseMarkdown('1. 重启光猫\n2. 重启路由器\n   - 等两分钟\n\n- [x] 交电费\n- [ ] 交水费\n');
    const [ol, ul] = blocks as Extract<Block, { t: 'list' }>[];
    expect(ol.ordered).toBe(true);
    expect(ol.items).toHaveLength(2);
    expect(ol.items[1].children.some((b) => b.t === 'list')).toBe(true);
    expect(ul.items.map((i) => i.checked)).toEqual([true, false]);
  });

  it('代码块和表格', () => {
    const { blocks, texts } = parseMarkdown('```go\nfmt.Println("hi")\n```\n\n| 设备 | 位置 |\n| --- | :-: |\n| 光猫 | 弱电箱 |\n');
    const code = blocks[0] as Extract<Block, { t: 'code' }>;
    expect(code.lang).toBe('go');
    expect(code.text).toBe('fmt.Println("hi")');
    const table = blocks[1] as Extract<Block, { t: 'table' }>;
    expect(table.align).toEqual([null, 'center']);
    expect(plain(table.rows[0][1])).toBe('弱电箱');
    expect(texts).toContain('弱电箱');
  });

  it('行内：粗体、斜体、删除线、代码、链接；snake_case 不算强调', () => {
    const [p] = parseMarkdown('**密码**不写在 *这里*，~~旧的~~ `ping` [小米](https://www.mi.com) a_b_c').blocks as Extract<Block, { t: 'paragraph' }>[];
    const kinds = p.children.map((x) => x.t);
    expect(kinds).toEqual(expect.arrayContaining(['strong', 'em', 'del', 'code', 'link']));
    expect(plain(p.children)).toContain('a_b_c');
  });

  it('不安全的链接不做成链接；HTML 照字面；图片只是链接', () => {
    const [p] = parseMarkdown('[点我](javascript:alert(1)) <img src=x onerror=alert(1)> ![照片](http://example.com/a.png)').blocks as Extract<Block, { t: 'paragraph' }>[];
    const link = p.children.find((x) => x.t === 'link') as Extract<Inline, { t: 'link' }>;
    expect(link.href).toBeNull();
    expect(plain(p.children)).toContain('<img src=x onerror=alert(1)>');
    const img = p.children.find((x) => x.t === 'image') as Extract<Inline, { t: 'image' }>;
    expect(img.href).toBe('http://example.com/a.png');
    expect(safeHref('mailto:a@b.com')).toBe('mailto:a@b.com');
    expect(safeHref('/api/v1/x')).toBeNull();
    expect(safeHref('data:text/html,hi')).toBeNull();
  });

  it('自动链接去掉句末标点', () => {
    const [p] = parseMarkdown('见 https://www.mi.com/service。').blocks as Extract<Block, { t: 'paragraph' }>[];
    const link = p.children.find((x) => x.t === 'link') as Extract<Inline, { t: 'link' }>;
    expect(link.href).toBe('https://www.mi.com/service');
  });
});
