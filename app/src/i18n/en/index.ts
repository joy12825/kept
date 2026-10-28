// 英文对照表：按页面分文件，这里合起来。只在界面语言是英文时加载（startLanguage）。
import type { Catalog } from '..';
import core from './core';
import settingsA from './settings-a';
import settingsB from './settings-b';
import components from './components';
import detail from './detail';
import pages from './pages';
import misc from './misc';
import edition from './edition';
import { eeCatalog } from '@ee';

const all: Catalog = { ...core, ...settingsA, ...settingsB, ...components, ...detail, ...pages, ...misc, ...edition, ...eeCatalog };

export default all;
