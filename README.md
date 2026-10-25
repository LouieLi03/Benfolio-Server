# Benfolio Server

供用户自行部署到 NAS 的开源阅读服务端。书库、书籍版本、阅读进度和笔记属于同一套产品，不依赖运营方提供托管云。与 Benfolio 阅读器分别维护，本仓库不包含 Apple 客户端源码。采用 AGPL-3.0-only 许可证，暂无发行镜像。

## 当前状态

项目处于框架阶段，尚未完成构建与运行验证，暂不提供可用发行版。目前包含以下代码与配置：

- Go 启动入口、环境配置、结构化日志和退出处理。
- 单 Owner 的 Bearer API Key 认证；支持从独立文件读取凭证。
- SQLite 初始化、事务迁移、迁移摘要检查和较新数据库降级拒绝。
- 持久化服务实例 ID 与 Owner ID；更换 Key 不另建用户。
- 启动时检查一个书籍来源目录并登记书库；同一规范化来源路径复用书库 ID，显示名称可配置。
- 受认证保护的 EPUB/PDF 来源文件发现接口；限定目录遍历、返回数量与并发，只列举候选文件，不解析或入库。
- 持久化扫描任务、后台文件发现与计数、任务查询、活动任务复用及启动时中断状态恢复；支持 discovery、epub_import、pdf_import 和 book_import 阶段。
- 单本 EPUB/PDF 来源导入：分别解析 EPUB 元数据和 PDF 书名、作者、页数，按 SHA-256 归档原件，以事务登记书籍、版本及来源绑定；同路径同内容复用版本，换版保留旧版本。
- 书籍详情、分页版本历史与受认证保护的原件下载；按版本读取归档，支持单段 Range、HEAD 和 ETag 条件请求。
- 健康状态、可配置的服务显示名称、服务信息、能力声明、书库/书目查询接口。
- 原书版本、页面、进度、批注、资源、工作 PDF、变更日志、操作回执和任务的数据结构。
- 按原书版本读取与条件保存阅读进度；修订号冲突保护、操作ID幂等回执和按Owner过滤的增量变更查询，状态/事件/回执在同一事务提交。
- 书签、高亮、文字笔记的版本内查询、条件新增/修改、软删除；批注修订、变更事件与操作回执同事务保存，已删除记录保留且不允许旧ID复活。
- 便携笔画资源的受认证上传/下载、原字节摘要校验、不可变归档及Owner归属；PDF手写批注引用资源，关联/状态/事件/回执同事务保存，保留历史引用。
- 从原件和有效批注快照生成用户工作PDF，添加标准高亮/备注/Ink及外观流；保留历史副本、变更位置和操作回执，生成结果结构校验后登记，批注已变化时拒绝替换当前副本。
- 独立PDF/Markdown导出记录与认证下载：PDF固定引用工作修订，EPUB/PDF笔记汇总绑定批注快照；文件/发布记录/回执一致登记，保留原件与旧导出。
- 独立 Dockerfile、Compose 和配置示例。

阅读进度、文字批注、PDF手写资源/引用及工作PDF生成/读取的服务端代码已加入，尚未构建或运行验收；Apple客户端的第一方进度/批注/笔画适配尚未完成，不能据此声明跨设备阅读或笔记恢复已可用。Pencil数据转换、跨端绘制保真、客户端工作PDF/分享联动、带批注EPUB副本、管理界面、完整备份恢复均未实现；独立PDF和Markdown导出代码已加入但未运行验收。新数据库未配置来源时书库查询返回空列表；配置有效来源后返回登记的书库，单本或批量 EPUB/PDF 导入成功后才有书目。下载针对已入库的原件，不是批注导出。未实现的业务接口返回 `501 not_implemented`。能力声明中相应项目为 `false`。Apple 客户端适配在独立项目维护，书库与原件下载适配代码已加入，尚未完成联调与验收。

## 工程结构

```text
cmd/benfolio-server/       启动、退出及容器健康探测入口
internal/config/          环境配置和凭证文件读取
internal/domain/          书库、版本、进度与批注契约
internal/httpapi/         HTTP 路由、认证、参数和错误返回
internal/source/          受限的来源目录读取与文件发现
internal/scan/            后台扫描任务执行与状态收尾
internal/publication/     受限的 EPUB/PDF 元数据读取
internal/importer/        来源快照、原书归档与导入流程
internal/originals/       归档读取、完整摘要校验与下载并发限制
internal/progress/        位置校验、修订前提及操作请求摘要
internal/annotation/      文字/手写引用批注、版本锚点及操作校验
internal/resource/        不可变笔画资源、内容校验及文件读写
internal/workingpdf/      批注快照生成PDF、外观流与不可变历史文件
internal/export/          固定修订PDF、Markdown笔记及导出文件发布
internal/position/        EPUB内容位置与比例校验
internal/syncvalue/       操作标识与原书摘要的公共校验
internal/jsonvalue/       有界JSON归一化与重复键拒绝
internal/localfile/       Linux/macOS 普通文件的受限打开
internal/storage/         状态目录、SQLite、书目/进度查询及事务
internal/storage/migrations/  内嵌迁移
deploy/                   Compose 和环境变量示例
```

运行状态目录结构（当前仅导入流程写入原书归档与暂存）：

```text
data/
  benfolio.sqlite         元数据、进度、变更记录与操作回执
  originals/              不可变原书归档
  working/                按用户/内容版本管理的工作文件
  resources/              笔迹和其他不可变资源
  staging/                尚未发布完成的暂存
  exports/                不可变Markdown导出文件
```

数据库使用 WAL；仅复制主数据库文件不能作为完整备份。当前按单实例、本机持久磁盘设计，不承诺多副本或网络文件系统上的共享 SQLite。来源书籍目录独立挂载为只读；启动检查最多读取一个条目名称。只读文件发现由请求或后台任务触发，受限递归列举目录和文件元数据，不读取书籍正文；显式导入读取正文并归档到独立状态目录。来源文件均不改写。

## 保存与同步边界

阅读进度、文字批注与手写资源的服务端代码分别见下方对应章节，尚未运行验收；工作PDF生成代码见专节；PDF/Markdown导出代码见专节；带批注EPUB副本、客户端Pencil转换、绘制保真和完整恢复仍是后续目标：

- **PDF**：保留导入原件；批注写入每个用户自己的工作 PDF，分享文件携带可见笔迹；NAS 同时保存可迁移笔画和同步记录。工作 PDF 的字节摘要与原书内容摘要分开，不能因画了一笔就把原书认作换版。写回来源路径需要后续明确的发布操作，当前只读挂载不支持写回。
- **EPUB**：原件保留；日常按内容锚点同步高亮、书签和文字备注；导出带批注 EPUB 副本及笔记汇总。其他阅读器是否识别内嵌批注单独验证。自由手写先后置，不绑定可重排的屏幕页码。
- **多端**：已有稳定实例/用户/内容版本身份；进度及文字/手写引用批注加入修订号条件写入、操作ID重试与变更日志，批注软删除保留墓碑；手写字节按摘要独立获取。不同批注分别修订，同一批注冲突不自动覆盖；本机修改保留、冲突选择、跨端界面与重装恢复仍待客户端接入。
- **恢复**：原件、工作文件、数据库及引用资源都属于恢复范围；尚未上传的设备离线修改不能由 NAS 恢复。当前不删除旧版和墓碑，不提供备份/恢复 API。

## 本机开发

