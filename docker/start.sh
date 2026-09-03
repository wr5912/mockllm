#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" \
    || { printf '无法定位脚本目录。\n' >&2; exit 1; }
readonly SCRIPT_DIR
# shellcheck source=lib/compose.sh
source "${SCRIPT_DIR}/lib/compose.sh"

main() {
    initialize_compose true

    local container_id=""
    local container_state=""

    validate_mock_config
    container_id="$(service_container_id)"

    if [[ -z "${container_id}" ]]; then
        log "服务容器不存在，使用已有镜像创建服务..."
        run_compose up --detach --no-build "${SERVICE_NAME}"
    else
        container_state="$(
            docker inspect --format '{{.State.Status}}' "${container_id}" 2>/dev/null \
                || true
        )"

        case "${container_state}" in
            running | restarting)
                log "服务正在运行，重启以重新加载 config.json..."
                run_compose restart --timeout 10 "${SERVICE_NAME}"
                ;;
            paused)
                log "服务已暂停，恢复并重启以重新加载 config.json..."
                run_compose unpause "${SERVICE_NAME}"
                run_compose restart --timeout 10 "${SERVICE_NAME}"
                ;;
            created | exited)
                log "启动已停止的服务..."
                run_compose start "${SERVICE_NAME}"
                ;;
            *)
                die "无法启动状态为 ${container_state:-未知} 的容器，请运行 ${DOCKER_DIR}/deploy.sh 重新部署。"
                ;;
        esac
    fi

    wait_for_service_health
    print_access_urls
}

main "$@"
