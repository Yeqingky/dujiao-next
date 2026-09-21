# Dujiao-Next

Dujiao-Next 是一个数字商品电商平台。本仓库包含完整应用，包括 Go 后端、用户商城和管理后台。

[English version](README.en.md)

## 技术栈

| 层 | 技术栈 |
| --- | --- |
| 后端 | Go 1.26 · Gin · GORM · SQLite / PostgreSQL |
| 鉴权 | JWT（管理员 / 用户独立 realm）· Casbin RBAC · TOTP 双因素认证 |
| 异步任务 | 基于 Redis 的 asynq（可选，服务器不依赖它也能运行） |
| 配置 | Viper（`config.yml`） |
| 前端 | Vue 3 · Vite · TypeScript · Tailwind CSS v4 · pnpm 10 |
| 管理后台 UI | shadcn-vue / reka-ui |

## 仓库结构

```text
.
├── cmd/server/               # 入口，同时提供 `admin` 管理员操作子命令
├── internal/
│   ├── app/                  # 组合根
│   │   ├── container/        # 依赖注入容器
│   │   ├── httpserver/       # Gin 路由、路由组和中间件
│   │   └── jobs/             # asynq worker 服务和消费者
│   ├── bootstrap/            # 各模块的装配（adapters.go + wiring.go）
│   ├── modules/              # 35 个业务模块，每个领域一个垂直切片
│   ├── workflows/            # 跨多个模块的用例
│   ├── platform/             # 面向框架的基础设施
│   │   ├── database/gormdb/  # 连接和自动迁移
│   │   └── http/             # 响应封装和 Gin 辅助函数
│   ├── shared/               # 无依赖的基础类型（money、jsonmap、serial 等）
│   ├── authz/                # Casbin RBAC：策略模型和内置角色种子
│   ├── web/                  # SPA 嵌入和挂载（由 build tag 控制）
│   ├── architecture/         # 架构守卫测试，不包含生产代码
│   ├── cache/ config/ constants/ crypto/ i18n/ logger/ queue/ version/
│   └── admincmd/ htmltext/ persistence/ telegramidentity/ testkit/ upstream/
├── frontend/
│   ├── admin/                # 管理后台 SPA（开发端口 :5174）
│   └── user/                 # 用户商城 SPA（开发端口 :5173）
├── config.yml.example
├── Dockerfile                # 单体全栈镜像
└── .goreleaser.yaml
```

首次启动时会创建运行时目录：`db/`（SQLite）、`uploads/` 和 `logs/`。

## 架构

本项目采用模块化单体架构。`internal/modules/<name>/` 下的每个领域都是一个垂直切片，包含自己的分层结构：

| 层 | 内容 | 可以导入 |
| --- | --- | --- |
| `domain/` | 实体、值对象和业务不变量 | 其他层均不可导入 |
| `application/` | 用例和端口接口 | `domain`、`contract` |
| `infrastructure/` | GORM 存储、网关和队列适配器 | `domain`、`application` 的端口 |
| `transport/` | HTTP 处理器和 presenter | `application` 层接口 |
| `contract/` | application 层依赖的端口接口，以及模块对外公开的接口 | — |

**这些规则由测试强制执行，而不是仅靠约定。**`internal/architecture/` 会解析整个代码树中的导入关系，并在发现违规时让构建失败。主要规则如下：

- `domain` 不得依赖 `application`、`infrastructure` 或 `transport`
- `application` 不得导入 Gin 或 asynq，用例中不得使用传输层库
- 只有模块的 `infrastructure/gormstore` 适配器可以导入 GORM
- `transport` 依赖 application 层接口，不得依赖具体存储实现
- `internal/shared` 不得依赖业务模块、GORM、Gin 或 asynq
- `internal/platform` 不得依赖业务模块

运行完整测试套件时会一并运行架构测试：`go test ./internal/architecture/...`

模块之间不得导入彼此的内部实现，而应通过 `contract/` 通信；装配代码位于 `internal/bootstrap/<module>/`。

