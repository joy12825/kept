Kept / 收好 — 品牌图标资源交付
版本：2026-10-25

来源与精度
以 reference/final-reference.png 中左上角图标为唯一造型参考。
本交付为高精度手绘矢量还原，非逐像素矢量描摹。保留三分之四角度柜体、右前方拉出抽屉、内部文件和蓝色短拉手。
曲线、渐变、颜色和文字字形为人工近似；不能作为与原图完全一致的无损矢量源文件。
reference/top-left-reference-crop.png 为原图裁切，仅供对照，不是重绘成果。

目录与用法
svg/：透明彩色图形、黑白图形、三套应用方形源图和 maskable 源图。
ios/：浅色、深色、着色版 1024×1024 RGB PNG，完全不透明，无预制圆角。
深色版是参考图对应的白色图形 + 黑底；着色版是蓝色图形 + 浅蓝底。
这些是设计资源，具体系统的着色和构建设置仍须在项目中配置。
web/：16、32、180、192、512 PNG，favicon.ico；maskable 为 192 与 512，不透明。
180 是无圆角不透明 Apple touch 源图；其余普通网页 PNG 带蓝色圆角背景。
maskable 图形相对标准源图缩放 80%，实际内容位于中心安全圆内；背景铺满画布。
site.webmanifest 是站点模板，id、start_url、名称等应根据部署项目调整。
wordmarks/：中文/英文字标黑白 SVG、横排与竖排组合 SVG/PNG；字形均已转路径。
字标按参考排版近似：中文 Hiragino Sans GB W6，英文 Arial Bold；不宣称是原图字体。
preview/brand-preview.png：对照与尺寸预览；preview/index.html：本地交互验收页面。

网页接入示例
<link rel="icon" href="/brand/web/favicon.ico" sizes="16x16 32x32">
<link rel="apple-touch-icon" href="/brand/web/kept-180.png">
<link rel="manifest" href="/brand/web/site.webmanifest">

主色：#075CF5（主蓝近似）；浅背景：#DFEAFE；深背景：#090C12。
SVG viewBox 为 461×459，沿用参考图裁切比例；PNG 输出为正方形。
生成源码保存在 build_brand.py；依赖 Python、cairosvg、Pillow、fonttools 及脚本所列 macOS 字体。
checks.json 记录实际文件检测结果；SHA256SUMS.txt 用于核对完整性。
