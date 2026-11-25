#!/usr/bin/env bash

set -Eeuo pipefail

LIB_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)" \
    || { printf '无法定位脚本目录。\n' >&2; exit 1; }
readonly LIB_DIR
DOCKER_DIR="$(cd -- "${LIB_DIR}/.." && pwd -P)" \
    || { printf '无法定位 docker 目录。\n' >&2; exit 1; }
readonly DOCKER_DIR
readonly COMPOSE_FILE="${DOCKER_DIR}/compose.yaml"
readonly CONFIG_FILE="${DOCKER_DIR}/config/config.json"
readonly REPO_ROOT="$(cd -- "${DOCKER_DIR}/.." && pwd -P)"
readonly IMAGE_ARCHIVE="${REPO_ROOT}/images/mockllm.tar"
readonly IMAGE_LOCK="${REPO_ROOT}/images/.mockllm.lock"
readonly PROJECT_NAME="mockllm"
readonly SERVICE_NAME="mockllm"
readonly IMAGE_NAME="mockllm:local"
readonly SERVICE_PORT="50502"

declare -a COMPOSE_BIN=()
declare -a COMPOSE_CMD=()
COMPOSE_VERSION=""
IMAGE_LOCK_FD=""
ARCHIVE_IMAGE_ID=""
ARCHIVE_CONFIG_ID=""
ARCHIVE_INDEX_ID=""

log() {
    printf '[mockllm] %s\n' "$*"
}

error() {
    printf '[mockllm] 错误: %s\n' "$*" >&2
}

die() {
    error "$*"
    exit 1
}

select_compose() {
    command -v docker >/dev/null 2>&1 || die "未找到 docker，请先安装 Docker。"

    if docker compose version >/dev/null 2>&1; then
        COMPOSE_BIN=(docker compose)
    elif command -v docker-compose >/dev/null 2>&1 \
        && docker-compose version >/dev/null 2>&1; then
        COMPOSE_BIN=(docker-compose)
    else
        die "未找到 Docker Compose v2 或更高版本（docker compose / docker-compose）。"
    fi

    local version_output
    if ! version_output="$("${COMPOSE_BIN[@]}" version --short 2>&1)"; then
        version_output="$("${COMPOSE_BIN[@]}" version 2>&1)" \
            || die "无法读取 Docker Compose 版本。"
    fi

    if [[ ! "${version_output}" =~ [vV]?([0-9]+)\.[0-9]+(\.[0-9]+)? ]]; then
        die "无法解析 Docker Compose 版本：${version_output}"
    fi

    local major_version="${BASH_REMATCH[1]}"
    if (( major_version < 2 )); then
        die "检测到 Docker Compose v1（${version_output}），请升级到 Compose v2 或更高版本。"
    fi

    COMPOSE_VERSION="${BASH_REMATCH[0]}"
    COMPOSE_CMD=(
        "${COMPOSE_BIN[@]}"
        --project-name "${PROJECT_NAME}"
        --project-directory "${DOCKER_DIR}"
        --file "${COMPOSE_FILE}"
    )
}

run_compose() {
    "${COMPOSE_CMD[@]}" "$@"
}

check_docker_daemon() {
    if ! docker info >/dev/null 2>&1; then
        die "无法连接 Docker daemon，请确认 Docker 已启动且当前用户有访问权限。"
    fi
}

check_files() {
    local require_config="${1:-true}"

    [[ -f "${COMPOSE_FILE}" ]] \
        || die "Compose 文件不存在：${COMPOSE_FILE}"

    if [[ "${require_config}" == "true" ]]; then
        [[ -f "${CONFIG_FILE}" ]] \
            || die "配置文件不存在：${CONFIG_FILE}"
        [[ -r "${CONFIG_FILE}" ]] \
            || die "配置文件不可读：${CONFIG_FILE}"
    fi
}

validate_compose() {
    log "校验 Compose 配置（${COMPOSE_BIN[*]} ${COMPOSE_VERSION}）..."
    if ! run_compose config >/dev/null; then
        die "Compose 配置校验失败。"
    fi
}

initialize_compose() {
    local require_config="${1:-true}"

    select_compose
    check_docker_daemon
    check_files "${require_config}"
    validate_compose
}

acquire_image_lock() {
    command -v flock >/dev/null 2>&1 \
        || die "未找到 flock，无法安全使用镜像归档。"

    log "等待镜像归档锁..."
    if ! exec {IMAGE_LOCK_FD}>"${IMAGE_LOCK}"; then
        die "无法打开镜像归档锁：${IMAGE_LOCK}"
    fi
    flock --exclusive "${IMAGE_LOCK_FD}" \
        || die "无法获取镜像归档锁：${IMAGE_LOCK}"
}

