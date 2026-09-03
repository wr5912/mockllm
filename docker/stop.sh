#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" \
    || { printf '无法定位脚本目录。\n' >&2; exit 1; }
readonly SCRIPT_DIR
# shellcheck source=lib/compose.sh
source "${SCRIPT_DIR}/lib/compose.sh"

main() {
    # 停止服务时即使 config.json 被误删，也应允许保留资源地退出。
    initialize_compose false

    local container_id=""
    local container_state=""
    container_id="$(service_container_id)"

    if [[ -z "${container_id}" ]]; then
        log "服务容器不存在，无需停止。"
        return 0
    fi

    container_state="$(
        docker inspect --format '{{.State.Status}}' "${container_id}" 2>/dev/null \
            || true
    )"
    case "${container_state}" in
        exited | created)
            log "服务已经停止，容器、镜像和配置均已保留。"
            return 0
            ;;
        paused)
            log "服务已暂停，先恢复进程再执行优雅停止..."
            run_compose unpause "${SERVICE_NAME}"
            ;;
    esac

    log "优雅停止 ${SERVICE_NAME}（超时 10 秒）..."
    run_compose stop --timeout 10 "${SERVICE_NAME}"
    log "服务已停止，容器、镜像和配置均已保留。"
}

main "$@"
