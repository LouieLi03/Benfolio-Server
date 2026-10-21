# Benfolio Server

供用户自行部署到 NAS 的开源阅读服务端。书库、书籍版本、阅读进度和笔记属于同一套产品，不依赖运营方提供托管云。与 Benfolio 阅读器分别维护，本仓库不包含 Apple 客户端源码。采用 AGPL-3.0-only 许可证，暂无发行镜像。

## 当前状态

项目处于框架阶段，尚未完成构建与运行验证，暂不提供可用发行版。目前包含以下代码与配置：

- Go 启动入口、环境配置、结构化日志和退出处理。
- 单 Owner 的 Bearer API Key 认证；支持从独立文件读取凭证。
- SQLite 初始化、事务迁移、迁移摘要检查和较新数据库降级拒绝。
- 持久化服务实例 ID 与 Owner ID；更换 Key 不另建用户。
- 健康状态、服务信息、能力声明、书库/书目查询接口。
- 原书版本、页面、进度、批注、资源、工作 PDF、变更日志、操作回执和任务的数据结构。
- 独立 Dockerfile、Compose 和配置示例。

扫描入库、下载、进度/笔记同步、笔迹转换、工作 PDF 保存、EPUB/PDF 导出、管理界面、备份恢复均未实现。新数据库的书库查询返回真实空列表；未实现的业务接口返回 `501 not_implemented`。能力声明中相应项目为 `false`。Apple 客户端尚未接入此服务。

## 工程结构

```text
cmd/benfolio-server/       启动、退出及容器健康探测入口
internal/config/          环境配置和凭证文件读取
internal/domain/          书库、版本、锚点、笔画与同步契约草案
internal/httpapi/         HTTP 路由、认证、参数和错误返回
internal/storage/         状态目录、SQLite、只读书库查询
internal/storage/migrations/  内嵌迁移
deploy/                   Compose 和环境变量示例
```

运行状态目录结构（业务文件写入尚未实现）：

```text
data/
  benfolio.sqlite         元数据及后续同步记录
  originals/              不可变原书归档
  working/                按用户/内容版本管理的工作文件
  resources/              笔迹和其他不可变资源
  staging/                尚未发布完成的暂存
```

数据库使用 WAL；仅复制主数据库文件不能作为完整备份。当前按单实例、本机持久磁盘设计，不承诺多副本或网络文件系统上的共享 SQLite。来源书籍目录独立挂载为只读；骨架不读取、创建、扫描或改写该目录。

## 保存与同步边界

以下是拟实现契约，不是已支持能力：

- **PDF**：保留导入原件；批注写入每个用户自己的工作 PDF，分享文件携带可见笔迹；NAS 同时保存可迁移笔画和同步记录。工作 PDF 的字节摘要与原书内容摘要分开，不能因画了一笔就把原书认作换版。写回来源路径需要后续明确的发布操作，当前只读挂载不支持写回。
- **EPUB**：原件保留；日常按内容锚点同步高亮、书签和文字备注；导出带批注 EPUB 副本及笔记汇总。其他阅读器是否识别内嵌批注单独验证。自由手写先后置，不绑定可重排的屏幕页码。
- **多端**：稳定的实例/用户/内容版本身份；后续以服务端修订号与操作 ID 处理条件写入、重试和删除。不同对象允许合并，同对象并发修改保留冲突。当前只有模型与表，尚无实际同步处理。
- **恢复**：原件、工作文件、数据库及引用资源都属于恢复范围；尚未上传的设备离线修改不能由 NAS 恢复。当前不删除旧版和墓碑，不提供备份/恢复 API。

## 本机开发

需要 Go 1.26 或更新版本。依赖版本记录在 go.mod/go.sum；SQLite 驱动为 modernc.org/sqlite，容器配置使用 `CGO_ENABLED=0`。

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
| `BENFOLIO_LISTEN_ADDR` | 默认 `:8080` |
| `BENFOLIO_DATA_DIR` | 持久状态目录，运行用户必须具备真实读写权限 |
| `BENFOLIO_BOOKS_DIR` | 预留书籍来源目录；当前仅记录是否配置，不代表来源已可达或已入库 |
| `BENFOLIO_API_KEY_FILE` | 推荐：独立文件，内容为 32–512 字节、无空白的 Key |
| `BENFOLIO_API_KEY` | 环境变量替代方式，不能与文件配置同时提供 |

当前 Key 只在启动时载入，认证为单 Owner，不支持注册、密码登录、多用户授权或自动 Key 轮换。外部访问的 HTTPS 由部署入口配置，服务当前提供 HTTP。

## 当前接口

健康接口以外均要求 `Authorization: Bearer <API Key>`；当前根路径不提供管理网页。

| 方法与路径 | 当前行为 |
|---|---|
| `GET /health/live` | 进程响应 |
| `GET /health/ready` | 数据库连接可用；不代表书籍来源或同步已就绪 |
| `GET /api/v1/server` | 实例 ID、版本、数据库版本、`stage=scaffold` |
| `GET /api/v1/me` | 当前 Owner 的稳定 ID 与显示名 |
| `GET /api/v1/capabilities` | 实际能力开关 |
| `GET /api/v1/libraries` | 数据库中的书库列表，未配置时为空 |
| `GET /api/v1/books` | 支持 query、format、limit、offset 的书目查询 |

书目查询当前只按标题匹配；`format` 为 epub/pdf，`limit` 默认30、上限100，`offset` 最大1000000。返回 `items/limit/offset/hasMore`。接口和模型仍处于草案阶段。

扫描、书目详情/版本、版本文件下载、progress、annotations、working-pdf、exports、changes、resources 等路由仅作预留并返回501；没有执行上传、生成文件或同步。不要因路由存在而开启客户端相应入口。

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

开发顺序：来源配置与扫描/元数据/不可变版本下载 → 第一方客户端进度接续 → 文字批注及中立笔画同步 → 工作 PDF、导出、管理与备份恢复。

NAS资源占用、多架构兼容性、多设备同步与恢复仍待验证。

## 许可证

Copyright (C) 2026 Bennett Li.

本项目采用 [GNU Affero General Public License v3.0](LICENSE)（SPDX：`AGPL-3.0-only`）。允许使用、修改和商用；分发本项目或其衍生作品须遵守许可证的源码提供要求。修改后的程序通过网络与用户交互时，须按许可证第 13 条向这些用户提供对应源码。

本许可证适用于本服务端仓库；独立阅读器客户端有各自的许可安排。第三方依赖遵循各自许可证。
