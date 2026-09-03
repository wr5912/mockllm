#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" \
    || { printf '无法定位脚本目录。\n' >&2; exit 1; }
readonly SCRIPT_DIR
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd -P)" \
    || { printf '无法定位仓库根目录。\n' >&2; exit 1; }
readonly REPO_ROOT
readonly DOCKERFILE="${REPO_ROOT}/docker/Dockerfile"
readonly IMAGE_NAME="mockllm:local"
readonly IMAGE_ARCHIVE="${SCRIPT_DIR}/mockllm.tar"
readonly IMAGE_LOCK="${SCRIPT_DIR}/.mockllm.lock"
readonly GOPROXY_VALUE="${GOPROXY:-https://proxy.golang.org,direct}"

archive_tmp=""
image_lock_fd=""

log() {
    printf '[mockllm-build] %s\n' "$*"
}

die() {
    printf '[mockllm-build] 错误: %s\n' "$*" >&2
    exit 1
}

cleanup() {
    if [[ -n "${archive_tmp}" && -e "${archive_tmp}" ]]; then
        rm -f -- "${archive_tmp}"
    fi
}

handle_signal() {
    local exit_code="$1"
    trap - HUP INT TERM
    exit "${exit_code}"
}

trap cleanup EXIT
trap 'handle_signal 129' HUP
trap 'handle_signal 130' INT
trap 'handle_signal 143' TERM

main() {
    command -v docker >/dev/null 2>&1 || die "未找到 docker，请先安装 Docker。"
    command -v mktemp >/dev/null 2>&1 || die "未找到 mktemp。"
    command -v flock >/dev/null 2>&1 || die "未找到 flock。"
    [[ -f "${DOCKERFILE}" ]] || die "Dockerfile 不存在：${DOCKERFILE}"
    if [[ "${GOPROXY_VALUE}" =~ ://[^/,\|[:space:]]+@ ]]; then
        die "GOPROXY 不得包含 URL 用户名、密码或 token；Docker build arg 不适合传递密钥。"
    fi
    if ! docker info >/dev/null 2>&1; then
        die "无法连接 Docker daemon，请确认 Docker 已启动且当前用户有访问权限。"
    fi

    log "等待镜像归档锁..."
    if ! exec {image_lock_fd}>"${IMAGE_LOCK}"; then
        die "无法打开镜像归档锁：${IMAGE_LOCK}"
    fi
    flock --exclusive "${image_lock_fd}" \
        || die "无法获取镜像归档锁：${IMAGE_LOCK}"

    log "使用 ${DOCKERFILE} 构建 ${IMAGE_NAME}..."
    docker build \
        --build-arg "GOPROXY=${GOPROXY_VALUE}" \
        --file "${DOCKERFILE}" \
        --tag "${IMAGE_NAME}" \
        "${REPO_ROOT}"

    archive_tmp="$(mktemp "${SCRIPT_DIR}/.mockllm.tar.tmp.XXXXXX")"

    log "导出镜像归档..."
    docker save --output "${archive_tmp}" "${IMAGE_NAME}"

    chmod 0644 "${archive_tmp}"

    mv -f -- "${archive_tmp}" "${IMAGE_ARCHIVE}"
    archive_tmp=""

    log "镜像归档已更新：${IMAGE_ARCHIVE}"
}

main "$@"
