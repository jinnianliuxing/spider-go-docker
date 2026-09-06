# Spider-Go

中南林业科技大学教务系统爬虫与服务平台

CSUFT Educational Administration Crawler & Management Platform

## 目录

- [功能特性](#功能特性)
- [技术栈](#技术栈)
- [项目结构](#项目结构)
- [快速启动](#快速启动)
  - [Docker 一键部署（推荐）](#docker-一键部署推荐)
  - [本地开发运行](#本地开发运行)
- [配置说明](#配置说明)
  - [.env 环境变量](#env-环境变量)
  - [生产配置 config.production.yaml](#生产配置-configproductionyaml)
  - [开发配置 config.dev.yaml](#开发配置-configdevyaml)
  - [配置一致性检查清单](#配置一致性检查清单)
- [定时任务](#定时任务)
- [API 文档](#api-文档)
- [数据库表结构](#数据库表结构)
- [安全说明](#安全说明)
- [常见问题](#常见问题)
- [更新日志](#更新日志)

---

## 功能特性

### 用户端

| 功能 | 说明 |
|------|------|
| 🔑 注册 / 登录 | 邮箱注册 + 验证码登录；JWT 鉴权 |
| 📱 微信登录 | 绑定微信小程序（需配置 AppID/AppSecret） |
| 🔗 教务系统绑定 | 学号 + 教务密码绑定，自动 RSA 加密；支持 MFA |
| 📊 成绩查询 | 全学期成绩、GPA 计算、等级考试（四六级等） |
| 📅 课表查询 | 按周查看课程表 |
| 📝 考试安排 | 考试日期 / 教室 / 座位信息 |
| 📢 系统通知 | 管理员发布的通知可见 |
| 🔄 数据同步 | 手动 / 自动触发，实时从教务系统拉取最新数据 |
| ⭐ 成绩排名 | 班级 / 年级排名，支持按学期或累计统计 |
| 📤 课表分享 | 生成分享链接，公开查看他人课表 |
| 🏃 体育选课提示 | 基于历史数据，展示教师评分统计（人数/均分/分数段分布） |
| 🔐 教评辅助 | 自动获取教评任务、题目并支持自动评教 |
| 🌙 深色模式 | 前端支持明暗主题切换 |

### 管理端

| 功能 | 说明 |
|------|------|
| 👥 用户管理 | 查看用户列表、删除用户、批量操作 |
| 📋 同步管理 | 查看所有同步任务、详情、失败重试 |
| 📨 群发邮件 | 向全体用户发送系统通知邮件 |
| ⚙️ 学期配置 | 设置当前学期及开学 / 放假日期 |
| 📊 数据统计 | DAU / 新增用户数 / 绑定统计 |
| 🔔 通知管理 | 增删改查系统通知 |
| 📖 使用须知管理 | 维护首页公告内容 |
| 🗄️ 数据库维护 | 执行 `OPTIMIZE TABLE` 释放磁盘空间 |

### 安全机制

- **登录频率限制**：5 分钟内最多 10 次，超限自动锁定
- **绑定次数限制**：每月最多 N 次教务系统绑定（可在后台配置）
- **教务密码加密**：前端 RSA 加密后传输，后端不存明文
- **会话管理**：Redis 缓存登录态，支持主动失效
- **自动清理**：过期同步日志保留 30 天后自动清理

---

## 技术栈

| 层 | 技术 |
|----|------|
| 语言 | Go 1.25+ |
| Web 框架 | Gin |
| ORM | GORM v2（MySQL 8.0） |
| 缓存 | Redis 7（go-redis/v9） |
| 认证 | JWT（golang-jwt/v5） |
| 调度 | robfig/cron v3 |
| HTML 解析 | PuerkitoBio/goquery |
| JavaScript 引擎 | goja（用于兼容旧版教务系统 JS 校验） |
| 构建部署 | Docker + docker-compose + nginx |

---

## 项目结构

```
spider-go/
├── main.go                        # 程序入口：初始化容器、启动调度器、启动 HTTP 服务
├── Dockerfile                     # 多阶段构建：Go 编译 + Alpine 运行镜像
├── docker-compose.yml             # 四服务编排：MySQL + Redis + spider-go + Nginx
├── nginx.conf                     # 反向代理：80 → /api → 8080，静态资源走本地
├── .env                           # 环境变量（MySQL/Redis 密码，生产环境必改）
├── go.mod / go.sum                # Go 模块依赖
│
├── config/
│   ├── config.production.yaml     # 生产环境配置（MySQL/Redis/JWT/教务系统/邮件）
│   └── config.dev.yaml            # 开发环境配置（独立数据库 + 公网 Redis）
│
├── html/
│   ├── index.html                 # 用户端前端页面（SPA）
│   ├── admin.html                 # 管理后台前端页面（SPA）
│   └── 原版/                      # 备份目录（可安全删除）
│
├── internal/
│   ├── app/
│   │   ├── config.go              # 配置加载（viper，GO_ENV 切换 dev/production）
│   │   ├── db.go                  # MySQL 连接初始化 + GORM AutoMigrate
│   │   └── container.go           # 依赖注入容器：初始化所有模块与服务
│   │
│   ├── api/
│   │   └── routes.go              # 路由注册：公开/认证/管理员三组路由
│   │
│   ├── middleware/
│   │   ├── jwtauth.go             # JWT 用户认证中间件
│   │   ├── admin_auth.go          # JWT 管理员认证中间件
│   │   ├── cors.go                # CORS 跨域配置
│   │   └── ratelimit.go           # IP/邮箱维度登录限流（Redis 滑动窗口）
│   │
│   ├── scheduler/
│   │   ├── scheduler.go           # cron 调度器封装
│   │   └── tasks/
│   │       ├── rsa_refresh.go     # 每小时刷新教务系统 RSA 公钥
│   │       ├── reset_bind_count.go # 每月1日重置绑定计数
│   │       ├── user_sync.go       # 每月1日2:00自动同步所有已绑定用户数据
│   │       └── sync_log_cleanup.go # 每天3:00清理30天前的同步日志
│   │
│   ├── service/                   # 基础设施服务层
│   │   ├── session_service.go     # 教务系统登录（campus/webvpn 双模式）
│   │   ├── crawler_service.go     # HTTP 爬虫适配层
│   │   ├── email_service.go       # 邮件发送适配层（QQ SMTP）
│   │   ├── rsa_key_service.go     # RSA 公钥获取与缓存
│   │   └── dau_service.go         # 日活统计服务
│   │
│   ├── cache/                     # Redis 缓存层
│   │   ├── session_cache.go       # 用户会话缓存
│   │   ├── captcha_cache.go       # 验证码缓存（Lua 原子操作）
│   │   ├── config_cache.go        # 学期配置缓存
│   │   ├── dau_cache.go           # DAU 计数缓存
│   │   ├── userdata_cache.go      # 用户教务数据缓存
│   │   ├── evaluation_cache.go    # 教评任务缓存
│   │   └── magic_link_cache.go    # Magic Link 登录令牌缓存
│   │
│   ├── modules/                   # 业务模块（每个模块独立：model/repo/service/handler）
│   │   ├── user/                  # 用户认证、绑定、微信登录
│   │   ├── admin/                 # 管理员登录、用户管理、群发邮件
│   │   ├── grade/                 # 成绩查询、GPA、平时分、等级考试
│   │   ├── course/                # 课表查询
│   │   ├── exam/                  # 考试安排查询
│   │   ├── evaluation/            # 教评任务、自动评教
│   │   ├── ranking/               # 成绩排名（GPA 实时计算）
│   │   ├── notice/                # 系统通知
│   │   ├── config/                # 学期配置（CRUD）
│   │   ├── statistics/            # DAU / 用户统计
│   │   ├── reconciliation/        # 数据同步任务引擎
│   │   ├── share/                 # 课表分享
│   │   └── coursetips/            # 体育选课教师统计提示
│   │
│   ├── shared/
│   │   ├── jwt_claims.go          # JWT Token 结构定义
│   │   └── user_query.go          # 全局用户查询服务（模块间共享）
│   │
│   ├── common/
│   │   ├── response.go            # 统一响应封装
│   │   └── errors.go              # 业务错误码映射
│   │
│   └── utils/
│       ├── jsCrypto.go            # 前端 CryptoJS AES/RSA 逻辑的 Go 实现
│       └── UrlConstant.go         # URL 常量
│
├── pkg/
│   ├── httpclient/
│   │   ├── client.go              # HTTP 客户端接口
│   │   └── crawler.go             # 带 Cookie 的爬虫实现
│   ├── redis/
│   │   └── client.go              # Redis 连接封装
│   ├── email/
│   │   └── service.go             # 邮件发送（gomail.v2）
│   └── errors/
│       └── errors.go              # 全局错误码定义
│
├── API.md                         # 完整 API 接口文档
└── README.md                      # 本文件
```

---

## 快速启动

### Docker 一键部署（推荐）

#### 前置条件

- Docker ≥ 20.10
- Docker Compose ≥ 2.0
- 一个 SMTP 邮箱（QQ/163 均可，用于发送验证码）

#### 步骤

```bash
# 1. 进入项目目录
cd spider-go

# 2. 检查配置（可选，默认已内置 root123/redis123，可直接启动）
cat .env
cat config/config.production.yaml

# 3. 构建并启动所有服务
docker-compose up -d

# 4. 查看启动日志
docker-compose logs -f spider-go
```

#### 访问地址

| 页面 | 地址 |
|------|------|
| 用户端 | http://localhost |
| 管理后台 | http://localhost/admin.html |

#### 默认管理员账号

> ⚠️ 首次登录后请立即修改密码！

- 邮箱：`admin@spider-go.com`
- 密码：`123456`

#### 常用 Docker 命令

```bash
# 停止服务
docker-compose down

# 停止并删除数据卷（⚠️ 会清空所有数据）
docker-compose down -v

# 重建镜像
docker-compose up -d --build

# 查看各服务状态
docker-compose ps

# 进入容器调试
docker exec -it spider-go sh
```

---

### 本地开发运行

```bash
# 1. 确保本地有 MySQL 和 Redis，或先启动依赖服务
docker-compose up -d mysql redis

# 2. 下载 Go 依赖
go mod download

# 3. 开发环境启动（端口 8081）
go run main.go -env=dev

# 4. 生产环境启动（端口 8080，读取 config.production.yaml）
go run main.go -env=production
```

> 开发环境使用独立的数据库 `spider-dev` 和公网 Redis `118.26.38.103`，生产环境使用 Docker 内的 MySQL/Redis。

---

## 配置说明

### .env 环境变量

用于 `docker-compose.yml`，覆盖默认密码：

```env
# MySQL root 密码（须与 config.production.yaml 中 database.pass 一致）
MYSQL_ROOT_PASSWORD=root123

# 自动创建的数据库名
MYSQL_DATABASE=spider_go

# Redis 密码（须与 config.production.yaml 中 redis.pass 一致）
REDIS_PASSWORD=redis123
```

### 生产配置 config.production.yaml

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `app.port` | `8080` | Go 服务监听端口 |
| `database.user` | `root` | MySQL 用户名 |
| `database.pass` | `root123` | MySQL 密码 |
| `database.name` | `spider_go` | 数据库名 |
| `redis.session.host` | `redis:6379` | Redis 容器内地址 |
| `redis.session.pass` | `redis123` | Redis 密码 |
| `jwt.secret` | `dev_secret_key...` | JWT 签名密钥（**生产环境必改**） |
| `jwc.mode` | `webvpn` | 教务访问模式：`campus`（校内）/ `webvpn`（校外） |
| `email.smtp_host` | `smtp.qq.com` | SMTP 服务器 |
| `email.username` | `3374793735@qq.com` | 发件人邮箱 |
| `email.password` | `ibmuhcxhuuhadaec` | SMTP 授权码（非 QQ 密码） |
| `wx.app_id` / `wx.app_secret` | 空 | 微信小程序配置，留空则禁用微信登录 |
| `oss.provider` | `tencent` | 对象存储提供商：`aliyun` / `tencent` |

### 开发配置 config.dev.yaml

开发与生产配置结构相同，主要差异：

| 配置项 | 开发环境值 | 说明 |
|--------|-----------|------|
| `app.port` | `8081` | 独立端口，避免与生产冲突 |
| `database.source` | `127.0.0.1` | 直连本机 MySQL |
| `database.user` | `spider-dev` | 独立数据库用户 |
| `redis.session.host` | `118.26.38.103:6379` | 公网 Redis（开发用） |
| `jwc.mode` | `webvpn` | 同生产 |

### 配置一致性检查清单

部署前请核对以下内容：

```
✅ .env 中的 MYSQL_ROOT_PASSWORD == config.production.yaml 中的 database.pass
✅ .env 中的 REDIS_PASSWORD == config.production.yaml 中的 redis.session.pass
✅ mysql:3306 与 database.port: 3306 一致
✅ jwt.secret 使用强随机字符串（openssl rand -hex 64）
✅ email.username/password 为真实可用的 SMTP 授权码
✅ cors.allow_origins 包含前端实际域名
✅ nginx.conf 中 server_name 设置为你的域名（或保持 _）
```

---

## 定时任务

系统内置 4 个后台定时任务，使用 `robfig/cron` 调度：

| 任务 | Cron 表达式 | 说明 |
|------|------------|------|
| RSA 公钥刷新 | `0 * * * *`（每小时） | 从教务 CAS 系统拉取最新 RSA 公钥，保证密码加密可用 |
| 重置绑定计数 | `0 0 1 * *`（每月1日0点） | 重置所有用户的本月教务绑定次数为 0 |
| 用户数据自动同步 | `0 2 1 * *`（每月1日2点） | 遍历所有已绑定用户，验证密码有效性，同步最新成绩/课表/考试数据 |
| 同步日志清理 | `0 3 * * *`（每天3点） | 删除 30 天前的同步任务记录和日志，每批 1000 条 |

---

## API 文档

完整 API 接口文档见 [API.md](./API.md)，主要接口概览：

### 认证相关

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/user/register` | 用户注册（需邮箱验证码） |
| `POST` | `/api/user/login` | 用户登录 |
| `POST` | `/api/user/reset-password` | 重置密码（需邮箱验证码） |
| `POST` | `/api/user/wechat/login` | 微信小程序登录/注册 |
| `POST` | `/api/captcha/send` | 发送邮箱验证码 |
| `POST` | `/api/admin/login` | 管理员登录 |

### 用户数据

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/user/info` | 获取当前用户信息 |
| `POST` | `/api/user/bind` | 绑定教务系统（学号+密码） |
| `GET` | `/api/user/bind-status` | 查询绑定状态详情 |
| `GET` | `/api/user/is-bind` | 快速检查是否已绑定 |
| `POST` | `/api/user/update-email` | 更新邮箱 |
| `POST` | `/api/user/update-name` | 更新用户名 |

### 成绩查询

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/user/grades` | 获取成绩（支持 term/year 筛选） |
| `GET` | `/api/user/grades/level` | 等级考试成绩（四六级等） |
| `GET` | `/api/user/grades/analysis` | 成绩统计分析 |
| `POST` | `/api/user/grades/regular` | 获取某门课的平时分 |
| `GET` | `/api/user/grades/student-info` | 学生基本信息（年级/学院/专业） |

### 课程与考试

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/user/courses` | 获取课表（需 week + term） |
| `GET` | `/api/user/exams` | 获取考试安排（需 term） |
| `GET` | `/api/user/course-tips` | 体育选课教师评分统计 |

### 教评

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/user/evaluation/tasks` | 获取教评任务列表 |
| `GET` | `/api/user/evaluation/courses` | 获取待评课程 |
| `GET` | `/api/user/evaluation/questions` | 获取评教题目 |
| `POST` | `/api/user/evaluation/submit` | 提交评教 |
| `POST` | `/api/user/evaluation/auto` | 自动评教 |
| `GET` | `/api/user/evaluation/status` | 查看评教状态 |

### 同步管理

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/user/sync/trigger` | 手动触发同步 |
| `GET` | `/api/user/sync/tasks` | 我的同步任务列表 |
| `GET` | `/api/user/sync/tasks/:taskId` | 同步任务详情 |
| `GET` | `/api/user/sync/status` | 各数据类型的同步状态 |

### 排名

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/user/ranking/my` | 我的排名 |

### 分享

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/user/share/course` | 创建课表分享链接 |
| `GET` | `/api/share/course/:code` | 查看他人分享的课表 |

### 管理后台

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/admin/info` | 管理员信息 |
| `POST` | `/api/admin/reset` | 修改管理员密码 |
| `POST` | `/api/admin/broadcast-email` | 群发邮件 |
| `POST` | `/api/admin/sync/all` | 同步所有用户数据 |
| `GET` | `/api/admin/sync/tasks` | 全部同步任务 |
| `POST` | `/api/admin/sync/optimize` | 优化数据库表 |
| `POST` | `/api/admin/config/term` | 设置当前学期 |
| `POST` | `/api/admin/config/semester-dates` | 设置学期日期 |
| `GET` | `/api/admin/statistics/dau` | 今日 DAU |
| `GET` | `/api/admin/statistics/dau/range` | DAU 范围统计 |
| `GET` | `/api/admin/statistics/user/count` | 用户总数 |
| `GET` | `/api/admin/statistics/user/new` | 新增用户统计 |
| CRUD | `/api/admin/notices` | 通知管理 |
| CRUD | `/api/admin/introductions` | 使用须知管理 |

---

## 数据库表结构

以下表由 GORM `AutoMigrate` 在启动时自动创建：

| 表名 | 说明 | 关键字段 |
|------|------|---------|
| `users` | 用户表 | uid, email(唯一), name, password, sid, spwd(教务密码), avatar, bind_count_current_month, bind_month, last_bind_at, total_bind_count |
| `user_wechat_mini_program` | 微信绑定表 | id, uid, phone_number, app_id, open_id, union_id, last_login |
| `administrators` | 管理员表 | uid, email(唯一), name, password, avatar |
| `notices` | 系统通知 | id, title, content, created_at |
| `introductions` | 使用须知 | id, title, content, created_at |
| `jwc_bind_logs` | 教务绑定日志 | uid, sid, bind_status, created_at |
| `sync_tasks` | 同步任务记录 | task_id, task_type, trigger_type, status, total_users, processed_users, success_users, created_at |
| `sync_logs` | 同步日志 | task_id, uid, data_type, status, error_message, created_at |
| `grades` | 成绩记录 | uid, serial_no, term, code, subject, score, credit, gpa, status, property, course_property, flag |
| `regular_grades` | 平时分记录 | uid, term, code, subject, final_exam_score, regular_score, final_score |
| `exams` | 考试记录 | uid, term, code, subject, exam_time, classroom |
| `level_exams` | 等级考试成绩 | uid, CourseName, LevelGrade, Time |
| `courses` | 课表记录 | uid, term, week, day, course_name, teacher, classroom |
| `user_sync_status` | 用户同步状态 | uid, grade_last_sync_at, grade_sync_version, regular_grade_*, exam_*, level_exam_*, course_* |
| `student_gpas` | GPA 排名 | uid, term, gpa, rank, total_students |
| `course_shares` | 课表分享记录 | token, uid, term, start_week, end_week, created_at |

---

## 安全说明

部署前请务必完成以下安全检查：

- [ ] 修改 `.env` 中的 `MYSQL_ROOT_PASSWORD` 和 `REDIS_PASSWORD`
- [ ] 修改 `config/config.production.yaml` 中的 `jwt.secret`（生成命令：`openssl rand -hex 64`）
- [ ] 修改默认管理员密码（初始：`admin@spider-go.com` / `123456`）
- [ ] 配置 HTTPS（建议使用 Let's Encrypt / Caddy 反向代理）
- [ ] 更新 `cors.allow_origins` 为实际前端域名，不要使用 `*`
- [ ] 更新 `nginx.conf` 中的 `server_name` 为你的域名
- [ ] 确保 MySQL/Redis 不暴露到公网（仅通过 Docker 内部网络通信）
- [ ] 定期备份 `mysql_data` 和 `redis_data` Docker 卷

---

## 常见问题

### Q: 启动后数据库连接失败？

检查 `.env` 中的 `MYSQL_ROOT_PASSWORD` 是否与 `config.production.yaml` 中的 `database.pass` 完全一致。

### Q: 教务系统登录返回 "登录失败"？

- 确认 `jwc.mode` 配置正确：校园网内用 `campus`，校外用 `webvpn`
- 确认学号和教务密码正确
- 部分账号需要 MFA（手机验证码），系统会自动检测并引导

### Q: 成绩/课表数据为空？

- 确认用户已正确绑定教务系统
- 手动触发一次同步：`POST /api/user/sync/trigger`
- 查看同步任务日志：`GET /api/user/sync/tasks`

### Q: 邮件验证码收不到？

- 检查 `config.production.yaml` 中的 `email.username` 和 `email.password` 是否正确
- QQ 邮箱需开启 SMTP 服务并使用**授权码**（非 QQ 登录密码）
- 检查垃圾邮件文件夹

### Q: 如何切换开发/生产环境？

- Docker 部署：`docker-compose.yml` 中固定为 `GO_ENV=production`
- 本地运行：`go run main.go -env=dev` 或 `-env=production`
- 或直接设置环境变量：`export GO_ENV=production`

### Q: 如何更换前端页面？

前端文件位于 `html/index.html` 和 `html/admin.html`，Nginx 直接静态服务。替换后重启 Nginx：
```bash
docker-compose restart nginx
```

### Q: 数据库迁移（已有数据如何升级）？

GORM `AutoMigrate` 只会新增/修改字段，不会删除已有数据或表。升级到新版本后重启容器即可。如需执行自定义 SQL 迁移，请在 `internal/app/db.go` 的 `InitDBWithConfig` 函数中添加。

---

## 更新日志

### 2026-09-06

**教评模块修复与前端接入**

- 🔧 教评 API 地址改为配置驱动（`evaluation_api_base_url`，默认走 WebVPN 网关域名），不再硬编码
- 🔧 修复教评登录：CAS 登录表单补齐 `mfaState` 等字段，修复此前误报「用户名或密码错误」的问题
- 🔧 补齐 `getevaluateResultId` 步骤，提交评教回填 `tevaluateResultid` 与每题答案 `id`，与教务真实接口对齐
- ✨ 前端新增「教学评价」标签页：评教任务 / 课程状态列表，支持一键自动评教（打分题随机一题少给 1 分，必填问答填默认好评）
- ✨ 一键评教采用「两次点击确认」，兼容内嵌浏览器等禁用弹窗的环境；登录按钮增加「登录中...」状态
- 📝 GPA 显示优化：无绩点数据时按 `(成绩-60)÷10+1` 公式在前端自动换算

**升级方式**：拉取代码后 `docker-compose up -d --build`（或导入预构建镜像后 `docker compose up -d`），数据库自动迁移，无需手工操作。

---

## License

MIT