### RBAC

每个 `/api/v1/admin/...` 路由都会经过 Casbin。权限目录由运行中的路由表生成，但**内置角色由人工维护**，位置是 `internal/authz/bootstrap.go`。新增管理路由时，如果没有将其添加到角色种子中，该路由将只能由超级管理员访问。`internal/app/httpserver/rbac_coverage_test.go` 会检查每个已注册路由是否都受到覆盖。

## 构建标签

| Tag | 作用 |
| --- | --- |
| *(none)* | 仅 API，不挂载 SPA，是本地开发默认模式 |
| `fullstack` | 通过 `go:embed` 将 `internal/web/dist/{admin,user}` 嵌入二进制 |
| `release` | 用于构建出站 URL 的生产环境行为 |

`go:embed all:dist/admin all:dist/user` 要求两个目录都存在，因此必须先构建前端，否则 `fullstack` 构建会直接失败。普通 `go build` 不会编译 `embed_fullstack.go`；修改 `internal/web/` 后，请使用 `go build -tags release,fullstack ./cmd/server` 验证。

## 运行模式

```bash
./dujiao-next                 # all    — HTTP 服务 + 后台 worker（默认）
./dujiao-next -mode api       # 仅 HTTP 服务
./dujiao-next -mode worker    # 仅后台 worker
```

管理员子命令包含在同一个二进制文件中，因此容器无需额外工具：

```bash
./dujiao-next admin list-admins
./dujiao-next admin reset-password
./dujiao-next admin reset-2fa
```

## 前端说明

两个 SPA 相互独立，发布时会构建并嵌入二进制文件。

**挂载点。** 用户商城在 `/` 提供服务，管理后台位于 `web.admin_path`（默认 `/admin`）。`/api`、`/uploads` 和 `/health` 是保留前缀；这些前缀下未匹配的路径会返回 404，而不会回退到 SPA 外壳。如果新增顶层后端前缀，需要同步更新 `internal/web/handler.go` 中的 `reservedPaths`。

**管理后台基础路径在运行时解析，而不是构建时解析。**由于 `web.admin_path` 可配置，`pnpm run build:fullstack` 只会注入 `<base href="__DJ_ADMIN_BASE__/">` 占位符，服务器启动时再进行重写。管理后台代码需要注意：

- 原生 `<a href>` 和 `window.location` 导航必须使用 `src/utils/adminBase.ts` 中的 `adminUrl()`
- `<router-link :to>` 和 `router.push()` 不应使用 `adminUrl()`；vue-router 已经携带基础路径，重复添加会产生 `/admin/admin/...`

**用户商城模板。**用户前端提供多个外观，由站点设置 `storefront_template`（`classic`、`vault`）选择。模板页面位于 `src/templates/<name>/`；如果某个页面没有模板专属版本，则回退到 `src/views/`。详见 `src/templates/registry.ts`。本地预览时可追加 `?template=vault`。

**i18n。**两个前端和所有 API 响应都支持本地化，包括简体中文、繁体中文和英语。任何一侧都不得硬编码面向用户的文本。

## 快速开始（部署）

### 官方一键安装器（Ubuntu / Debian）

在全新的 Ubuntu 22.04+ 或 Debian 12+ 服务器上，下载并运行官方交互式安装器：

```bash
curl -fsSL https://raw.githubusercontent.com/dujiao-next/dujiao-next/main/scripts/dujiao-next-manager.sh \
  -o /tmp/dujiao-next-manager.sh
sudo bash /tmp/dujiao-next-manager.sh install
```

安装器会通过 systemd 部署发布版二进制文件、隔离的本地 Redis、Nginx、SQLite 和 Let's Encrypt 证书。安装后可以重新打开管理菜单：

```bash
sudo dujiao-next-manager
```

也支持以下适合自动化的命令：