需要 Go 1.26.10 或更新版本，Dockerfile 默认使用1.26.10。依赖版本记录在 go.mod/go.sum；SQLite 驱动为 modernc.org/sqlite，容器配置使用 `CGO_ENABLED=0`。来源读取使用 `os.Root`；构建版本须包含其路径处理修复，参见 [Go 安全公告](https://pkg.go.dev/vuln/GO-2026-4970)及[发布记录](https://go.dev/doc/devel/release#go1.26.10)。

```sh
umask 077
mkdir -p .local
openssl rand -hex 32 > .local/api-key
export BENFOLIO_API_KEY_FILE="$PWD/.local/api-key"
go build -o bin/benfolio-server ./cmd/benfolio-server
./bin/benfolio-server
```

默认监听 `:8080`，状态目录为当前目录下 `.local/benfolio-server/`。不要把 Key 写进源码、共享日志或提交到 Git。

| 配置 | 说明 |
|---|---|
| `BENFOLIO_SERVER_NAME` | 服务显示名称；去除首尾空白后为空时使用 `Benfolio` |
| `BENFOLIO_LISTEN_ADDR` | 默认 `:8080` |
| `BENFOLIO_DATA_DIR` | 持久状态目录，运行用户必须具备真实读写权限 |
| `BENFOLIO_BOOKS_DIR` | 可选的书籍来源目录；配置后须存在、是目录且可列举内容，启动时登记为书库；容器内为 `/books` |
| `BENFOLIO_LIBRARY_NAME` | 书库显示名称；去除首尾空白后为空时使用 `书库`，下次启动更新名称并保留 ID |
| `BENFOLIO_API_KEY_FILE` | 推荐：独立文件，内容为 32–512 字节、无空白的 Key |
| `BENFOLIO_API_KEY` | 环境变量替代方式，不能与文件配置同时提供 |

当前 Key 只在启动时载入，认证为单 Owner，不支持注册、密码登录、多用户授权或自动 Key 轮换。外部访问的 HTTPS 由部署入口配置，服务当前提供 HTTP。

例如设置 `BENFOLIO_SERVER_NAME="家里的书库"` 后，重启服务，`GET /api/v1/server` 的 `name` 字段返回 `家里的书库`。直接运行时通过环境变量配置；Compose 部署时写入 deploy/.env。显示名称只影响服务信息，不改变实例 ID、用户 ID、数据库路径或来源目录。

直接运行时可设置 `BENFOLIO_BOOKS_DIR` 和 `BENFOLIO_LIBRARY_NAME`；Compose 部署时设置 deploy/.env 中的 `BENFOLIO_BOOKS_PATH` 与 `BENFOLIO_LIBRARY_NAME`。来源检查先于状态目录创建；来源与状态目录不能互相包含，检查会解析符号链接，也会检查尚未创建的状态目录的已有父目录。

来源目录、书库记录与名称在启动时处理。相同规范化来源路径复用书库 ID，不重复建库；不同路径另建书库，旧记录保留。取消来源配置后，已有书库仍返回，但 `sourceConfigured=false`。API 不返回来源绝对路径。`sourceConfigured` 表示本次启动已检查并绑定来源，不是持续的挂载监控，也不证明所有书籍可读或已经导入。

来源路径以服务进程看到的路径为准。容器中替换宿主机挂载但仍使用 `/books` 时会复用该书库，当前不检测磁盘或挂载身份。

## 当前接口

健康接口以外均要求 `Authorization: Bearer <API Key>`；当前根路径不提供管理网页。

| 方法与路径 | 当前行为 |
|---|---|
| `GET /health/live` | 进程响应 |
| `GET /health/ready` | 数据库连接可用；不代表书籍来源或同步已就绪 |
| `GET /api/v1/server` | 实例 ID、版本、数据库版本、`stage=scaffold` |
| `GET /api/v1/me` | 当前 Owner 的稳定 ID 与显示名 |
| `GET /api/v1/capabilities` | 实际能力开关 |
| `GET /api/v1/libraries` | 持久化书库列表；每项包含 id、name、createdAt、sourceConfigured，新数据库未配置来源时为空 |
| `GET /api/v1/libraries/{libraryID}/source-files` | 本次配置来源的 EPUB/PDF 候选文件预览；支持 limit，默认100、上限200 |
| `POST /api/v1/libraries/{libraryID}/scan` | 提交或复用指定阶段扫描，返回202；空请求体仅发现文件，显式选择 EPUB、PDF 或两种格式的批量入库阶段 |
| `POST /api/v1/libraries/{libraryID}/imports` | 按来源相对路径同步导入单本 EPUB/PDF，返回书籍 ID、版本、元数据和新建标记 |
| `GET /api/v1/tasks/{taskID}` | 当前 Owner 的扫描任务状态、发现数量、导入统计、结果摘要与错误 |
| `GET /api/v1/books` | 支持 query、format、limit、offset 的书目查询 |
| `GET /api/v1/books/{bookID}` | 书籍详情及当前版本 ID；未知书籍404 |
| `GET /api/v1/books/{bookID}/versions` | 版本历史，支持 limit、offset；未知书籍404 |
| `GET /api/v1/versions/{versionID}` | 版本详情及保存的元数据，PDF 包含页数与声明版本；未知版本404 |
| `GET /api/v1/versions/{versionID}/file` | 按版本下载归档原件，支持 HEAD、单段 Range 和条件请求 |
| `GET /api/v1/versions/{versionID}/progress` | 当前Owner的版本进度，无记录时revision为0；未知版本404 |
| `PUT /api/v1/versions/{versionID}/progress` | 带操作ID、原书摘要与基础修订号的条件保存 |
| `GET /api/v1/changes?after=0&limit=50` | 当前Owner的进度及文字/手写引用批注增量变更 |
| `GET /api/v1/versions/{versionID}/annotations?limit=50` | 批注分页列表，含删除墓碑 |
| `GET /api/v1/versions/{versionID}/annotations/{annotationID}` | 单条批注，包括已软删除记录 |
| `PUT /api/v1/versions/{versionID}/annotations/{annotationID}` | 条件新增或完整更新书签、高亮、文字笔记及手写资源引用 |
| `DELETE /api/v1/versions/{versionID}/annotations/{annotationID}` | 带JSON请求体的条件软删除 |
| `PUT /api/v1/resources/{sha256}` | 上传单个PDF笔画组JSON，按原始字节校验SHA-256 |
| `GET /api/v1/resources/{sha256}` | 读取当前Owner笔画资源；支持HEAD、单段Range和条件请求 |
| `GET /api/v1/versions/{versionID}/working-pdf` | 工作PDF状态、批注变更位置和是否过期；可按revision查询历史 |
| `POST /api/v1/versions/{versionID}/working-pdf` | 基于预期批注位置及工作副本修订号显式生成 |
| `GET /api/v1/versions/{versionID}/working-pdf/file` | 下载当前或指定revision的历史工作PDF |
| `GET /api/v1/versions/{versionID}/export-context` | 原件摘要、当前批注序号/数量及最新工作修订 |
| `POST /api/v1/versions/{versionID}/exports` | 显式创建固定修订PDF或笔记Markdown导出 |
| `GET /api/v1/exports/{exportID}` | 当前Owner的不可变导出记录 |
| `GET /api/v1/exports/{exportID}/file` | 按记录下载导出文件 |

书目查询当前只按标题匹配；`format` 为 epub/pdf，`limit` 默认30、上限100，`offset` 最大1000000。返回 `items/limit/offset/hasMore`；已导入书目含 `currentVersionID`。接口和模型仍处于草案阶段。

导出仅支持同步创建PDF/Markdown记录，不是后台任务；尚不支持上传书籍、覆盖上传工作PDF或带批注EPUB副本。progress、annotations、resources、working-pdf、exports与changes已接业务处理，POST libraries仍501。内置配置的annotationKinds为bookmark/highlight/note/ink，resourceMediaTypes仅含application/vnd.benfolio.ink+json、inkSchemaVersions仅含1；resourceTransfer随资源服务接线声明，inkSync须同时有资源与批注服务，epubInk仍为false。pdfWorkingCopy/pdfAnnotationMaterialization随工作PDF服务接线声明；pdfExport须有导出及工作PDF服务，notesMarkdownExport/epubNotesExport随导出服务声明，exportFormats为markdown/pdf，epubAnnotationExport仍为false。客户端必须检查具体类型与版本，不能据此启用尚未实现的Pencil转换、编辑后PDF上传或带批注EPUB副本。接口存在或能力声明均不替代构建与运行验收。

书库来源目前仅通过启动配置登记；`librarySourceConfiguration=true` 表示支持此配置。`POST /api/v1/libraries` 仍返回501，`libraryManagement=false`；尚不支持通过 API 增删书库或同时配置多个来源目录。

## 来源文件发现

`source-files` 接口要求同一 Owner API Key，仅列举指定书库本次绑定的来源。未知书库返回404；历史书库未绑定当前来源时返回409；目录读取失败返回503，不把读取错误报告成完整空书库。该接口不写入书目、版本或扫描任务，`sourceDiscovery` 与显式写入的 `scan` 分开声明。

返回 `libraryID/items/complete/incompleteReasons`，每个候选项包含 `relativePath/format/byteSize/modifiedAt`。扩展名匹配不区分大小写；只返回普通 EPUB/PDF 文件，跳过枚举时遇到的符号链接与特殊文件。相对路径使用 `/` 分隔，不返回宿主机绝对路径。扩展名匹配不证明文件格式有效、正文可读或已通过权限检查，零字节候选仍可能出现在预览中。

目录按128条一批读取，最多处理10000个目录条目、递归32层、返回200个候选。达到限制时 `complete=false`，`incompleteReasons` 给出 `file_limit/entry_limit/depth_limit`；遍历中消失的条目或无效UTF-8名称会被跳过并标为 `source_changed/invalid_name`。响应同时提供实际访问条目数、限制值及相应跳过计数。预览结果不是全库清单，不能用于判定哪些旧书应被删除。

同一进程的文件发现与单本导入共用来源访问限制；其他预览/导入请求返回429并提示重试。请求配置5秒上下文期限，在文件操作之间检查取消；底层文件系统调用阻塞时不能保证在5秒内返回。超时返回504，失败不返回部分结果。成功返回的候选按相对路径排序，但遍历取样顺序由文件系统决定，没有分页游标；目录变化或返回限制都可能使多次结果不同。

目录操作使用 Go `os.Root` 句柄。服务启动时打开来源根目录，关闭服务时释放；移动或替换根路径后不自动切换句柄，需要重启。当前面向Linux/macOS部署；根目录内另行挂载的文件系统仍属于可访问范围，`os.Root` 不替代部署挂载边界。

## 扫描任务：发现与书籍入库

扫描请求在认证后验证书库来源，先将任务提交到SQLite，再返回202及任务查询的 `Location`。同一 Owner、同一书库、同一阶段已有 queued/running 任务时复用原ID，响应标记 `reused=true`；同书库已有其他阶段活动任务时返回409 scan_stage_busy；任务结束后再次请求会创建新任务。可选择 discovery、epub_import、pdf_import 或 book_import 阶段，不接受客户端提交来源路径；响应丢失后可重试以复用仍活动的任务，不能据此承诺跨终态的严格一次执行。

请求体为空时保留 `stage=discovery` 的只读发现行为；也可发送 `Content-Type: application/json` 与下面的选项（请求体上限1024字节）：

```json
{"stage":"book_import"}
```

任务的 `kind=source_scan`、`stage=discovery/epub_import/pdf_import/book_import`；阶段在提交时持久化，不会把旧排队发现任务升级为导入。任务状态为 `queued → running → succeeded/failed`。`sourceScanTasks=true` 表示后台任务可用；`epubScanImport/pdfScanImport` 分别表示对应格式导入阶段，`scan=true` 表示可以通过 book_import 显式处理两种格式，不改变空请求的只读行为。epub_import 只导入 EPUB、跳过 PDF；pdf_import 相反；book_import 处理两种格式。

后台执行通过流式回调处理候选，不收集整库文件列表，也不受预览接口200项限制。discovery 阶段每128个候选保存发现数量，完成后保存最终计数；不读取正文、入库或生成同步事件，成功只表示完成发现。`result.items` 为空，不表示没有发现文件。任务不自动清理来源、旧版或已入库书目。

入库阶段顺序处理候选，未被阶段选择的格式计为跳过。每本使用与单本导入相同的128 MiB等解析边界及20秒上下文期限；来源占用由整个扫描持有，回调只获得当前候选的读取入口，避免重复加锁。每本书独立提交；坏书、大小超限、来源变化、不可读或单本超时会记录错误后继续；归档校验失败、未知存储错误或数据库故障会停止后续导入。批次超时、中断或后续失败不回滚先前成功的书籍。

任务的 `import` 提供 `processedFiles/importedFiles/newBooks/newVersions/reusedVersions/skippedFiles/failedFiles`。processedFiles 只统计已完成处理的候选，等于导入确认成功、跳过及失败数量之和；importedFiles 等于新增版本与复用版本之和，newBooks 不超过 newVersions。例如 EPUB 专用任务只发现PDF时可成功，但导入数量为0。每完成一本被选格式的书籍保存统计，纯跳过时每128个候选保存，收尾保存最终值；发现数可能包含尚未完成的当前文件。

每本书事务与任务计数分别提交，强制中断时统计可能落后于实际入库结果；没有逐文件断点恢复。重新扫描按同路径、同内容复用版本，不能将任务计数当成全库精确账本。超时或断开也可能令当前文件结果未确认，不能据此判定其必然未入库。

逐文件错误保留前50个 `relativePath/code`，超出后 `failuresTruncated=true`，失败总数继续累加；不返回来源绝对路径或底层错误文本。仅当前Owner可查此摘要。被选格式的书籍处理存在错误时任务为 `failed/import_errors`；已完成的目录发现仍可能 `result.complete=true`，该字段始终只描述发现完整性。入库阶段的 succeeded 表示当前受限扫描内被选格式的候选已确认导入、其他格式已跳过；仍不证明客户端阅读、笔记或进度同步已通过验收。

同一进程只执行一个扫描任务，并与预览、单本导入共用来源访问限制。扫描最多处理1000000个目录条目、递归64层，执行上下文期限为10分钟；底层文件系统阻塞的取消限制与预览相同。达到遍历边界、遇到目录变化或无效名称时标记 `failed/incomplete_discovery`，保留不完整原因；读取失败、超时和中断使用独立错误码。发现结果不保证目录快照一致或格式有效，导入阶段另按逐文件结果判断任务是否成功。

重启时，遗留 running 任务标为 `failed/interrupted`；queued 任务保留并由后台继续领取。若更改服务进程看到的来源路径并绑定到另一书库，旧排队任务不会切换到新书库，而是记录来源未绑定错误；宿主挂载身份的识别限制见来源配置说明。运行中的中断任务不自动重跑，也不声称从文件级检查点续扫。任务查询始终按当前 Owner 过滤，来源绝对路径不写入任务响应。

关闭服务时会取消后台执行，另用短时上下文记录收尾状态，再关闭来源与数据库。如果底层I/O阻塞，退出可能延迟；强制终止后由下次启动恢复任务状态。结果落库失败时记录服务日志，任务可能保留running直至重启处理。当前按单实例运行设计，任务历史不自动清理。

## 单本 EPUB/PDF 导入

这是独立的同步导入入口；后台各入库阶段复用同一解析、归档和版本登记流程。使用相同的 Owner Key，向 `POST /api/v1/libraries/{libraryID}/imports` 发送 `Content-Type: application/json`，请求体例如：

```json
{"relativePath":"小说/示例.epub"}
```

路径来自来源文件预览，使用 `/` 分隔；请求体上限8192字节，路径上限4096字节。只接受当前书库来源内的普通 EPUB/PDF 文件，不接受绝对路径、上级路径、反斜杠或显式符号链接。当前读取正文的实现面向 Linux/macOS，来源挂载仍为只读，不能通过接口提交服务器任意绝对路径。

导入先复制到独立状态目录并计算 SHA-256，再对同一份副本解析与归档。两种格式的文件都必须非空且不超过128 MiB；EPUB 支持经典 ZIP 中的 EPUB 2.0/3.0 包，最多10000个条目、8 MiB中央目录、1 MiB容器描述和2 MiB OPF，XML深度上限64，不接受DTD。暂不支持ZIP64、分卷包、附加尾部数据；重复或不安全的条目路径被拒绝。包内 `mimetype`、容器描述、OPF 版本、非空标题及本地 spine 引用会被检查；不做完整 EPUB 合规、正文解密或渲染验证，也不请求外部资源。存在 `encryption.xml` 时只返回 `hasEncryptionDescriptor=true`，不据此判定为DRM；字体混淆和阅读能力仍需后续区分。

元数据返回首个非空书名、全部作者、首个语言及标识符列表；每组最多64项、每项最多4096字节。先按来源读取句柄、文件身份、大小及修改时间检查变化，解析后落库前再次检查；发现变化时返回409。该检查不是文件系统快照，不能检测被恢复到相同元数据的所有原地修改；来源文件应在写入完成后导入。

归档位于 `originals/<摘要前两位>/<摘要>.<epub或pdf>`，使用只读文件权限并禁止应用覆盖；来源文件始终不改写。暂存与原件目录必须位于同一支持硬链接及目录同步的本机文件系统。已有归档需重新校验大小和完整摘要，异常时拒绝入库，不覆盖或删除。先发布归档，再以一次SQLite事务保存书籍、版本、该版本的元数据及来源绑定；数据库失败可能留下未引用的完整归档，当前不自动清理。请求结束时尝试移除本次暂存文件；清理失败或强制中断遗留的暂存不自动清理。

同书库、同相对路径保持同一个 `bookID`；相同内容复用 `version.id`，内容变化新增版本并保留历史。来源恢复为已导入的旧字节时切回原版本，书目显示该版本元数据。同内容出现在不同路径时分别建书，仅复用物理归档；改名/移动尚不自动关联原书；仅改扩展名不作为格式转换处理。笔记和进度不跨版本迁移，不写入原书。归档摘要仅标识内容，不能代替书籍或用户身份。

新建版本返回201，同内容重试返回200；响应包含 `bookID/version/metadata/bookCreated/versionCreated`。来源访问占用时返回429，格式无效/不支持或超出范围返回422，读取失败返回503；未知书库404、未绑定来源409。请求采用20秒上下文期限，底层I/O阻塞时不能保证期限；客户端超时或断开可能无法确认已完成的事务，可重试同一路径与内容复用版本。导入不产生同步事件；`epubImport/pdfImport` 与 `download`、`progressSync` 分开声明，不代表批注同步能力。

## PDF 元数据与解析边界

PDF 与 EPUB 使用同一导入地址，请求例如 `{"relativePath":"资料/示例.pdf"}`，格式由扩展名选择后交给对应解析器验证。PDF 读取固定依赖 [pdfcpu v0.16.3](https://github.com/pdfcpu/pdfcpu/releases/tag/v0.16.3)，使用独立的无状态配置与宽松结构验证；处理交叉引用、目录和页树，提取书名、作者、页数及声明的PDF版本。缺少书名时使用来源文件名（不含扩展名），作者字段作为单项保留，不猜测逗号分隔的人名。字段归一化后最多4096字节；页数范围1–10000。现有 metadata 的 EPUB 字段保持兼容，PDF 专用值在 `metadata.pdf.pageCount/version`；对PDF不适用的EPUB字段为空；可通过 GET /api/v1/versions/{versionID} 再次读取保存的元数据。元数据只在新版本创建时保存，重复内容复用原版本的元数据，不改写已存摘要或文件。

第一阶段拒绝加密PDF，包括可用空密码打开但仍存在加密字典的文件；不接收或保存PDF密码、不移除保护。密码/权限错误返回422 encrypted_pdf；无效、不支持或超出限制的文件返回422 invalid_pdf。没有OCR、扫描件识别、封面渲染、全文索引、页面稳定ID/几何登记或笔迹处理；PDF 2.0等特性的接受范围以此版本解析器为限，结构验证不等同于全部页面已成功渲染。已有可见批注仍作为原件字节保留，尚不提取成可编辑的同步笔记。

解析不调用渲染、图片提取、水印检测或写出API，不访问文档外链，不加载用户配置、字体或证书目录。只对暂存快照解析，不向来源路径写回；库内部的宽松修复仅发生在内存模型，归档仍是复制时参与 SHA-256 计算的原始字节。校验/导入后可按同一内容版本下载原件，原件下载并不代表支持PDF编辑。

额外限制：间接对象缓冲4 MiB、单个编码流32 MiB、单个解码流及合并页面内容32 MiB、对象编号范围和单段交叉引用均为100000、对象流最多10000个对象/1 MiB前导、递归深度64，图像相关限制为1600万像素和64 MiB。不开启全流解码。这些限制不等于进程总内存上限，多个对象仍可能叠加占用；解析采用调用方的20秒上下文，取消是协作式的，无法恢复进程内存耗尽或保证每次底层调用及时返回。

Compose 新增默认512m容器内存限制，以及 `GOMEMLIMIT=384MiB` 的Go软内存目标，可分别用 BENFOLIO_MEMORY_LIMIT、BENFOLIO_GO_MEMORY_LIMIT 调整。运行二进制时需要自行配置进程/容器资源上限；只有Go软目标不能保证硬性内存边界。触及容器硬限制可能导致进程被终止；重启后沿用现有任务中断恢复，不把中断任务记为成功。

批量阶段遇到 encrypted_pdf/invalid_pdf 后继续处理其他书；意外解析器panic转换为 pdf_parser_failed 并停止后续导入，不输出原始错误、堆栈或书籍内容。归档和数据库故障仍停止批次；已有成功书籍保留。不修改既有数据库迁移；PDF元数据使用现有版本元数据记录，旧EPUB任务仍按其提交阶段执行。

## 版本查询与原件下载

先查询书籍列表或详情取得 `currentVersionID`，再请求 `/api/v1/versions/{versionID}/file`。版本列表默认每页30项、最多100项，offset 最大1000000，返回 `items/limit/offset/hasMore`；各项包含 `id/bookID/sha256/byteSize/createdAt`，按创建时间文本及ID降序排列。分页不是数据库快照；并发导入可能影响后续页。详情和版本列表只查数据库，采用5秒上下文期限，不检查来源或原件是否仍可读取。

所有请求均要求 Owner Bearer Key，包括 HEAD、Range 和条件请求。下载只接受已登记版本ID，从数据库读取并检查对象键，使用固定的摘要文件名定位归档；客户端不能提交文件路径。当前导入入口可登记 EPUB 与受支持的未加密 PDF。下载不会切换到来源目录的最新文件、生成工作PDF或进行笔迹导出；来源改名、换版或取消配置后，保留在状态目录的旧版本仍按原ID访问。

文件打开限制在启动时固定的 originals 根目录内，拒绝显式符号链接和特殊文件。发送内容前核对登记大小、完整 SHA-256 及读取前后的大小/修改时间；归档缺失或无读权限返回503，摘要、大小或对象键不一致返回409，不回退到来源副本或自动修复。HEAD 与返回304的请求也先校验归档。校验使用流式缓冲，每次请求都读取整本归档；断点续传能减少网络传输，但当前不缓存完整性校验结果，频繁分段会增加磁盘读取。

传输使用 [Go ServeContent](https://pkg.go.dev/net/http#ServeContent) 处理单段字节 Range、If-Range、If-None-Match、If-Match 及修改时间条件；正常完整下载200、有效分段206、未修改304、前置条件失败412、不可满足范围416。多段、多个Range头或超出256字节的Range头由本服务返回400。文件名为 `<sha256>.<格式>`，Content-Type 按已登记格式设置，Content-Disposition 为 attachment；ETag 为带引号的 `sha256-<完整原件摘要>`，Last-Modified 使用版本登记时间。缓存策略为 private, no-store；客户端应根据版本ID和摘要管理离线文件，下载或续传完成后核对完整文件摘要，避免把不同版本的片段拼接。

同一进程最多2个在途原件请求，包含数据库查询、校验和传输，占用到响应结束；超出时429并提示2秒后重试。不与来源扫描争用同一个访问槽。版本记录查询期限5秒、摘要校验期限20秒、单次下载总上下文期限5分钟，原件大小上限512 MiB；内置服务器为下载延长写入期限，外层代理或包装器可能采用更短期限。底层I/O阻塞时无法保证按期限结束。开始传输后遇到读取失败会中断响应，不向书籍字节追加JSON；传输成功不代表客户端已持久保存或阅读验收。

这些校验依赖应用保持归档不变，不能替代只允许受信任用户写入状态目录的部署权限。校验后由外部进程原地改写文件仍可能影响传输；客户端完整摘要检查是最终接收判断。接口不返回对象键、来源绝对路径或底层路径错误，当前不提供匿名或签名分享链接。

## 阅读进度与增量变更

以下为本轮新增服务端契约，未运行构建、数据库迁移或联调，Apple客户端尚未调用这些接口。现有Owner Bearer Key认证覆盖进度、冲突响应与变更查询；请求中不能指定其他用户。进度按Owner＋documentVersionID隔离，旧版可继续保存自己的位置，不自动迁移到新版，不修改原书、工作PDF或笔记。

### 位置与条件写入

先从书籍详情取得版本ID，再从版本详情取得原件sha256。GET进度始终返回200：无记录时为 `documentVersionID/locator:null/completed:false/revision:0`，不伪造起始位置、不写入数据库；有记录时还返回 `deviceID/updatedAt`。未知版本返回404。已读状态不会把位置重置为首页，客户端可分别提供“接着读”和“重新阅读”。

PUT请求必须是单个JSON对象，Content-Type为application/json，总体不超过32 KiB，归一化后的locator不超过16 KiB，嵌套不超过32层；拒绝重复键、未知请求/载荷/位置封装/PDF字段、缺失字段与无效类型。以下示例中摘要占位符必须替换为该版本实际的64位小写sha256：

```json
{
  "id": "b6749e19-3706-4968-b8d6-a4811513b6c1",
  "deviceID": "reader-ipad",
  "baseRevision": 0,
  "payload": {
    "sourceSHA256": "<从版本详情复制sha256>",
    "locator": {"format":"pdf","pdf":{"pageIndex":0,"progression":0.25}},
    "completed": false
  }
}
```

- id为本次操作的稳定ID，deviceID为设备安装身份；仅接受1–128个ASCII字母、数字、短横线或下划线。超时后保留同一ID及原请求，新阅读操作使用新ID。deviceID不是账号，也不参与授权。
- baseRevision显式必填，创建时为0，后续使用最近读取的revision；取值0至9007199254740990。每次成功的新操作递增1，不比较客户端时钟，不执行“最后请求直接覆盖”。completed必须显式提供布尔值。
- PDF使用零基pageIndex，必须小于该版本保存的PDF页数。可选progression为当前页内0–1的比例，不能代替原件页索引；尚未提供稳定页面ID和编辑后页映射。
- EPUB locator示例为 `{"format":"epub","epub":{"href":"EPUB/chapter1.xhtml","type":"application/xhtml+xml","locations":{"progression":0.3}}}`。保留完整内容Locator及扩展字段，检查非空href/type、相对内容URI、已知progression/totalProgression范围与正整数position，不换算成屏幕页码。仅做结构校验，不保证href、CFI或摘录确实存在于书内，也不执行EPUB内容加载或重新锚定。
- PUT成功返回200，包含progress、changeSeq和replayed。progress含该版本位置、完成状态、递增revision、deviceID及服务器UTC更新时间。changeSeq为十进制字符串；不将它当作进度revision。

位置、upsert变更和操作回执在同一SQLite事务中保存；新增迁移006仅为reading_progress补充device_id，不修改001–005迁移。启动应用006后，旧版程序将因数据库版本较新拒绝打开；升级前由部署者保留完整状态目录备份。此处未实际执行迁移或部署。

### 冲突与重试

| 情况 | 响应与处理 |
|---|---|
| 两台设备基于同一revision更新 | 后提交者409 progress_conflict，响应current为当前远端位置；本次不写入进度、事件或回执。客户端须保留本机分支，不能收到409后无提示地改baseRevision再次覆盖 |
| 相同操作ID、版本、设备、基础revision和载荷再次提交 | 返回原来提交的progress/changeSeq，replayed=true，不新增事件，不递增revision；即使其他设备后来更新过，也返回原回执而非冒充最新状态 |
| 已成功使用的操作ID对应不同请求 | 409 mutation_id_reused，不能复用ID覆盖回执 |
| sourceSHA256与目标版本不一致 | 409 source_version_mismatch，不保存位置 |
| 传输断开、504或500 | 写入结果可能未知，保留本机位置，使用原ID与原请求重试确认；不要直接当成失败后新建另一操作 |

幂等摘要含操作种类、版本、设备、baseRevision和完整载荷；JSON对象键顺序和空白不影响摘要；不保证不同数字表示全部等价，尤其是EPUB透传字段，客户端应保留待传原操作。回执与变更均不自动清理。数据库、事件或回执写入任一步失败会回滚本次事务；事务已经提交但响应丢失时依靠回执确认。既有单实例/单数据库连接约束保留，不声明多进程并发部署支持。

### 增量读取

GET `/api/v1/changes?after=0&limit=50`，after为非负十进制整数，limit默认为50、上限100。返回items、nextCursor（十进制字符串）及hasMore；事件包含seq（字符串）、documentVersionID、entityType、entityID、revision、operation、payload和createdAt。进度事件的entityType为progress、entityID为版本ID、operation为upsert，payload为提交时的完整进度快照。

客户端仅在应用并持久保存这一页事件后更新自己的游标；hasMore为true时用nextCursor继续。无新事件保持原游标，不跳到未返回的记录。游标需绑定服务实例和Owner；超出当前Owner历史上限返回409 change_cursor_invalid，保留本机未传修改后重新读取，不以清空本机数据恢复。游标不是数据库恢复世代标识，备份回退后的完整同步与冲突恢复流程仍待实现。

变更流覆盖已提交的阅读位置，以及文字/手写引用批注的upsert/delete事件；手写事件只携带资源摘要，资源字节需另行下载。工作PDF生成也产生working_pdf事件，包含副本元数据与文件摘要，文件通过其专用接口下载。不包含书目导入、原件下载或独立资源上传，不替代书库刷新。轮询、iPhone/iPad自动接续和笔记恢复仍待客户端适配，尚无推送连接或端到端同步完成确认。

代码阅读顺序：`internal/httpapi/progress.go`接收请求与返回错误 → `internal/progress/service.go`校验位置并生成操作摘要 → `internal/storage/progress.go`以事务提交位置/事件/回执；`internal/storage/changes.go`读取增量事件。HTTP层不直接写数据库，存储层不处理HTTP状态码，domain接口用于约束两层之间的输入和结果。

待部署者验收：首次创建/读取、EPUB定位字段保留、PDF页码边界、版本隔离和摘要不符、并发旧revision冲突、丢响应后的原请求重试、操作ID换载荷、事件分页与游标回退，以及006迁移前后数据保留。此列表不表示这些场景已通过。

## 书签、高亮与文字笔记

本节为新增服务端代码契约，未运行构建、数据库迁移或联调。复用同一Owner认证、原书版本和增量变更流，不增加独立笔记服务。本节描述bookmark/highlight/note；PDF的ink资源与引用另见下一节。尚不接收图片资源、工作PDF或整份PDF覆盖上传；服务端生成的工作PDF见后文。

### 读取与身份

批注ID位于URL中的annotationID，由客户端创建；请求体id是每次保存/删除操作的ID，两者不能混淆。批注ID在同一Owner下保持唯一，绑定原书版本及kind后不能换版或改类型；不同用户未来仍需独立授权，目前只有Owner。

GET单条和列表均包含deletedAt不为空的墓碑，不能把它重新展示成有效笔记；不存在的批注404，已软删除的批注GET仍为200。列表limit默认50、上限100，after使用上一页nextCursor（批注ID），按ID升序返回items/nextCursor/hasMore；游标必须属于当前Owner和版本，否则409 annotation_cursor_invalid。列表不是并发快照，不能因某条未出现在列表中就认定已删除。首次恢复可从changes的after=0读取保留的事件，重复事件按实体修订号处理，完整客户端恢复尚未实现。

### 新增和修改

PUT使用application/json，body上限64 KiB；epub或pdf锚点及body各自归一化后不超过16 KiB。与进度相同，拒绝重复键、未知请求/载荷/锚点/正文键，标识为1–128个ASCII字母、数字、短横线或下划线，baseRevision必填。新增时baseRevision=0；修改需提交完整批注及已读取的revision，不是局部PATCH。摘要占位符须替换为目标版本真实sha256。

```json
{
  "id": "note-save-001",
  "deviceID": "reader-ipad",
  "baseRevision": 0,
  "payload": {
    "sourceSHA256": "<从版本详情复制sha256>",
    "kind": "highlight",
    "epub": {
      "href": "EPUB/chapter1.xhtml",
      "quote": {"exact":"选中的原文","prefix":"前文","suffix":"后文"}
    },
    "body": {"text":"自己的理解","color":"#FFF176"}
  }
}
```

- EPUB仅提供epub锚点，href必须是出版物内容URI。可选cfi最多4096字节，quote含exact/prefix/suffix（上限分别8192/2048/2048字节）；highlight必须有非空exact。可选locator完整保留内容定位与扩展字段，其href必须与锚点href一致。没有文本重排后的屏幕坐标，也不声称CFI或摘录已对照正文验证或自动重新锚定。
- PDF仅提供pdf锚点，例如 `{"pageIndex":0,"coordinateSpace":"normalized-crop-unrotated-top-left","rects":[{"x":0.1,"y":0.2,"width":0.5,"height":0.04}]}`。pageIndex为该固定原件版本内的零基索引，须在保存的PDF页数范围内。矩形位于未旋转CropBox的左上原点、0–1比例空间，宽高大于0且区域不能超出页面，最多128个；提供矩形时必须显式声明上述coordinateSpace。highlight至少一个矩形；bookmark只定位页面，不接受矩形；note可定位页面或区域。无矩形时coordinateSpace可省略。
- PDF可选quote保存客户端选文，字段及上限同EPUB；扫描页可没有摘录。服务端不提取/OCR选文、不验证文字和区域对应关系、不转换设备显示坐标；PageID、页面编辑后映射、混合旋转与裁边对齐仍待实现和验收。不接受未登记的pageID，也不借用当前页码充当稳定页ID。
- body必须是对象，支持text和color。note需要非空text；bookmark可用text作标题或备注；highlight可用text保存评论，并可选六位RGB色值#RRGGBB（统一大写）。bookmark/note不接受color；不执行HTML或Markdown内容，展示端须按普通文字处理。自由手写不保存在text中。

PUT成功返回200，含annotation/changeSeq/replayed。annotation含原书版本、类型、锚点、正文、revision、createdAt/updatedAt和deviceID；changeSeq仍为十进制字符串。同一条批注每次新操作递增修订号，其他批注独立修订，不因修改两条不同笔记产生同一对象的冲突。

### 删除、冲突与重试

DELETE同一批注URL也必须携带application/json请求体，例如：

```json
{"id":"note-delete-001","deviceID":"reader-ipad","baseRevision":1,"payload":{"sourceSHA256":"<该版本sha256>"}}
```

删除不接收kind、epub、pdf、body，基础修订号必须大于0；不接受空请求体的无条件删除。若代理丢弃DELETE请求体，将返回请求错误而非执行删除。未知批注404，不凭空生成墓碑；成功删除保留原锚点/笔记字节，记录deletedAt并递增revision，不删除来源文件或资源。

- 陈旧修订返回409 annotation_conflict，current含当前记录（可能已删除）；不存在记录但baseRevision非0时current为null。客户端保留本机分支，不自动提高修订号重传覆盖。
- 批注ID换版本/类型返回409 annotation_identity_mismatch；原书摘要不符返回409 source_version_mismatch。
- 已删除记录不允许PUT复活；旧修订仍冲突，使用其最新修订操作返回410 annotation_deleted。再次用新操作ID删除同一墓碑也返回410。恢复需后续显式功能或新建另一批注ID，当前没有自动恢复接口。
- 同一操作原请求重试返回原回执、replayed=true，不重复变更；改载荷、改目标或把进度操作ID用于批注返回409 mutation_id_reused（请求本身须先通过校验）。回执确认的是当时已提交结果，不能当成当前最新批注。

所有写入以一次事务保存批注、annotation事件和操作回执。变更event的entityID为批注ID，operation为upsert/delete，payload含完整批注及删除时间；客户端可据此处理离线删除，墓碑、事件和回执均不自动清理。不同版本、不同批注不会自动合并或搬迁。写入结果不明时保留原操作与本机内容，以原ID重试。

迁移007为annotations补充device_id及版本内分页索引；既有001–006迁移保持不变。未实际执行迁移，升级前保留完整状态目录备份；较旧程序会拒绝打开较新数据库。损坏记录保留并报错，不作为空列表或无批注处理。

代码阅读顺序：`internal/httpapi/annotations.go` → `internal/annotation/service.go` → `internal/storage/annotations.go`。本轮抽出position/syncvalue供进度和批注复用校验，不改变已定义的进度请求结构。

待部署者验收：三类批注创建/读取/修改、EPUB内容锚点与PDF矩形边界、旧revision并发、不同版本与类型隔离、更新和删除的重复请求、墓碑防复活、进度/批注操作ID复用拒绝、列表和混合事件分页，以及007迁移的数据保留。这些均未由本次开发执行。

## 便携手写资源与PDF笔画引用

以下为新增服务端代码，未运行构建、迁移或联调。支持PDF固定版本上的笔画组；EPUB自由手写、PencilKit转换、客户端渲染、擦除交互、编辑后稳定页映射均未实现；服务器侧工作PDF生成见下一节。资源及批注共用同一服务和Owner认证，不增加托管云或笔记旁路服务。

### 资源格式与上传

上传采用PUT `/api/v1/resources/{sha256}`，Content-Type必须为application/vnd.benfolio.ink+json。URL为原始请求体全部字节的64位小写SHA-256；服务端只为校验解析JSON，保存和下载均保留原始字节，不排序重写或修改笔画。JSON空白、键顺序或数字写法改变会得到另一摘要，重试必须保留原文件字节。资源不接受PKDrawing二进制、整份PDF、压缩包或任意附件。

```json
{
  "schemaVersion": 1,
  "documentVersionID": "<已经入库的PDF版本ID>",
  "sourceSHA256": "<该原件的sha256>",
  "pageIndex": 0,
  "coordinateSpace": "normalized-crop-unrotated-top-left",
  "strokes": [{
    "id": "stroke-001",
    "tool": "pen",
    "color": "#1A1A1AFF",
    "width": 0.002,
    "points": [{"x":0.1,"y":0.2,"pressure":0.6,"timeMS":0}, {"x":0.2,"y":0.3,"pressure":0.7,"timeMS":12}]
  }]
}
```

- 示例占位符须替换为真实版本/摘要后，对实际保存的UTF-8文件计算摘要再上传；不能对解析后重编码的另一份JSON计算摘要。原书摘要与笔画资源摘要是不同身份。
- schemaVersion固定为1；documentVersionID、sourceSHA256和pageIndex必须与已登记PDF版本相符。相同笔画跨原件版本也需显式生成新资源，不能自动套到新版。
- x/y为未旋转CropBox左上原点的0–1比例，分别相对页面宽/高；width相对页面宽，范围大于0且不超过0.05。所有数值须有限，出界数据拒绝，不自动裁切。
- tool只接受pen/highlighter；color为#RRGGBBAA，包括alpha；不改变原始大小写。pressure可省略，提供时为0–1；timeMS可省略，提供时为每笔内部的相对毫秒、0–86400000且不能倒退。当前不支持倾角、方位或其他私有笔刷字段。
- 每份资源1–1000笔，每笔至少1个点，累计最多20000个点，原始请求不超过2 MiB；拒绝重复笔画ID、重复JSON键、未知字段、无效类型和超过32层嵌套，不截断笔画。更大内容需由客户端拆成独立笔画组及批注。

服务先在有界内存中校验摘要、格式和版本，再在状态目录staging写入独立暂存文件并同步，以硬链接发布为只读 `resources/<摘要前两位>/<摘要>.ink.json`。已有文件重新校验，冲突返回409，不覆盖、不自动修复。资源、staging需位于支持硬链接和目录同步的同一受信任本机文件系统，两个目录拒绝显式符号链接，不支持跨挂载原子发布。

文件完整发布后，以一次数据库事务登记资源、版本/页元数据和Owner归属；新登记返回201，当前Owner已有相同登记返回200且reused=true。重复上传不产生笔记变更事件。数据库失败可能留下未引用的完整文件，强制中断也可能留下暂存；只尝试清理本次操作自己的暂存，不自动清理其他文件、旧资源或用户数据。

### 关联、修改与删除

资源上传成功后，用原有批注PUT接口提交kind=ink。pdf锚点只接收pageIndex（coordinateSpace可按既有规则省略），不带文字摘录或矩形；body仅含resourceSHA256。例如：

```json
{
  "id":"ink-save-001", "deviceID":"reader-ipad", "baseRevision":0,
  "payload": {
    "sourceSHA256":"<原书sha256>", "kind":"ink",
    "pdf":{"pageIndex":0},
    "body":{"resourceSHA256":"<已上传笔画资源的sha256>"}
  }
}
```

批注事务检查资源归当前Owner、原书版本、原件摘要和页索引是否一致；缺少可访问登记返回404，版本/页不符409。批注状态、资源关联、变更和操作回执在同一事务提交。登记检查不在该数据库事务内重读文件，依赖应用不改写/删除已发布资源；外部删改状态文件时资源下载会失败，不把笔画登记成功当作已完成工作PDF生成或端到端恢复。

编辑笔画组时先上传一份新资源，再以当前批注revision及新操作ID更新引用；旧文件及历史关联不删除。擦除后的非空笔画组同样生成新资源；清空整组通过批注DELETE软删除，不能上传空笔画组。不同批注分别修订，同一组并发编辑返回冲突，不执行逐点合并或笔画级CRDT。软删除和防复活规则沿用上一节，删除不回收资源。

变更流的ink批注payload只包含页面锚点与资源摘要；接收端应用修订并获取对应资源，实际下载、校验、解码和本机保存完成后才能报告笔画已可用。上传资源成功、提交批注成功、另一设备收到事件和工作PDF生成是不同阶段；客户端离线队列及同步完成状态尚未接入。

### 下载与存储边界

GET `/api/v1/resources/{sha256}`要求Owner Key及资源归属，未登记或非当前用户可访问的摘要404，不枚举磁盘孤立文件。发送前核对对象键、普通文件身份、大小与完整摘要；损坏返回409，缺失或不可读返回503。服务将已验证的有界字节缓冲用于本次发送，不在校验后重新打开文件。响应为原始JSON附件，含SHA-256 ETag，支持HEAD、单段Range和条件请求；多Range或超过256字节的Range头返回400，HEAD和304也先认证并校验。

上传/下载合计最多2个在途操作，含接收、校验、发布和发送，超出返回429及2秒重试提示。每次资源请求使用20秒上下文；内置HTTP读超时15秒、写超时30秒，代理可能采用更短期限，底层文件系统阻塞的取消仍是协作式。传输中断不追加JSON；客户端完成下载后须核对全部字节摘要，分段不能作为独立完整资源使用。

迁移008新增ink_resources和resource_owners，不修改001–007；已有未授权资源行不自动授予访问权。annotation_resources保留当前及历史引用，暂不提供资源删除、配额或垃圾回收。恢复须同时保留数据库、原件、资源及其他状态目录；仅备份笔记JSON不能恢复全部笔画。

代码阅读顺序：`internal/httpapi/resources.go` → `internal/resource/ink.go`与`internal/resource/service.go` → `internal/storage/resources.go`；资源引用继续走批注事务。待部署者验收：原字节摘要和格式边界、重复上传/并发、越界笔画、错误版本或页、用户归属、资源关联冲突与重试、下载/HEAD/Range、文件缺失损坏、008迁移和中断后重试。以上场景未由本次开发运行。

## 工作PDF生成与历史副本

本节为新增服务端代码，未运行构建、迁移、实际PDF生成或查看器验收。工作PDF由服务端从不可变原件和已提交批注重建，不接受外部整份PDF覆盖上传。每次读取同一Owner、原书版本、工作副本修订及批注变更位置，原件、可编辑笔画和旧工作文件均保留。

### 状态与显式生成

先GET `/api/v1/versions/{versionID}/working-pdf`。无副本时返回200、available=false、revision=0、stale=true；包含sourceSHA256、sourceByteSize及latestAnnotationSeq（字符串），不伪造文件。已有副本返回fileSHA256/byteSize/materializedChangeSeq、updatedAt、deviceID和annotationCount，并提供latestRevision。annotationCount统计写入PDF的源批注数，不计书签，一组多笔手写仍计一条。stale仅比较当前批注变更位置与该副本生成位置；只读状态不触发生成、不检测文件是否仍存在或完整。

POST同一路径，使用application/json，请求体上限8192字节，拒绝重复键、未知字段和缺失字段。例如：

```json
{
  "id":"working-build-001", "deviceID":"reader-ipad", "baseRevision":0,
  "sourceSHA256":"<该原件sha256>", "annotationSeq":"<状态接口的latestAnnotationSeq>"
}
```

baseRevision为当前工作副本revision，不是某条批注revision；annotationSeq必须是十进制字符串。两者及原件摘要须与一致性快照相符。服务端读取该版本所有有效批注，生成后在最终事务内再次检查修订和批注位置；期间新增、修改或删除批注会返回409 working_pdf_conflict，不替换当前副本，不悄悄把请求改成“基于最新数据”。

成功返回200，含workingPDF/changeSeq/replayed。工作副本修订递增，materializedChangeSeq记录本次批注快照的全局事件序号；它不是工作PDF自身事件的序号。只更新阅读进度不会使工作PDF过期。生成后新增批注将使状态stale=true，不自动覆盖用户正在使用的副本。

操作ID与进度、批注共用回执空间；原操作重试先查回执，不重复生成。更换目标、参数或复用其他操作的ID返回409 mutation_id_reused。旧回执确认当时提交结果，不代表当前最新状态或文件仍可读取；超时、断开或500后保留原请求，以原ID重试确认。当前只支持显式同步生成，不是后台队列任务。

### PDF内容与可编辑数据

- 每次从原件重建，不从上一份工作PDF继续叠加。NAS来源与归档原件不改写；来源PDF已有批注保留，删除本系统笔记不会删除来源原生批注。书签仍为NAS记录，不转换为PDF目录。
- 文字高亮写入Highlight及QuadPoints，文字备注写入Text便签和Contents；中文内容保存在PDF文字备注中，显示方式由查看器决定，不在页面上安装字体绘制整段备注。
- 每笔手写生成Ink/InkList及显式AP外观流，坐标从未旋转CropBox的左上比例空间转换到PDF用户坐标。支持页旋转的坐标约定，实际混合页型、裁边和不同查看器效果仍待验收。高亮及笔迹颜色、透明度通过外观流表示。
- 首版外观使用每笔固定线宽和圆头，单点生成圆点；压力和时间不改变这份PDF外观，但完整保留在原始笔画资源中。PDF第三方重新编辑/重建外观是否保留同样效果未验证，可编辑主数据仍是批注和便携资源，不能仅用下载的PDF替代完整备份。
- 有效批注全部处理，无法处理时整次失败，不只写入部分笔记后宣告完成。生成后重新校验PDF结构和页数，再发布文件；结构校验不等同于每页渲染、文本可见性或视觉保真已通过。

初始范围：未加密、无已识别签名/只追加限制的PDF，原件不超过32 MiB及1000页，每次最多1000条有效批注（含书签）、20000个生成的PDF批注对象。累计笔画资源不超过16 MiB、笔画不超过20000、点不超过200000，输出不超过128 MiB。页面需有有限有效MediaBox/CropBox，CropBox在MediaBox内、旋转为90度整数倍、宽高不超过20000用户单位；转换后线宽小于0.01或矩形宽高小于0.000001用户单位暂拒绝。超过范围返回422，不截断、删除或覆盖原件和旧工作副本。原书阅读/下载原有范围不受此生成限制影响。

PDF使用固定pdfcpu v0.16.3的独立离线配置，带已有对象/流/递归资源限制，不加载用户字体或外部资源；仅重写派生文件。加入新外观时将有效PDF版本至少提升至1.7。常规错误或意外解析/写入panic均不更新工作副本，进程内存耗尽等致命运行错误不属于可恢复异常；资源限制和容器设置仍需NAS实测。

### 登记、变更与下载

输出先写入独立staging文件，重新校验、计算摘要并同步后以不覆盖硬链接发布为 `working/<摘要前两位>/<摘要>.pdf`。文件已存在时核对完整摘要与大小。随后以一次事务登记PDF资源及Owner、历史版本、当前副本、working_pdf事件和操作回执。数据库失败或批注冲突可留下未引用的完整文件，不自动清理；本次暂存尽力删除，不删除其他用户工作。

工作文件不通过仅接收笔画JSON的resources端点下载。GET `/api/v1/versions/{versionID}/working-pdf/file`读取当前副本；可用 `?revision=1`读取保留的历史修订。状态接口同样支持revision，历史副本的stale仍按当前批注位置计算。未知历史修订或未生成文件404，磁盘缺失503，完整性异常409；不回退成原件或其他版本。

下载需Owner Key，发送前校验对象键、普通文件身份、大小和完整摘要。响应application/pdf附件、SHA-256 ETag，支持HEAD、单段Range和条件请求；提供X-Benfolio-Working-Revision、X-Benfolio-Materialized-Seq、X-Benfolio-Working-Stale。状态和stale是读取时的快照，之后笔记仍可能更新；下载方必须核对完整字节摘要，不将旧副本当作覆盖上传入口。

同时最多1个生成和2个工作PDF下载。生成使用90秒上下文、内置响应写期限100秒；下载总上下文与内置响应写期限5分钟，代理可能更短，文件系统及解析取消仍为协作式。超时不等于未提交，重试遵循操作回执。下载期间外部进程改写状态文件仍可能影响流，部署须限制工作目录写权限；应用不会原地改写已发布文件。

迁移009新增working_pdf_history及按用户/版本/类型查变更位置的索引，001–008保持不变；原有pdf_working_copies作当前指针，resources登记派生PDF，旧文件和历史均不自动清理。working_pdf事件只含副本元数据，客户端需按版本/修订下载文件；App尚未接入。pdfWorkingCopy/pdfAnnotationMaterialization为服务端能力，独立导出已另有PDF/Markdown记录和下载接口，不代表App分享界面或完整可编辑备份已实现。

代码阅读顺序：`internal/httpapi/working_pdf.go` → `internal/workingpdf/service.go`与`render.go` → `internal/storage/working_pdf.go`。待部署者验收：无批注原件副本、中文备注/高亮/单点及连续笔画、旋转与裁边、来源原生批注保留、删除后的重新生成、混合事件/旧副本过期、生成期间批注变化、重复操作、文件及资源损坏、历史下载/Range，以及009迁移与中断恢复。所有这些仍待实际执行。

## 独立PDF和Markdown导出

本节为新增服务端代码，未构建、执行迁移、生成真实导出文件或做外部查看器验收。导出记录及文件仅当前Owner可访问，不创建匿名分享链接，也不修改原书、笔画资源或已有工作副本。当前格式为pdf/markdown；不把Markdown称为带批注EPUB或完整恢复备份。

### 创建与请求快照

先GET `/api/v1/versions/{versionID}/export-context`，读取sourceSHA256、bookFormat、latestAnnotationSeq、activeAnnotationCount和latestWorkingRevision；这是只读元数据快照，不生成文件或验证磁盘文件。创建使用POST `/api/v1/versions/{versionID}/exports`，application/json、上限8192字节，拒绝重复键、未知字段及无效类型。

Markdown请求示例（替换摘要和序号占位符）：

```json
{"id":"notes-export-001","deviceID":"reader-ipad","format":"markdown","sourceSHA256":"<原件sha256>","annotationSeq":"<latestAnnotationSeq>"}
```

PDF请求另须显式提供正整数workingRevision；annotationSeq须使用这一修订的materializedChangeSeq，不是工作PDF自身事件序号或当前进度修订：

```json
{"id":"pdf-export-001","deviceID":"reader-ipad","format":"pdf","sourceSHA256":"<原件sha256>","annotationSeq":"<选定副本的materializedChangeSeq>","workingRevision":1}
```

- id同时作为本次操作ID与导出ID，同一Owner内不可变；与进度/批注/工作PDF操作共用回执ID空间。deviceID和id使用既有1–128个ASCII字母、数字、短横线或下划线规则。
- Markdown使用一致性事务读取该原件版本的标题、作者、全部有效批注和变更位置，最终登记时再次检查原件摘要与批注序号。期间笔记变化返回409 export_snapshot_conflict，不混合新旧内容，不截断笔记；新快照用新操作ID明确请求。
- PDF必须已有所选工作修订，缺少则404，不隐式生成、不回退成无批注原件。可以显式选择保留的旧修订；记录的annotationSeq固定到这一副本，staleAtCreation表明创建当时是否已有更新的批注。之后笔记更新不改写已发布导出。
- 创建完成返回201，含export/replayed；相同ID和相同规范化请求重试返回原记录、200及replayed=true，不重新发布。相同ID换参数/目标或复用其他操作ID返回409 mutation_id_reused。超时、断开或500时创建结果可能未知，保留原请求并以原ID确认。

当前为显式同步创建，不返回虚构的排队/后台成功状态。PDF请求固定修订后只校验并复用对应文件，不再次重写PDF。Markdown不接受非零workingRevision；其快照与PDF工作修订互相独立。

### Markdown内容

汇总包含该版本书名/作者、格式、原件版本/摘要、批注快照、导出时间，以及批注ID/修订/时间、位置、摘录和文字备注。EPUB使用内容href/CFI，PDF使用原件页索引及一基阅读提示，不冒充文档印刷页码。只导出NAS管理的有效批注，不从来源文件提取已有原生批注或执行OCR，也不包含删除墓碑、阅读进度或整份定位JSON。

手写只列页面和资源摘要，不把笔画字节嵌入Markdown；原书、笔画资源及可编辑状态须另行保留。文件开头明确注明不具备完整恢复能力。用户标题、摘录和备注按字面文字转义HTML及Markdown控制符，不生成远程图片、脚本或可执行嵌入。无有效批注时明确写出空快照。

每次最多1000条有效批注，生成文件最多16 MiB，超出返回422 export_limit并拒绝整次发布，不截断。PDF导出沿用已生成副本的范围；annotationCount在Markdown中计全部有效批注，在PDF中沿用工作副本写入的源批注数（不计书签）。带批注EPUB书籍副本和包含原件/资源的可编辑导出包仍待实现。

### 记录、文件与下载

GET `/api/v1/exports/{exportID}`读取固定记录，GET `/api/v1/exports/{exportID}/file`下载。记录含seq（字符串）、documentVersionID/sourceSHA256、书籍格式和导出格式、fileSHA256/byteSize、annotationSeq、可选workingRevision、annotationCount、staleAtCreation、deviceID和createdAt。staleAtCreation仅描述创建时，不保证以后仍为最新；元数据读取不检测文件存在性。

Markdown先发布只读 `exports/<摘要前两位>/<摘要>.md`，完整校验/同步后登记；PDF记录引用选定工作文件，不复制或覆盖字节。资源归属、不可变导出记录/发布序号和操作回执同事务提交。export_records的追加行同时作为导出发布事件，其seq是独立命名空间，不是changes游标；导出不进入阅读变更流，不使笔记或工作PDF过期。

下载核对当前Owner、记录绑定及文件身份/大小/完整SHA-256，缺失503、摘要或记录冲突409、无记录或其他Owner记录404，不回退到原件或最新副本。响应PDF或UTF-8 Markdown附件，含SHA-256 ETag，支持HEAD、单段Range和条件请求，提供X-Benfolio-Export-Seq与X-Benfolio-Annotation-Seq。文件名使用摘要，不把私有宿主路径放入记录或下载头。

最多1个创建和2个导出下载，PDF还受工作PDF下载并发限制；超出429及2秒重试提示。创建30秒上下文、内置写期限40秒，下载上下文及内置写期限5分钟，代理可能更短；I/O取消仍为协作式。大文件校验和格式兼容性仍待NAS实测。旧记录、文件、历史工作副本及孤立完整文件不自动清理；只尽力移除本次未发布暂存，现阶段没有导出删除/过期任务。

迁移010新增export_records及索引，001–009保持不变；新增状态子目录exports，和staging须在支持硬链接/目录同步的同一本机文件系统，拒绝目录显式符号链接。尚未运行迁移或部署。pdfExport、notesMarkdownExport、epubNotesExport表示对应服务端实现接线；epubAnnotationExport及backupRestore仍false，App导出/分享界面未接入。

代码阅读顺序：`internal/httpapi/exports.go` → `internal/export/service.go`、`markdown.go`和`files.go` → `internal/storage/exports.go`。待部署者验收：EPUB/PDF汇总与字面转义、空快照、批注变化冲突、旧工作修订固定、重试/ID冲突、跨Owner访问、损坏/缺失文件、HEAD/Range，以及010迁移与中断重试；这些均未由本次开发执行。

## NAS 部署准备

Compose 构建上下文为本仓库根目录。暂无预构建镜像，从源码构建的配置步骤如下：

1. 将 deploy/.env.example 复制为 deploy/.env，填写独立状态目录、书籍目录、Key 文件的绝对路径。
2. 提前创建这些目录及 Key 文件。选择具备状态目录写权限、来源目录和凭证读权限的 UID/GID；不能只凭管理员组身份推定有写权限，也不扩大到 everyone。
3. 默认绑定宿主机127.0.0.1。局域网访问时明确设置绑定地址或反向代理，设置对应的 TLS 入口。
4. 配置完成后执行：

```sh
docker compose --env-file deploy/.env -f deploy/compose.yaml up -d --build
```

容器根文件系统只读，状态目录可写、书籍来源只读；bind 路径不存在时配置要求失败，不自动创建宿主目录。开发阶段使用独立状态目录。尚未实现在线备份和恢复，停服备份需要保留完整状态目录。

## 开发路线

来源配置、书库登记、受限预览、文件发现任务、单本及后台批量 EPUB/PDF 解析入库、版本查询与原件下载代码已加入，待构建与运行验收。服务端版本进度、文字/手写引用批注、笔画资源、删除墓碑、工作PDF生成/历史下载与增量变更代码已加入，仍待构建、迁移与运行验收。接下来：第一方客户端进度/批注/笔画转换及跨设备接续 → 带批注EPUB副本 → 管理与完整备份恢复。

NAS资源占用、多架构兼容性、多设备同步与恢复仍待验证。

## 许可证

Copyright (C) 2026 Bennett Li.

本项目采用 [GNU Affero General Public License v3.0](LICENSE)（SPDX：`AGPL-3.0-only`）。允许使用、修改和商用；分发本项目或其衍生作品须遵守许可证的源码提供要求。修改后的程序通过网络与用户交互时，须按许可证第 13 条向这些用户提供对应源码。

本许可证适用于本服务端仓库；独立阅读器客户端有各自的许可安排。第三方依赖遵循各自许可证。

PDF 解析依赖 pdfcpu（Apache-2.0），相关许可与附带声明保留于 third_party/pdfcpu/；Dockerfile 将本项目、Go 及依赖模块可找到的许可声明复制到镜像 /licenses/，镜像构建与许可打包仍待验证。
