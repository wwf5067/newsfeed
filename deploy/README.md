# 部署到腾讯云 Lighthouse

整体思路:GitHub Actions 自动构建镜像推到 GHCR,再 SSH 到 Lighthouse 拉镜像启动 docker-compose。前端构建为静态产物,由 Nginx 提供。

## 一次性准备(每个新服务器只需做一次)

### 1. 买 Lighthouse 实例
- 镜像选 **Ubuntu 22.04 / 24.04 LTS**(脚本基于 apt)
- 配置选 2C2G 或更高
- 创建后记下公网 IP

### 2. 给服务器添加 SSH 密钥(本地 → Lighthouse 免密登录,GitHub Actions 也用这把)
本地终端:
```bash
# 生成一对部署专用密钥(不要复用日常密钥)
ssh-keygen -t ed25519 -C "newsfeed-deploy" -f ~/.ssh/newsfeed_deploy

# 把公钥扔到 Lighthouse 控制台 -> 密钥 -> 创建 -> 关联到实例
cat ~/.ssh/newsfeed_deploy.pub
```
登录测试:
```bash
ssh -i ~/.ssh/newsfeed_deploy ubuntu@<LIGHTHOUSE_IP>
```

### 3. 在服务器上跑引导脚本
```bash
# 把脚本传上去并执行
scp -i ~/.ssh/newsfeed_deploy deploy/bootstrap.sh ubuntu@<IP>:/tmp/
ssh -i ~/.ssh/newsfeed_deploy ubuntu@<IP> 'bash /tmp/bootstrap.sh'

# 重新登录一次,让 docker 用户组生效
ssh -i ~/.ssh/newsfeed_deploy ubuntu@<IP>
```
脚本会:
- 装 Docker
- 配置防火墙(只放 22 / 80)
- 在 `/opt/newsfeed/` 创建 `.env` 模板

### 4. 填好服务器上的 `/opt/newsfeed/.env`
```bash
ssh ... 'vim /opt/newsfeed/.env'
```
重点项:
- `POSTGRES_PASSWORD` — 改成强密码
- `ZHIHU_COOKIE` — 浏览器登录知乎后从 DevTools 复制整段 Cookie
- `API_IMAGE` / `CRAWLER_IMAGE` 暂时不用改,CI 部署时会自动替换 tag

### 5. 配 GitHub Secrets
仓库 Settings → Secrets and variables → Actions → New repository secret:

| Name | Value |
|---|---|
| `SSH_HOST` | Lighthouse 公网 IP |
| `SSH_USER` | `ubuntu`(或你创建实例时设的用户名) |
| `SSH_PORT` | `22`(默认即可) |
| `SSH_KEY` | `cat ~/.ssh/newsfeed_deploy` 的**完整内容**,含 `-----BEGIN OPENSSH PRIVATE KEY-----` 头尾 |

GHCR 镜像默认是 private——如果你想保持 private,需要在服务器上:
```bash
echo <GITHUB_PAT> | docker login ghcr.io -u <你的 GitHub 用户名> --password-stdin
```
PAT 在 GitHub Settings → Developer settings → Personal access tokens → 勾 `read:packages`。

或者更省事:把镜像设为 public(GitHub 仓库 Packages 页面 → 包名 → Settings → Change visibility)。

### 6. 触发首次部署
两种方式任选:
- **推 main**: `git push origin main`
- **手动触发**: 仓库 Actions 页面 → `deploy` workflow → Run workflow

部署完成后浏览器访问 `http://<LIGHTHOUSE_IP>` 即可。

## 日常运维

> 所有命令在 `/opt/newsfeed/` 下执行。

### 查看运行状态
```bash
docker compose -f docker-compose.prod.yml ps
docker compose -f docker-compose.prod.yml logs -f api
docker compose -f docker-compose.prod.yml logs -f crawler
```

### 手动触发一次抓取(用 RUN_ON_START)
```bash
sed -i 's/^RUN_ON_START=.*/RUN_ON_START=true/' .env
docker compose -f docker-compose.prod.yml up -d crawler
docker compose -f docker-compose.prod.yml logs -f crawler
# 验证完改回 false
sed -i 's/^RUN_ON_START=.*/RUN_ON_START=false/' .env
docker compose -f docker-compose.prod.yml up -d crawler
```

### 应用新数据库迁移
CI 会自动跑,如要手动:
```bash
docker compose -f docker-compose.prod.yml --profile migrate run --rm migrate
```

### 进 psql 查数据
```bash
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U newsfeed -d newsfeed
```

### 备份数据库到本机
```bash
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U newsfeed newsfeed | gzip > newsfeed-$(date +%Y%m%d).sql.gz
```

### 回滚到旧版本
```bash
# 每个 commit 的 7 位短 sha 就是镜像 tag,例如 abc1234
sed -i 's|newsfeed-api:.*|newsfeed-api:abc1234|' .env
sed -i 's|newsfeed-crawler:.*|newsfeed-crawler:abc1234|' .env
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d
```

### Cookie 失效后更新
```bash
vim .env  # 改 ZHIHU_COOKIE
docker compose -f docker-compose.prod.yml up -d crawler
```

## 后续扩展(按需)

- **HTTPS**: 申请域名 + 备案(腾讯云内地实例必需)→ Certbot 加证书 → 改 nginx.conf 加 443 server
- **数据库托管**: 流量大了把 postgres 容器换成腾讯云 CDB,生产更稳
- **监控**: 上 Uptime Kuma 自己监控自己,或用腾讯云监控告警