validate_image_archive() {
    command -v sha256sum >/dev/null 2>&1 \
        || die "未找到 sha256sum，无法校验镜像归档。"
    command -v tar >/dev/null 2>&1 \
        || die "未找到 tar，无法检查镜像归档 manifest。"

    [[ -f "${IMAGE_ARCHIVE}" && -r "${IMAGE_ARCHIVE}" ]] \
        || die "镜像归档不存在或不可读：${IMAGE_ARCHIVE}。请先运行 ${REPO_ROOT}/images/build.sh。"

    local manifest_json=""
    if ! manifest_json="$(tar -xOf "${IMAGE_ARCHIVE}" manifest.json 2>/dev/null)"; then
        die "镜像归档缺少可读的 manifest.json，未更改正式服务。"
    fi

    local tag_pattern='"RepoTags"[[:space:]]*:[[:space:]]*\[[^]]*"mockllm:local"'
    if [[ ! "${manifest_json}" =~ ${tag_pattern} ]]; then
        die "镜像归档 manifest.json 不包含 ${IMAGE_NAME}，未更改正式服务。"
    fi

    local config_pattern='"Config"[[:space:]]*:[[:space:]]*"([^"]+)"'
    if [[ ! "${manifest_json}" =~ ${config_pattern} ]]; then
        die "无法从镜像归档 manifest.json 解析 image config，未更改正式服务。"
    fi

    local config_path="${BASH_REMATCH[1]}"
    local config_digest="${config_path##*/}"
    config_digest="${config_digest%.json}"
    [[ "${config_digest}" =~ ^[[:xdigit:]]{64}$ ]] \
        || die "镜像归档的 image config digest 无效，未更改正式服务。"

    local config_blob_hash=""
    if ! config_blob_hash="$(
        tar -xOf "${IMAGE_ARCHIVE}" "${config_path}" 2>/dev/null | sha256sum
    )"; then
        die "无法读取镜像归档的 image config blob，未更改正式服务。"
    fi
    config_blob_hash="${config_blob_hash%% *}"
    if [[ "${config_blob_hash,,}" != "${config_digest,,}" ]]; then
        die "镜像归档的 image config digest 校验失败，未更改正式服务。"
    fi

    ARCHIVE_CONFIG_ID="sha256:${config_digest,,}"
    ARCHIVE_IMAGE_ID="${ARCHIVE_CONFIG_ID}"

    # Docker 使用 classic image store 时，image ID 是 config digest；使用
    # containerd image store 保存的 OCI archive 时，image ID 是 index.json
    # 中的顶层 descriptor digest。两种格式都需要支持。
    local index_json=""
    if index_json="$(tar -xOf "${IMAGE_ARCHIVE}" index.json 2>/dev/null)"; then
        local image_id_pattern='"digest"[[:space:]]*:[[:space:]]*"(sha256:[[:xdigit:]]{64})"'
        if [[ ! "${index_json}" =~ ${image_id_pattern} ]]; then
            die "无法从镜像归档 index.json 解析 image ID，未更改正式服务。"
        fi
        ARCHIVE_INDEX_ID="${BASH_REMATCH[1],,}"

        local index_blob_path="blobs/sha256/${ARCHIVE_INDEX_ID#sha256:}"
        local index_blob_hash=""
        if ! index_blob_hash="$(
            tar -xOf "${IMAGE_ARCHIVE}" "${index_blob_path}" 2>/dev/null | sha256sum
        )"; then
            die "无法读取镜像归档的 OCI index descriptor，未更改正式服务。"
        fi
        index_blob_hash="${index_blob_hash%% *}"
        if [[ "${index_blob_hash,,}" != "${ARCHIVE_INDEX_ID#sha256:}" ]]; then
            die "镜像归档的 OCI index descriptor digest 校验失败，未更改正式服务。"
        fi

        ARCHIVE_IMAGE_ID="${ARCHIVE_INDEX_ID}"
    fi

    log "镜像归档 manifest 与内部 digest 校验通过。"
}

load_image_archive() {
    [[ -n "${ARCHIVE_IMAGE_ID}" ]] \
        || die "尚未完成镜像归档 manifest 校验，拒绝加载。"

    log "从归档加载 ${IMAGE_NAME}..."
    docker load --input "${IMAGE_ARCHIVE}"

    local loaded_image_id=""
    if ! loaded_image_id="$(docker image inspect --format '{{.Id}}' "${IMAGE_NAME}" 2>/dev/null)"; then
        die "归档加载后未找到镜像 ${IMAGE_NAME}，未更改正式服务。"
    fi
    loaded_image_id="${loaded_image_id,,}"
    if [[ "${loaded_image_id}" != "${ARCHIVE_CONFIG_ID}" \
        && ( -z "${ARCHIVE_INDEX_ID}" || "${loaded_image_id}" != "${ARCHIVE_INDEX_ID}" ) ]]; then
        die "${IMAGE_NAME} 与归档 manifest 的 image ID 不一致，未更改正式服务。"
    fi

    log "已确认 ${IMAGE_NAME} 对应归档 image ID：${loaded_image_id}"
}

