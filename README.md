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
- 独立 Dockerfile、Compose 和配置示例。

进度/笔记同步、笔迹转换、工作 PDF 保存、EPUB/PDF 导出、管理界面、备份恢复均未实现。新数据库未配置来源时书库查询返回空列表；配置有效来源后返回登记的书库，单本或批量 EPUB/PDF 导入成功后才有书目。下载针对已入库的原件，不是批注导出。未实现的业务接口返回 `501 not_implemented`。能力声明中相应项目为 `false`。Apple 客户端尚未接入此服务。

## 工程结构

```text
cmd/benfolio-server/       启动、退出及容器健康探测入口
internal/config/          环境配置和凭证文件读取
internal/domain/          书库、版本、锚点、笔画与同步契约草案
internal/httpapi/         HTTP 路由、认证、参数和错误返回
internal/source/          受限的来源目录读取与文件发现
internal/scan/            后台扫描任务执行与状态收尾
internal/publication/     受限的 EPUB/PDF 元数据读取
internal/importer/        来源快照、原书归档与导入流程
internal/originals/       归档读取、完整摘要校验与下载并发限制
internal/localfile/       Linux/macOS 普通文件的受限打开
internal/storage/         状态目录、SQLite、书目查询与导入事务
internal/storage/migrations/  内嵌迁移
deploy/                   Compose 和环境变量示例
```

运行状态目录结构（当前仅导入流程写入原书归档与暂存）：

```text
data/
  benfolio.sqlite         元数据及后续同步记录
  originals/              不可变原书归档
  working/                按用户/内容版本管理的工作文件
  resources/              笔迹和其他不可变资源
  staging/                尚未发布完成的暂存
```

数据库使用 WAL；仅复制主数据库文件不能作为完整备份。当前按单实例、本机持久磁盘设计，不承诺多副本或网络文件系统上的共享 SQLite。来源书籍目录独立挂载为只读；启动检查最多读取一个条目名称。只读文件发现由请求或后台任务触发，受限递归列举目录和文件元数据，不读取书籍正文；显式导入读取正文并归档到独立状态目录。来源文件均不改写。

## 保存与同步边界

以下是拟实现契约，不是已支持能力：

- **PDF**：保留导入原件；批注写入每个用户自己的工作 PDF，分享文件携带可见笔迹；NAS 同时保存可迁移笔画和同步记录。工作 PDF 的字节摘要与原书内容摘要分开，不能因画了一笔就把原书认作换版。写回来源路径需要后续明确的发布操作，当前只读挂载不支持写回。
- **EPUB**：原件保留；日常按内容锚点同步高亮、书签和文字备注；导出带批注 EPUB 副本及笔记汇总。其他阅读器是否识别内嵌批注单独验证。自由手写先后置，不绑定可重排的屏幕页码。
- **多端**：稳定的实例/用户/内容版本身份；后续以服务端修订号与操作 ID 处理条件写入、重试和删除。不同对象允许合并，同对象并发修改保留冲突。当前只有模型与表，尚无实际同步处理。
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

书目查询当前只按标题匹配；`format` 为 epub/pdf，`limit` 默认30、上限100，`offset` 最大1000000。返回 `items/limit/offset/hasMore`；已导入书目含 `currentVersionID`。接口和模型仍处于草案阶段。

progress、annotations、working-pdf、exports、changes、resources 等路由仅作预留并返回501；尚不支持客户端上传书籍和同步。不要因路由存在而开启客户端相应入口。

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

新建版本返回201，同内容重试返回200；响应包含 `bookID/version/metadata/bookCreated/versionCreated`。来源访问占用时返回429，格式无效/不支持或超出范围返回422，读取失败返回503；未知书库404、未绑定来源409。请求采用20秒上下文期限，底层I/O阻塞时不能保证期限；客户端超时或断开可能无法确认已完成的事务，可重试同一路径与内容复用版本。导入不产生同步事件；`epubImport/pdfImport` 与独立的 `download` 能力分开声明，不开启 `progressSync/annotationSync`。

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

来源配置、书库登记、受限预览、文件发现任务、单本及后台批量 EPUB/PDF 解析入库、版本查询与原件下载代码已加入，待构建与运行验收。接下来：第一方客户端接入与进度接续 → 文字批注及中立笔画同步 → 工作 PDF、导出、管理与备份恢复。

NAS资源占用、多架构兼容性、多设备同步与恢复仍待验证。

## 许可证

Copyright (C) 2026 Bennett Li.

本项目采用 [GNU Affero General Public License v3.0](LICENSE)（SPDX：`AGPL-3.0-only`）。允许使用、修改和商用；分发本项目或其衍生作品须遵守许可证的源码提供要求。修改后的程序通过网络与用户交互时，须按许可证第 13 条向这些用户提供对应源码。

本许可证适用于本服务端仓库；独立阅读器客户端有各自的许可安排。第三方依赖遵循各自许可证。

PDF 解析依赖 pdfcpu（Apache-2.0），相关许可与附带声明保留于 third_party/pdfcpu/；Dockerfile 将本项目、Go 及依赖模块可找到的许可声明复制到镜像 /licenses/，镜像构建与许可打包仍待验证。
