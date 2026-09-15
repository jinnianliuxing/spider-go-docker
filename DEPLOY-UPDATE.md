# 服务器更新部署说明（2026-09-12）

本次更新：**修复电子成绩单「显示已受理但收不到邮件」的静默失败**（陈旧教务会话被复用）。

---

## 一、以后的标准更新流程（3 步）

**原理**：`docker-compose.yml` 里的 spider-go 服务现在**只写 `image: spider-go:latest`，不再有 `build: .`**。
服务器上不需要 Go 工具链、不需要联网拉依赖，`docker compose up -d` 会直接使用已加载的镜像。

```bash
# 1. 加载新镜像（约 14MB）
docker load -i spider-go-image.tar

# 2. 重建容器（不加 --build！）
docker compose up -d

# 3. 验证
docker ps                                            # spider-go 应为 Up
docker inspect spider-go --format '{{.Image}}'       # 应输出 sha256:29c7951a5071...
docker logs spider-go --tail 20
```

就这三步。**从今往后都不要再加 `--build`。**

---

## 二、本次更新的额外一步（仅这次需要）

本次修复的是**Redis 里的陈旧会话**。服务器上残留的旧会话需要清一次，
否则新代码要等第一次请求时通过预检才发现它失效（虽然会自动重登，但清掉更干净）：

```bash
docker exec spider-go-redis redis-cli -a <你的REDIS_PASSWORD> DEL session:2 session:tgc:2
```

之后正常使用即可，新代码会自动处理今后出现的会话失效。

---

## 三、为什么之前那么麻烦

旧版 `docker-compose.yml` 是这样写的：

```yaml
spider-go:
    build: .              # ← 元凶
    image: spider-go:latest
```

Compose 见到 `build` 字段就**优先本地构建**。服务器上没有 Go 工具链、
也可能拉不到 goproxy，构建就会失败或长时间卡住。
于是只能绕道：本地 `docker build` → `docker save` → 传 14MB 压缩包 → 服务器 `docker load`。

现在删掉 `build: .` 后，服务器端就是纯粹的「加载镜像 + 起容器」，两行命令搞定。

本地开发仍然可以构建：

```bash
docker compose up -d --build     # 显式指定时才会构建
```

---

## 四、产物校验

| 项 | 值 |
|---|---|
| 镜像包 | `spider-go-image.tar`（13.7 MB） |
| md5 | `618adc4b5c65da6cfd8aad5c696d9811` |
| 镜像 ID | `29c7951a5071` |

```bash
md5sum spider-go-image.tar       # 核对 md5
```

---

## 五、注意事项

- **`docker compose up -d` 不加 `--build`** —— 这是关键，加上会触发本地构建（服务器上必然失败）
- `config/` 是 bind-mount 挂载的（`./config:/app/config`），改配置只需 `docker restart spider-go`，不用重建镜像
- `html/` 同样是 bind-mount，改前端只需覆盖文件 + Ctrl+F5 强刷（现在走 admin.html / index.html 直改即可）
- 数据保存在 Docker 命名卷 `mysql_data` / `redis_data` 中，`docker compose up -d` 不会丢失
- 若服务器上的 `config/config.production.yaml` 有手工定制项，覆盖前先备份：
  `cp config/config.production.yaml ~/config.production.yaml.bak`

---

## 六、⚠️ 多站点共存注意事项（2026-09-12 教训）

**这台服务器上的 `spider-nginx` 同时为多个站点提供入口**，不要以为它只服务 spider-go。

当前 vhost 分配（均已并入本包的 `nginx.conf`，bind-mount 生效）：

| 域名 | 后端 | 说明 |
|---|---|---|
| `spider-go.cc.cd` | `spider-go:8080` | 主站（`default_server` 兜底） |
| `sign.spider-go.cc.cd` | `csuft-sign:8787` | 独立项目 `/root/csuft-sign` |

**踩过的坑**：`sign` 的 vhost 原先单独放在容器的 `/etc/nginx/conf.d/sign.conf`，
而 `spider-nginx` **并未挂载 `conf.d` 目录**。因此当 `docker compose up -d` 重建容器时，
该文件随旧容器一起销毁，`sign.spider-go.cc.cd` 就落到了 spider-go 的默认 server。

**现已永久修复**：sign 的 vhost 已并入 `nginx.conf`（bind-mount 到 `/etc/nginx/nginx.conf`），
容器重建不再丢失。同时给 spider-go 的 server 加了 `default_server`，未匹配域名有明确归属。

**以后更新前必做**：

```bash
# 备份 nginx.conf（不要只备份 config！）
cp /root/spider-go/nginx.conf /root/spider-go/nginx.conf.bak-$(date +%Y%m%d-%H%M)

# 覆盖前先 diff，确认丢不了东西
diff /root/spider-go/nginx.conf /path/to/new/nginx.conf

# 覆盖后必须校验再 reload
docker exec spider-nginx nginx -t
docker exec spider-nginx nginx -s reload
```

**判断 nginx 托管了哪些站点的通用方法**：

```bash
# 看容器内实际的 vhost 列表
docker exec spider-nginx grep -rn "server_name" /etc/nginx/

# 看挂载了哪些文件
docker inspect spider-nginx --format '{{json .Mounts}}'
```

**另一条经验**：`docker cp` 写进容器的文件是**临时的**，容器一重建就没了。
凡是需要持久化的配置，必须放进 bind-mount 的文件里。

### 附带修复：nginx 与上游解耦

原配置用的是**字面量** `proxy_pass http://spider-go:8080/api/;`。
nginx 启动时会对字面量上游做 DNS 解析，**若 `spider-go` 容器没在运行，
nginx 会直接启动失败**（`host not found in upstream "spider-go"`），
连带把 `sign` 站点也一起拖挂。

现已改为**变量式**写法：

```nginx
location /api/ {
    set $upstream_api http://spider-go:8080;
    proxy_pass $upstream_api$request_uri;    # 变量不会在启动时解析
    ...
}
```

配合 `resolver 127.0.0.11`，上游容器 IP 变化会自动跟上，
且**单个上游挂掉只会让该站点返回 502，nginx 本身照常运行**，站点之间彻底解耦。

本地已验证：`spider-go` 容器处于 Exited 状态时，nginx 仍能正常启动、
静态资源照常服务，仅 `/api/` 返回 502。

⚠️ **注意变量式 proxy_pass 必须手动拼接 `$request_uri`**，
否则路径会丢（这是变量式写法最常见的坑，已验证路径透传正常）。