service_container_id() {
    local container_id=""

    if ! container_id="$(run_compose ps --all --quiet "${SERVICE_NAME}" 2>/dev/null)"; then
        die "无法查询 ${SERVICE_NAME} 容器状态。"
    fi

    printf '%s\n' "${container_id}"
}

service_container_id_best_effort() {
    run_compose ps --all --quiet "${SERVICE_NAME}" 2>/dev/null || true
}

validate_mock_config() {
    docker image inspect "${IMAGE_NAME}" >/dev/null 2>&1 \
        || die "镜像 ${IMAGE_NAME} 不存在，请先运行 ${DOCKER_DIR}/deploy.sh。"

    log "使用 ${IMAGE_NAME} 校验 config.json..."
    if ! docker run --rm \
        --network none \
        --read-only \
        --mount "type=bind,source=${CONFIG_FILE},target=/etc/mockllm/config.json,readonly" \
        "${IMAGE_NAME}" \
        --config /etc/mockllm/config.json \
        --check-config >/dev/null; then
        die "config.json 无法由 MockLLM 解析，未更改正式服务。"
    fi
}

show_service_diagnostics() {
    local container_id
    container_id="$(service_container_id_best_effort)"

    if [[ -n "${container_id}" ]]; then
        docker inspect \
            --format '容器状态={{.State.Status}} 健康状态={{if .State.Health}}{{.State.Health.Status}}{{else}}未配置{{end}} 重启次数={{.RestartCount}}' \
            "${container_id}" 2>/dev/null || true
    fi

    printf '%s\n' '----- mockllm 最近日志 -----' >&2
    run_compose logs --no-color --tail 100 "${SERVICE_NAME}" >&2 || true
    printf '%s\n' '----------------------------' >&2
}

wait_for_service_health() {
    local timeout="${MOCKLLM_HEALTH_TIMEOUT_SECONDS:-90}"
    [[ "${timeout}" =~ ^[1-9][0-9]*$ ]] \
        || die "MOCKLLM_HEALTH_TIMEOUT_SECONDS 必须是正整数。"

    local deadline=$((SECONDS + timeout))
    local container_id=""
    local container_state=""
    local health_state=""

    log "等待服务健康（最多 ${timeout} 秒）..."
    while (( SECONDS < deadline )); do
        container_id="$(service_container_id)"

        if [[ -n "${container_id}" ]]; then
            container_state="$(
                docker inspect --format '{{.State.Status}}' "${container_id}" 2>/dev/null \
                    || true
            )"
            health_state="$(
                docker inspect \
                    --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' \
                    "${container_id}" 2>/dev/null || true
            )"

            if [[ "${container_state}" == "running" && "${health_state}" == "healthy" ]]; then
                log "服务已健康。"
                return 0
            fi

            if [[ "${health_state}" == "unhealthy" ]]; then
                error "容器健康检查失败。"
                show_service_diagnostics
                return 1
            fi

            case "${container_state}" in
                dead | exited | removing)
                    error "容器未能保持运行（状态：${container_state}）。"
                    show_service_diagnostics
                    return 1
                    ;;
            esac
        fi

        sleep 1
    done

    error "等待服务健康超时（${timeout} 秒）。"
    show_service_diagnostics
    return 1
}

detect_lan_ip() {
    local route_info=""
    local candidate=""
    local previous=""

    if command -v ip >/dev/null 2>&1; then
        route_info="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
        for candidate in ${route_info}; do
            if [[ "${previous}" == "src" && "${candidate}" =~ ^[0-9]+(\.[0-9]+){3}$ ]]; then
                printf '%s\n' "${candidate}"
                return 0
            fi
            previous="${candidate}"
        done
    fi

    if command -v hostname >/dev/null 2>&1; then
        route_info="$(hostname -I 2>/dev/null || true)"
        for candidate in ${route_info}; do
            if [[ "${candidate}" =~ ^[0-9]+(\.[0-9]+){3}$ && "${candidate}" != 127.* ]]; then
                printf '%s\n' "${candidate}"
                return 0
            fi
        done
    fi

    return 1
}

print_access_urls() {
    local lan_ip=""

    printf '\n访问地址：\n'
    printf '  本机：   http://127.0.0.1:%s\n' "${SERVICE_PORT}"
    if lan_ip="$(detect_lan_ip)"; then
        printf '  局域网： http://%s:%s\n' "${lan_ip}" "${SERVICE_PORT}"
    else
        printf '  局域网： http://<宿主机局域网IP>:%s（未能自动检测 IP）\n' "${SERVICE_PORT}"
    fi
}
