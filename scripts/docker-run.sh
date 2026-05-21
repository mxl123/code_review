#!/usr/bin/env bash
set -euo pipefail

# ------------------------------------------------------------------ #
#  用法：
#    ./scripts/docker-run.sh                 # 使用默认值，从 .env 读取环境变量
#    ./scripts/docker-run.sh -t v1.2.3       # 指定镜像 tag
#    ./scripts/docker-run.sh -r registry.example.com/myns  # 指定镜像仓库
#    ./scripts/docker-run.sh -p 9090         # 指定宿主机端口
#    ./scripts/docker-run.sh --no-run        # 只构建，不运行
#    ./scripts/docker-run.sh --push          # 构建后推送镜像
# ------------------------------------------------------------------ #

# ---------- 默认值 ----------
IMAGE_REPO="code-review"
IMAGE_TAG="latest"
CONTAINER_NAME="code-review"
HOST_PORT="8080"
ENV_FILE=".env"
DO_RUN=true
DO_PUSH=false

# ---------- 参数解析 ----------
while [[ $# -gt 0 ]]; do
  case "$1" in
    -r|--repo)    IMAGE_REPO="$2";    shift 2 ;;
    -t|--tag)     IMAGE_TAG="$2";     shift 2 ;;
    -n|--name)    CONTAINER_NAME="$2"; shift 2 ;;
    -p|--port)    HOST_PORT="$2";     shift 2 ;;
    -e|--env-file) ENV_FILE="$2";    shift 2 ;;
    --no-run)     DO_RUN=false;       shift   ;;
    --push)       DO_PUSH=true;       shift   ;;
    -h|--help)
      sed -n '3,10p' "$0" | sed 's/^#  \?//'
      exit 0
      ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

IMAGE="${IMAGE_REPO}:${IMAGE_TAG}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------- 构建 ----------
echo "▶ 构建镜像: ${IMAGE}"
docker build -t "${IMAGE}" "${PROJECT_ROOT}"
echo "✓ 构建完成"

# ---------- 推送（可选）----------
if [[ "${DO_PUSH}" == "true" ]]; then
  echo "▶ 推送镜像: ${IMAGE}"
  docker push "${IMAGE}"
  echo "✓ 推送完成"
fi

# ---------- 运行 ----------
if [[ "${DO_RUN}" == "false" ]]; then
  echo "（跳过运行，仅构建）"
  exit 0
fi

# 检查 .env 文件
ENV_FILE_PATH="${PROJECT_ROOT}/${ENV_FILE}"
if [[ ! -f "${ENV_FILE_PATH}" ]]; then
  echo "⚠ 未找到 ${ENV_FILE_PATH}，将不加载环境变量文件"
  echo "  可复制 .env.example 并填写配置：cp .env.example .env"
  ENV_OPT=""
else
  ENV_OPT="--env-file ${ENV_FILE_PATH}"
fi

# 停止并删除同名旧容器
if docker inspect "${CONTAINER_NAME}" &>/dev/null; then
  echo "▶ 停止旧容器: ${CONTAINER_NAME}"
  docker stop "${CONTAINER_NAME}" &>/dev/null || true
  docker rm   "${CONTAINER_NAME}" &>/dev/null || true
fi

echo "▶ 启动容器: ${CONTAINER_NAME}"
docker run -d \
  --name "${CONTAINER_NAME}" \
  --restart unless-stopped \
  -p "${HOST_PORT}:8080" \
  ${ENV_OPT} \
  "${IMAGE}"

echo "✓ 容器已启动"
echo ""
echo "  健康检查: http://localhost:${HOST_PORT}/health"
echo "  查看日志: docker logs -f ${CONTAINER_NAME}"
echo "  停止容器: docker stop ${CONTAINER_NAME}"