```bash
sudo dujiao-next-manager status
sudo dujiao-next-manager logs app
sudo dujiao-next-manager restart
sudo dujiao-next-manager configure-domain
sudo dujiao-next-manager configure-admin-path
sudo dujiao-next-manager renew-cert
sudo dujiao-next-manager admin-reset-password
sudo dujiao-next-manager admin-reset-2fa
sudo dujiao-next-manager uninstall
```

首个版本仅支持 Ubuntu/Debian 上的单个非通配符域名，不会接管已有的手动安装。如果跳过 SMTP，需要先在管理面板中配置 SMTP，再启用邮箱验证注册。应用数据位于 `/opt/dujiao-next`，安装器状态存储在 `/etc/dujiao-next/install-state.json`。TLS 失败时只会启用 ACME challenge endpoint；修复 DNS 或防火墙后可以重新运行 `install`。安全卸载会在删除受管数据前，于 `/var/backups/dujiao-next` 创建并验证权限为 `0600` 的恢复归档。

### 手动安装二进制文件

从 [Releases](https://github.com/dujiao-next/dujiao-next/releases) 下载最新的 `dujiao-next_*.tar.gz`：

```bash
tar -xzf dujiao-next_*.tar.gz
cp config.yml.example config.yml
# 编辑 config.yml：设置 jwt.secret、user_jwt.secret 和 web.admin_path
# config.yml 只保留启动阶段必须的配置；邮箱 SMTP、验证码、Telegram/Google 登录、订单设置、上游同步间隔改在后台「系统设置」中配置
./dujiao-next
```

完整说明见：https://dujiao-next.com/deploy/

也可以使用 Docker：

```bash
docker run -d -p 8080:8080 -v $PWD/config.yml:/app/config.yml:ro ghcr.io/Yeqingky/dujiao-next:latest
```

发布镜像发布到 GitHub Container Registry，提供多架构 `linux/amd64` 和 `linux/arm64` 镜像。可用标签包括发布标签、语义化版本和 `latest`。

### 分支 Docker 镜像

每次推送分支都会构建并发布多架构镜像，标签由分支名和 7 位 commit hash 组成。例如：

```text
ghcr.io/Yeqingky/dujiao-next:dev-cap-captcha-13876af
```

镜像标签中的分支斜杠会替换为连字符。

### Docker Compose

根目录的 `docker-compose.yml` 会从 GHCR 拉取应用镜像，并启动 PostgreSQL、Redis、API 以及两个嵌入式 SPA：

```bash
cp .env.example .env
docker compose pull
docker compose up -d
docker compose ps
docker compose logs -f dujiao-next
```

在 `.env` 中设置 `TAG` 可以选择其他已发布的 GHCR 标签，默认值为 `latest`。

如果要基于当前工作副本的 `Dockerfile` 构建本地源码，请使用 `docker-compose-dev.yml`：

```bash
docker compose -f docker-compose-dev.yml up -d --build
```

应用监听 `127.0.0.1:${APP_PORT}`。运行时凭据从 `.env` 读取。开发 Compose 文件从 `/opt/dujiao-next/config/config.yml` 读取应用配置，并将持久化文件存储在 `/opt/dujiao-next/data/` 下。这些路径包含敏感信息或运行时数据，会被有意排除在 Git 和 Docker 构建上下文之外。使用 `docker compose down` 停止服务；绑定挂载的数据仍保留在 `/opt/dujiao-next/data/` 下。

## 快速开始（开发）

分别运行后端和两个前端，以启用热重载：

```bash
go mod tidy && go run ./cmd/server   # :8080 — 仅 API，不挂载 SPA

cd frontend/user  && pnpm install && pnpm run dev   # :5173
cd frontend/admin && pnpm install && pnpm run dev   # :5174
```

两个开发服务器都会将 `/api`、`/uploads`、`/sitemap.xml` 和 `/robots.txt` 代理到 `localhost:8080`。生产环境中所有内容使用同源，因此这些代理只用于开发。

## 通用 OIDC 登录

项目支持配置一个全局 OIDC Provider，用于用户登录和账号绑定。后台设置位于 **通用 OIDC 登录**，支持以下模式：

- `PKCE + none`：公共客户端，默认使用 PKCE；
- `PKCE + client_secret_basic`：服务端 Web 应用推荐模式；
- 关闭 PKCE 并使用 `client_secret_basic`：兼容不支持 PKCE 的旧 Provider；
- `client_secret_post`：仅用于特殊兼容场景，不建议默认使用。

配置必须包含 Issuer URL、Client ID、回调地址和 `openid` Scope。服务端会通过 OIDC Discovery 获取授权端点、令牌端点和 JWKS，并校验 `state`、`nonce`、issuer、audience、签名和过期时间。Microsoft Entra ID 建议使用租户专属 Issuer，例如：

```text
https://login.microsoftonline.com/<tenant-id>/v2.0
```

回调地址必须精确登记为：

```text
https://你的商城域名/auth/oidc/callback
```

外部身份使用 `iss + sub` 标识。未验证的邮箱不会自动关联已有本地账号。

## 可选的 Cap CAPTCHA

Cap 作为独立服务部署。请按照 [Cap Standalone 指南](https://trycap.dev/guide/standalone/) 运行带有 Valkey/Redis 的 Cap 容器，通过 HTTPS 域名对外提供服务，并创建 site key。配置 Cap 的 `CORS_ORIGIN`，允许用户商城和管理后台的来源访问。

在 Dujiao-Next 中打开 **Settings -> CAPTCHA**，选择 `Cap`，然后配置：

- **Server endpoint**：公开的 Cap 基础 URL，例如 `https://cap.example.com`；
- **Site Key**：在 Cap 控制台创建的公开 key；
- **Site Secret Key**：site key secret，不是 Cap 控制台的 `ADMIN_KEY`。

浏览器使用 `https://cap.example.com/<site-key>/`；Dujiao-Next 会通过 Cap 的 `/siteverify` endpoint 在服务端验证返回的 token。secret 不会包含在公开配置中。这些值只保存在数据库设置里，`config.yml` 不再提供 `captcha.cap`，请勿在两个位置重复填写。

> 使用 corepack 提供的 `pnpm`。`pnpm --dir X` 不会读取目标目录的 `packageManager` 字段，可能选择错误的版本，因此请先 `cd` 到对应 package 目录。

## 构建全栈二进制文件

```bash
goreleaser build --snapshot --single-target --clean
```

该命令会构建两个前端，将它们嵌入二进制文件，并使用 `-tags fullstack` 编译，与 CI 的发布路径一致。手动执行方式如下：

```bash
(cd frontend/admin && pnpm run build:fullstack)   # 注入 <base> 占位符
(cd frontend/user  && pnpm run build)
rm -rf internal/web/dist && mkdir -p internal/web/dist
cp -r frontend/admin/dist internal/web/dist/admin
cp -r frontend/user/dist  internal/web/dist/user
go build -tags release,fullstack -o dujiao-next ./cmd/server
```

注意，管理后台使用 `build:fullstack` 而不是 `build`。普通 `build` 会生成固定到 `/` 的 bundle，在自定义 `web.admin_path` 时会静默失效。

## 测试

```bash
go test ./...                              # 完整测试套件
go test ./internal/architecture/...        # 依赖和分层守卫
go test ./internal/modules/order/...       # 单个模块

cd frontend/user  && pnpm run build        # 包含 vue-tsc 类型检查
cd frontend/admin && pnpm run build
```

健康检查端点：`GET /health`

## 数据访问说明

SQLite 使用 `MaxOpenConns=1`。存储层通过 `WithinTransaction(func(tx contract.Transaction) error)` 开启事务，闭包中的每个查询都必须使用 `tx` 句柄，或使用 `WithTx(tx)` 绑定到该事务的存储层。重新使用全局 DB 句柄会请求第二个永远无法获得的连接，从而让进程死锁；即使是间接调用也一样，例如调用了自行查询的 service。请在打开事务前读取所需的设置，并将出站 HTTP 请求（例如支付网关请求）放在事务之外。

## 在线文档

- https://dujiao-next.com
