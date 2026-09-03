#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" \
    || { printf '无法定位脚本目录。\n' >&2; exit 1; }
readonly SCRIPT_DIR
# shellcheck source=lib/compose.sh
source "${SCRIPT_DIR}/lib/compose.sh"

main() {
    initialize_compose true
    acquire_image_lock
    validate_image_archive
    load_image_archive
    validate_mock_config

    log "创建或重建 ${SERVICE_NAME} 服务..."
    run_compose up --detach --force-recreate --no-build "${SERVICE_NAME}"

    wait_for_service_health
    print_access_urls
}

main "$@"
