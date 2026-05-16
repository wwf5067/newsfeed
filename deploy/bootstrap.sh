#!/usr/bin/env bash
# Lighthouse 服务器一次性环境引导。
# 用法:首次 SSH 进服务器后:
#   curl -fsSL https://raw.githubusercontent.com/<OWNER>/newsfeed/main/deploy/bootstrap.sh | bash
# 或者 scp 上来后 bash deploy/bootstrap.sh

set -euo pipefail

APP_DIR=/opt/newsfeed

echo "==> Updating apt and installing prerequisites"
sudo apt-get update -y
sudo apt-get install -y ca-certificates curl gnupg ufw

if ! command -v docker >/dev/null 2>&1; then
  echo "==> Installing Docker via Tencent Cloud mirror (国内服务器 get.docker.com 经常被打断)"

  sudo install -m 0755 -d /etc/apt/keyrings
  sudo curl -fsSL https://mirrors.tencent.com/docker-ce/linux/ubuntu/gpg \
    -o /etc/apt/keyrings/docker.asc
  sudo chmod a+r /etc/apt/keyrings/docker.asc

  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
https://mirrors.tencent.com/docker-ce/linux/ubuntu \
$(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
    | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

  sudo apt-get update
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io \
    docker-buildx-plugin docker-compose-plugin

  # 国内服务器拉 docker.io 镜像也常常超时,配置镜像加速
  sudo mkdir -p /etc/docker
  sudo tee /etc/docker/daemon.json > /dev/null <<'JSON'
{
  "registry-mirrors": [
    "https://mirror.ccs.tencentyun.com",
    "https://docker.m.daocloud.io",
    "https://dockerproxy.com"
  ]
}
JSON

  sudo usermod -aG docker "$USER"
  sudo systemctl enable --now docker
  sudo systemctl restart docker
  echo ">> Docker installed. NOTE: 你需要重新 SSH 登录一次,'docker' 命令才不需要 sudo。"
fi

echo "==> Configuring firewall (allow 22 and 80)"
sudo ufw allow 22/tcp || true
sudo ufw allow 80/tcp || true
sudo ufw --force enable || true

echo "==> Preparing $APP_DIR"
sudo mkdir -p "$APP_DIR"
sudo chown "$USER":"$USER" "$APP_DIR"

if [ ! -f "$APP_DIR/.env" ]; then
  cat > "$APP_DIR/.env" <<'ENV'
# 首次部署:填好以下值,然后从仓库的 GitHub Actions 触发一次部署。
# 字段含义见 deploy/.env.prod.example。

API_IMAGE=ghcr.io/wwf5067/newsfeed-api:latest
CRAWLER_IMAGE=ghcr.io/wwf5067/newsfeed-crawler:latest

POSTGRES_USER=newsfeed
POSTGRES_PASSWORD=CHANGE_ME_STRONG_PASSWORD
POSTGRES_DB=newsfeed

ZHIHU_COOKIE=
ZHIHU_SCHEDULE=0 */30 * * * *
RUN_ON_START=false
ENV
  echo ">> Created $APP_DIR/.env (template). 请先编辑该文件填入 ZHIHU_COOKIE 和强密码,再从 GitHub 触发部署。"
else
  echo ">> $APP_DIR/.env already exists, leaving untouched."
fi

echo "==> Done. 接下来:"
echo "   1) vim $APP_DIR/.env  # 填好 cookie / 数据库密码"
echo "   2) 在 GitHub 仓库 Actions 触发一次 'deploy' workflow,或 push 到 main"
echo "   3) 部署成功后访问: http://<本机 IP>"
