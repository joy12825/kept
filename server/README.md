# 收好（Kept）服务端

Go 编写的单体服务：HTTP 接口加后台任务 worker，数据存放在 SQLite 中。部署见 `../docs/部署.md`，各版本的变化见 `../CHANGELOG.md`。

## 环境要求

- Go 1.26 及以上
- 不需要 cgo：SQLite 以 WASM 形式运行（`ncruces/go-sqlite3`）
- 内存：全文搜索的中文分词词典常驻约 110 MB（gse 内嵌简体词典），实测几百份文档时整个服务常驻约 270 MB
- 入库 PDF 和图片需要外部工具：poppler（`pdfinfo`、`pdftotext`、`pdftoppm`）、`ocrmypdf` 和 tesseract 的中文语言包。macOS：`brew install ocrmypdf poppler tesseract-lang`；Debian / Ubuntu：`apt install ocrmypdf poppler-utils tesseract-ocr-chi-sim`。缺少时服务照常启动并在日志中警告，这类文件的入库任务会失败（纯文本仍可入库），相关测试自动跳过
- Office 文档（doc、docx、xls、xlsx、ppt、pptx、odt、ods、odp、rtf）需要 [Gotenberg](https://gotenberg.dev)，用 `KEPT_GOTENBERG_URL` 指定地址；没有配置时这类文件入库失败并说明原因

## 常用命令

```bash
make run        # 本地启动，数据放在 ./data，监听 :8000
make test       # 运行测试（带 -race）
make generate   # 修改 SQL 或 ../api/openapi.yaml 后重新生成代码
make build      # 编译到 bin/kept
make help       # 查看全部命令
```

启动后访问：

```bash
curl localhost:8000/healthz
curl localhost:8000/api/v1/system/version
```

## 命令行

```
kept serve                        启动 HTTP 服务和后台任务
kept migrate                      执行数据库迁移后退出（serve 启动时也会自动执行）
kept user create [--admin] --email <邮箱> [<用户名>]  创建账号
kept user set-password <用户名或邮箱>  重置密码并结束该账号的所有会话（忘记管理员密码时使用）
kept index rebuild                删除全文索引并按数据库重新建立（服务需要先停止）
kept classifier eval [-k 5] <空间 ID>  用 k 折交叉验证评估自动分类，按对象输出召回与误判（只读，服务运行时也可执行）
kept check                        检查每个空间的文档、文件和全文索引是否一致（不删除任何文件；服务运行时每周自动检查）
kept backup [<目录>]              备份数据库快照和它引用的文件到目录（服务运行时也可以执行，增量复制，快照保留 7 份）；不写目录时用 KEPT_BACKUP_DIR
kept restore [--snapshot <文件名>] [--force] <目录>  把备份恢复到空的数据目录（先校验原件）
kept import paperless --workspace <空间名称或 ID> <导出目录>  导入 Paperless-ngx 的导出（document_exporter），可以重复执行
kept version                      显示版本信息
```

`kept user` 的密码取自环境变量 `KEPT_PASSWORD`，未设置时从终端读取。

`serve` 和 `migrate` 发现数据库需要升级时，先把 `app.db` 备份到 `data/backups/app-schema<旧版本号>-<时间>.db`（保留最近 3 份）再升级。

### 演示数据

```sh
kept demo load <用户名或邮箱>
```

为该账号建立“王家”（家庭）和“某某工作室”（团队）两个空间，写入示例数据：往来方、类型、标签（含颜色和匹配规则）、示例文档（已归档的带原件编号，另有两份在回收站里）和常用筛选，并为每个空间训练自动分类模型；待整理里的示例记下了分类依据（规则或自动学习、正文里的日期），能看到“自动填入”的对勾。抽屉按原型建好（收纳规则、要点、手动放入的文档、大事标记、年度回顾里的用途标签），示例的到期日按加载当天推算，首页能看到提醒；“王家”的房子、车、孩子只读共享给了“某某工作室”；“王家”有一台已连接的手机“小王的 iPhone”（只用于展示，它的 token 无法使用），待整理里最新的几份标为手机扫描。示例文档是按原型数据生成的中文 PDF（字体为内嵌的思源黑体子集，`internal/demo/fonts`），走真实的入库流水线（文字层、缩略图），再填上原型中的类型、往来方、标签、金额和是否待整理；缺少 poppler 时跳过文档并给出提示。账号需要先存在；可以重复执行，已有的数据不会重复写入。正式部署不会自动执行。以后每个阶段有了新表（抽屉、提醒……），对应的示例数据会加进这个命令（`internal/demo`）。

## 首次使用

第一次打开时还没有任何账号：网页端调用 `GET /api/v1/setup` 得到 `needsSetup: true`，显示“创建管理员账号”表单，提交到 `POST /api/v1/setup`。也可以用命令行创建：`kept user create --admin admin`。

## 认证方式

- **网页端**：Cookie 会话 `kept_session`（HttpOnly、SameSite=Lax，闲置 30 天过期，使用时自动续期）。修改类请求必须带 `X-CSRF-Token` 请求头，值取自登录或 `GET /api/v1/me` 返回的 `csrfToken`。
- **脚本和 App**：`Authorization: Bearer kept_…`，在“设置 → API Token”中创建，不需要 CSRF Token。
- 哪些接口无需登录由 `../api/openapi.yaml` 中的 `security: []` 决定；`admin` 开头的接口只允许实例管理员调用。

## 配置（环境变量）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `KEPT_DATA_DIR` | `data` | 数据目录：`app.db`、`index/`（全文索引，可随时重建，不需要备份）、`tmp/`、`media/`、`branding/`、`backups/`（升级前的自动备份） |
| `KEPT_HTTP_ADDR` | `:8000` | 监听地址 |
| `KEPT_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `KEPT_LOG_FORMAT` | `text` | `text` / `json` |
| `KEPT_WORKERS` | CPU 核数的一半，最多 8 | 后台任务的并发数 |
| `KEPT_SHUTDOWN_TIMEOUT` | `30s` | 优雅退出的最长等待时间 |
| `KEPT_SESSION_TTL` | `720h` | 登录会话的闲置有效期 |
| `KEPT_AUDIT_RETENTION` | `8760h` | 活动记录（审计日志）的保留期限，过期的记录每天 05:00 清理 |
| `KEPT_SECURE_COOKIE` | `auto` | Cookie 的 Secure 标记：`auto`（HTTPS 或反向代理声明 HTTPS 时开启）/ `always` / `never` |
| `KEPT_MAX_UPLOAD_SIZE` | `200MB` | 单个上传文件的上限：字节数，或带 `KB`、`MB`、`GB` 单位 |
| `KEPT_OCR_LANGUAGES` | `chi_sim+eng` | OCR 语言（tesseract 语言包需另行安装） |
| `KEPT_SEARCH_LIMIT` | `10000` | 一次关键词搜索最多召回的文档数 |
| `KEPT_GOTENBERG_URL` | 空 | Office 文档转换服务（Gotenberg）的地址，如 `http://gotenberg:3000` |
| `KEPT_BARCODES` | `false` | 识别上传的 PDF 中的条码：分隔页（`KEPT_BARCODE_SEPARATOR`，默认 `PATCHT`）拆分、ASN（`KEPT_BARCODE_ASN_PREFIX`，默认 `ASN`）填原件编号 |
| `KEPT_CONSUME_DIR` | 空 | 监听目录（绝对路径）：每个空间一个“<ID>-<名称>”文件夹，子文件夹作为标签，“双面”文件夹合并双面扫描 |
| `KEPT_BACKUP_DIR` | 空 | 每日自动备份（02:30）的目录，绝对路径；设置后系统状态页可以“立即备份” |
| `KEPT_PUBLIC_URLS` | 空 | 手机配对二维码中的服务器地址（逗号分隔，按顺序尝试），放在网页端当前访问地址之前 |
| `TZ` | 系统时区 | 调度器每日计划项（抽屉提醒 03:00、清理回收站 04:00、已删除的空间 04:30、审计记录 05:00）按这个时区；官方镜像设为 `Asia/Shanghai` |
| `KEPT_OCR_MODE` | `skip` | `skip`：只识别没有文字的页面，已有文字层的 PDF 不另存可搜索版；`redo`：重新识别已有的 OCR 文字；`force`：全部页面栅格化后重新识别 |

## 目录结构

```
cmd/kept/                 程序入口和子命令
internal/
  app/                    组装各模块、启动和优雅退出
  config/                 读取环境变量
  logging/                slog 日志；最近 2000 条保存在内存里，供“设置 → 日志”查看
  version/                版本号（编译时注入）
  auth/                   账号、个人空间、密码（argon2id）、会话、API Token、登录限流、两步验证
  tenant/                 当前请求所在的空间（Scope），空间内数据的唯一入口
  apperr/                 带类别的业务错误，由 api 统一映射成状态码
  audit/                  审计日志（与业务修改在同一事务中写入）
  workspace/              空间与成员
  taxonomy/               往来方、类型、标签、自定义字段
  pinyin/                 拼音排序键与分组首字母
  branding/               实例名称与 Logo
  document/               文档：列表（keyset 分页）、分面计数、详情、修改、批量操作、回收站
  filter/                 筛选条件 JSON 的解析与编译（参数化 SQL；关键词交给 search）
  search/                 全文索引（Bleve + gse 中文分词）、查询语法、后台索引器
  classify/               自动分类：匹配规则、TF-IDF + Complement Naive Bayes 模型、训练与评估、入库时的建议、重新分类任务
  drawer/                 抽屉：收纳规则与放入、模板、要点、提醒、年度回顾、只读共享
  notes/                  文档的备注
  views/                  常用筛选（空间内共享）
  ingest/                 入库与重新处理：接收上传、去重、解析、提交
  parser/                 类型识别、PDF 文本层、OCR、缩略图、日期识别（调用 poppler、ocrmypdf）；Office 文档经 Gotenberg 转成 PDF
  files/                  原件、可搜索版、缩略图的存放位置（media/）与临时目录
  events/                 进程内事件总线，供 SSE（/api/v1/events）订阅
  backup/                 在线备份与恢复（kept backup、kept restore、每日备份任务）
  barcode/                条码：渲染页面后用 gozxing 识别 QR 码与 Code 128，得出分隔页与原件编号
  mail/                   邮件收取：IMAP（go-imap v1）、按条件筛选、附件入库、mail_seen 防重复
  paperless/              从 Paperless-ngx 导入：分类、字段、文档（原样复制文件）、备注
  pages/                  PDF 页面编辑：旋转、删除页面（原件不变，可恢复）、拆分与合并（qpdf）
  watch/                  监听目录：按空间文件夹入库、子文件夹作为标签、双面扫描合并（qpdf）
  integrity/              完整性检查：文件与记录双向比对、补建缺失的索引（每周计划项与 kept check）
  system/                 系统状态：版本、存储、索引、任务队列、外部工具与 Gotenberg
  demo/                   kept demo load 的演示数据
  web/                    嵌入网页端的构建产物
  api/                    HTTP 处理函数、中间件（请求 ID、访问日志、panic 恢复、认证、CSRF）
    gen/                  由 ../api/openapi.yaml 生成（oapi-codegen），不要手改
  store/                  打开 SQLite、升级前备份、执行迁移
    migrations/           迁移脚本，按编号顺序执行
    queries/              SQL 查询，sqlc 据此生成代码
    sqlc/                 由 sqlc 生成，不要手改
  jobs/                   任务队列（jobs 表）和 worker 池：重试退避、心跳、崩溃恢复；调度器（schedules 表）
tools/                    代码生成工具的独立 go.mod（sqlc、oapi-codegen），不污染主模块依赖
```

## 开发约定

- **接口先改规范**：先修改 `../api/openapi.yaml`，执行 `make generate`，再实现 `gen.StrictServerInterface` 中新增的方法。
- **数据库**：新增迁移文件 `internal/store/migrations/000N_描述.sql`，查询写在 `internal/store/queries/`，然后 `make generate`。迁移和查询文件**只用 ASCII 字符**（sqlc 的 SQLite 引擎遇到中文会算错偏移）。
- **时间**：数据库中统一存 Unix 毫秒。
- **后台任务**：在 `internal/app/app.go` 的 `registerJobs` 中注册处理函数；不值得重试的错误用 `jobs.Permanent(err)` 包装。
- **空间隔离**：访问空间内数据的查询一律接收 `tenant.Scope` 参数（而不是裸的 workspace ID），从请求上下文中用 `tenant.MustFrom(ctx)` 取得。每新增一种空间内的资源，都要在 `internal/api/isolation_test.go` 中加一条“别人的资源返回 404”的用例。
- **错误响应**：统一为 `{code, message, details}`，`message` 用面向用户的中文。
- 模块名暂定为 `kept/server`，确定代码托管地址后用 `go mod edit -module` 修改。
