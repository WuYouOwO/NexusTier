#!/usr/bin/env bash
# NexusTier Compose operations helper: repeatable deployment, verification,
# backup/restore checks, credential rotation, and upgrade/rollback.
#
# Every destructive step asks for confirmation. This script never deletes a
# PostgreSQL volume and never echoes a secret.
set -euo pipefail

readonly SCRIPT_NAME="${0##*/}"

COMPOSE_FILE="${NEXUSTIER_COMPOSE_FILE:-compose.example.yaml}"
ENV_FILE="${NEXUSTIER_ENV_FILE:-.env}"
BACKUP_DIR="${NEXUSTIER_BACKUP_DIR:-backups}"
GATEWAY_URL="${NEXUSTIER_GATEWAY_HEALTH_URL:-http://127.0.0.1:11211}"
CONTROLLER_URL="${NEXUSTIER_CONTROLLER_HEALTH_URL:-http://127.0.0.1:8080}"
HEALTH_TIMEOUT="${NEXUSTIER_HEALTH_TIMEOUT:-120}"

COMPOSE_BASE=()
TEMP_FILES=()

log()  { printf '%s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

cleanup() {
  local file
  for file in ${TEMP_FILES[@]+"${TEMP_FILES[@]}"}; do
    [[ -e "${file}" ]] && rm -f -- "${file}"
  done
}
trap cleanup EXIT

# track_temp registers a path for removal even when the script exits early, so a
# cookie jar or rewritten env file never outlives the run.
track_temp() { TEMP_FILES+=("$1"); }

confirm() {
  local prompt="$1" reply
  if [[ -n "${NEXUSTIER_OPS_ASSUME_YES:-}" ]]; then
    log "${prompt} [assuming yes]"
    return 0
  fi
  read -r -p "${prompt} 输入 yes 继续: " reply
  [[ "${reply}" == "yes" ]] || fail "已取消"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少命令: $1"
}

detect_compose() {
  if [[ -n "${NEXUSTIER_COMPOSE_CMD:-}" ]]; then
    read -r -a COMPOSE_BASE <<<"${NEXUSTIER_COMPOSE_CMD}"
  elif docker compose version >/dev/null 2>&1; then
    COMPOSE_BASE=(docker compose)
  elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_BASE=(docker-compose)
  else
    fail "未找到 docker compose 或 docker-compose"
  fi
}

compose() {
  "${COMPOSE_BASE[@]}" --env-file "${ENV_FILE}" -f "${COMPOSE_FILE}" "$@"
}

require_env_file() {
  [[ -f "${ENV_FILE}" ]] || fail "找不到 ${ENV_FILE}；参考部署指南第 6 节创建"
  local mode
  mode="$(stat -c '%a' "${ENV_FILE}")"
  [[ "${mode}" == "600" ]] || warn "${ENV_FILE} 权限为 ${mode}，建议 600"
}

# read_env_value prints one value without sourcing the file, so a malformed line
# cannot execute anything.
read_env_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "${ENV_FILE}" | tail -n 1
}

# replace_env_value rewrites one key in place through a private temp file and
# preserves the original permissions.
replace_env_value() {
  local key="$1" value="$2" temp
  temp="$(mktemp "${ENV_FILE}.XXXXXX")"
  track_temp "${temp}"
  chmod 0600 "${temp}"
  grep -v "^${key}=" "${ENV_FILE}" >"${temp}" || true
  printf '%s=%s\n' "${key}" "${value}" >>"${temp}"
  cat "${temp}" >"${ENV_FILE}"
  rm -f -- "${temp}"
}

http_status() {
  curl --silent --show-error --output /dev/null --write-out '%{http_code}' "$1"
}

wait_for_http() {
  local url="$1" expected="$2" label="$3" deadline status
  deadline=$(( $(date +%s) + HEALTH_TIMEOUT ))
  while :; do
    status="$(http_status "${url}" || true)"
    if [[ "${status}" == "${expected}" ]]; then
      log "  ${label}: ${status}"
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      fail "${label} 在 ${HEALTH_TIMEOUT}s 内未返回 ${expected}（最近 ${status:-无响应}）"
    fi
    sleep 3
  done
}

cmd_preflight() {
  require_command curl
  require_command stat
  require_env_file
  local key
  for key in NEXUSTIER_CONTROLLER_DATABASE_URL POSTGRES_PASSWORD \
    NEXUSTIER_GATEWAY_ADMISSION_TOKEN NEXUSTIER_GATEWAY_IMAGE \
    NEXUSTIER_CONTROLLER_IMAGE; do
    [[ -n "$(read_env_value "${key}")" ]] || fail "${ENV_FILE} 缺少 ${key}"
  done
  # Compose publishes the controller on 0.0.0.0:8080 inside the container, which
  # is non-loopback, so auto mode refuses to start without a password hash.
  if [[ -z "$(read_env_value NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH)" \
     && "$(read_env_value NEXUSTIER_CONTROLLER_AUTH_MODE)" != "disabled" ]]; then
    fail "缺少 NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH；运行 ${SCRIPT_NAME} rotate-console-password 生成"
  fi
  compose config --quiet
  log "preflight 通过"
}

cmd_deploy() {
  cmd_preflight
  compose pull
  compose up -d
  cmd_verify
}

cmd_verify() {
  require_command curl
  log "检查健康端点"
  wait_for_http "${GATEWAY_URL}/healthz" 200 "gateway /healthz"
  wait_for_http "${CONTROLLER_URL}/healthz" 200 "controller /healthz"
  wait_for_http "${CONTROLLER_URL}/readyz" 200 "controller /readyz"

  # Without a session /v1/* must answer 401. Anything else means the guard is
  # missing, which is the failure this check exists to catch.
  local status
  status="$(http_status "${CONTROLLER_URL}/v1/topology" || true)"
  case "${status}" in
    401) log "  controller /v1/topology: 401（认证已启用）" ;;
    200) warn "controller /v1/topology 返回 200，认证未启用；确认这是有意的 auth_mode=disabled" ;;
    *)   fail "controller /v1/topology 返回 ${status:-无响应}" ;;
  esac
  log "verify 通过"
}

# cmd_login_verify proves the operator credential works and the topology API is
# reachable with a session. The password is read from stdin, never as an argument.
cmd_login_verify() {
  require_command curl
  local username jar password status
  username="$(read_env_value NEXUSTIER_CONTROLLER_AUTH_USERNAME)"
  username="${username:-admin}"
  jar="$(mktemp)"
  track_temp "${jar}"
  chmod 0600 "${jar}"

  read -r -s -p "控制台口令（${username}）: " password && echo
  [[ -n "${password}" ]] || fail "口令不能为空"

  status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
    --cookie-jar "${jar}" \
    --data-urlencode "username=${username}" \
    --data-urlencode "password=${password}" \
    "${CONTROLLER_URL}/login" || true)"
  password=""
  [[ "${status}" == "303" ]] || fail "登录失败，HTTP ${status:-无响应}"

  status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
    --cookie "${jar}" "${CONTROLLER_URL}/v1/topology" || true)"
  [[ "${status}" == "200" ]] || fail "带会话访问 /v1/topology 返回 ${status:-无响应}"
  log "login-verify 通过：登录成功且 /v1/topology 可读"
}

cmd_backup() {
  require_env_file
  mkdir -p "${BACKUP_DIR}"
  chmod 0700 "${BACKUP_DIR}"
  local target
  target="${BACKUP_DIR}/nexustier-$(date +%Y%m%d-%H%M%S).dump"

  # Stopping the controller gives the dump a quiescent writer. The gateway keeps
  # serving clients, so this is not a full outage.
  log "暂停 controller 以取得一致快照"
  compose stop controller
  if ! compose exec -T postgres pg_dump -U nexustier -d nexustier --format=custom >"${target}"; then
    compose start controller
    rm -f -- "${target}"
    fail "pg_dump 失败，controller 已重新启动"
  fi
  compose start controller
  chmod 0600 "${target}"
  [[ -s "${target}" ]] || fail "备份文件为空: ${target}"
  log "备份完成: ${target} ($(stat -c '%s' "${target}") 字节)"
}

cmd_restore_verify() {
  local dump="${1:-}"
  [[ -n "${dump}" ]] || fail "用法: ${SCRIPT_NAME} restore-verify <dump 文件>"
  [[ -f "${dump}" ]] || fail "找不到 ${dump}"
  local scratch
  scratch="nexustier_restore_$(date +%Y%m%d%H%M%S)"

  log "恢复到临时库 ${scratch}"
  compose exec -T postgres createdb -U nexustier "${scratch}"
  if ! compose exec -T postgres pg_restore -U nexustier -d "${scratch}" \
    --no-owner --exit-on-error <"${dump}"; then
    warn "pg_restore 失败，保留 ${scratch} 供排查"
    fail "恢复验证失败"
  fi
  compose exec -T postgres psql -U nexustier -d "${scratch}" \
    -c 'SELECT version, applied_at FROM schema_migrations ORDER BY version;'
  compose exec -T postgres psql -U nexustier -d "${scratch}" \
    -c 'SELECT count(*) AS machines FROM machines;'

  # Guard the name before dropping so this can never target the live database.
  [[ "${scratch}" == nexustier_restore_* ]] || fail "拒绝删除非临时库 ${scratch}"
  confirm "恢复验证通过。删除临时库 ${scratch}?"
  compose exec -T postgres dropdb -U nexustier "${scratch}"
  log "restore-verify 通过"
}

cmd_rotate_token() {
  require_env_file
  require_command openssl
  warn "轮换准入 Token 后，所有 EasyTier 客户端都必须更新配置服务器地址才能重新注册"
  confirm "继续轮换 NEXUSTIER_GATEWAY_ADMISSION_TOKEN?"
  local token
  token="$(openssl rand -hex 32)"
  replace_env_value NEXUSTIER_GATEWAY_ADMISSION_TOKEN "${token}"
  token=""
  compose up -d --force-recreate gateway
  wait_for_http "${GATEWAY_URL}/healthz" 200 "gateway /healthz"
  log "Token 已轮换。新值只写入 ${ENV_FILE}，未打印到终端。"
  log "读取新值: sed -n 's/^NEXUSTIER_GATEWAY_ADMISSION_TOKEN=//p' ${ENV_FILE}"
}

cmd_rotate_console_password() {
  require_env_file
  # Hashing runs in a one-off container, so this needs the plain docker CLI even
  # when the deployment drives compose through docker-compose.
  require_command docker
  local image password hash
  image="$(read_env_value NEXUSTIER_CONTROLLER_IMAGE)"
  [[ -n "${image}" ]] || fail "${ENV_FILE} 缺少 NEXUSTIER_CONTROLLER_IMAGE"

  read -r -s -p "新的控制台口令: " password && echo
  [[ -n "${password}" ]] || fail "口令不能为空"
  local confirm_password
  read -r -s -p "再次输入确认: " confirm_password && echo
  [[ "${password}" == "${confirm_password}" ]] || fail "两次输入不一致"
  confirm_password=""

  # The plaintext travels on stdin only, so it never reaches the process list.
  hash="$(printf '%s\n' "${password}" | docker run --rm -i "${image}" -hash-password)"
  password=""
  [[ "${hash}" == pbkdf2-sha256\$* ]] || fail "生成的哈希格式不符合预期"

  replace_env_value NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH "${hash}"
  compose up -d --force-recreate controller
  wait_for_http "${CONTROLLER_URL}/healthz" 200 "controller /healthz"
  log "口令已更新。现有会话在 TTL 到期前仍有效；如需立即失效，同时轮换 NEXUSTIER_CONTROLLER_SESSION_KEY。"
}

cmd_upgrade() {
  local gateway_image="${1:-}" controller_image="${2:-}"
  [[ -n "${gateway_image}" && -n "${controller_image}" ]] \
    || fail "用法: ${SCRIPT_NAME} upgrade <gateway 镜像> <controller 镜像>"
  require_env_file

  local previous_gateway previous_controller
  previous_gateway="$(read_env_value NEXUSTIER_GATEWAY_IMAGE)"
  previous_controller="$(read_env_value NEXUSTIER_CONTROLLER_IMAGE)"
  log "当前镜像:"
  log "  gateway    ${previous_gateway}"
  log "  controller ${previous_controller}"
  log "目标镜像:"
  log "  gateway    ${gateway_image}"
  log "  controller ${controller_image}"
  warn "升级会执行新的数据库 migration，且当前没有自动 down migration"
  confirm "已备份数据库并审核过 migration?"

  replace_env_value NEXUSTIER_GATEWAY_IMAGE "${gateway_image}"
  replace_env_value NEXUSTIER_CONTROLLER_IMAGE "${controller_image}"
  compose config --quiet
  compose pull gateway controller
  compose up -d gateway controller
  cmd_verify
  log "升级完成。回滚命令:"
  log "  ${SCRIPT_NAME} rollback ${previous_gateway} ${previous_controller}"
}

cmd_rollback() {
  local gateway_image="${1:-}" controller_image="${2:-}"
  [[ -n "${gateway_image}" && -n "${controller_image}" ]] \
    || fail "用法: ${SCRIPT_NAME} rollback <gateway 镜像> <controller 镜像>"
  require_env_file
  warn "若新版本已应用 migration，旧 Controller 必须兼容当前 schema"
  confirm "确认回滚到 ${gateway_image} 与 ${controller_image}?"
  replace_env_value NEXUSTIER_GATEWAY_IMAGE "${gateway_image}"
  replace_env_value NEXUSTIER_CONTROLLER_IMAGE "${controller_image}"
  compose config --quiet
  compose pull gateway controller
  compose up -d gateway controller
  cmd_verify
  log "回滚完成"
}

usage() {
  cat <<USAGE
用法: ${SCRIPT_NAME} <命令> [参数]

命令:
  preflight                             校验 ${ENV_FILE}、必需变量与 compose 定义
  deploy                                preflight + pull + up -d + verify
  verify                                检查健康端点，并确认 /v1/* 需要会话
  login-verify                          用运维口令登录并读取一次 /v1/topology
  backup                                暂停 controller 后创建 pg_dump 备份
  restore-verify <dump>                 恢复到临时库、校验 migration 后删除
  rotate-token                          轮换 Gateway 准入 Token 并重建 gateway
  rotate-console-password               重新生成控制台口令哈希并重建 controller
  upgrade <gateway 镜像> <controller 镜像>   固定新镜像、重建并验证
  rollback <gateway 镜像> <controller 镜像>  恢复到指定镜像并验证

环境变量:
  NEXUSTIER_COMPOSE_FILE     默认 compose.example.yaml
  NEXUSTIER_ENV_FILE         默认 .env
  NEXUSTIER_BACKUP_DIR       默认 backups
  NEXUSTIER_COMPOSE_CMD      覆盖 compose 基础命令
  NEXUSTIER_HEALTH_TIMEOUT   健康检查等待秒数，默认 120
  NEXUSTIER_OPS_ASSUME_YES   非空时跳过交互确认，仅用于自动化
USAGE
}

main() {
  local command="${1:-}"
  [[ -n "${command}" ]] || { usage; exit 1; }
  shift

  case "${command}" in
    -h|--help|help) usage; return 0 ;;
  esac

  cd "${NEXUSTIER_DEPLOY_DIR:-.}"
  detect_compose

  case "${command}" in
    preflight)                cmd_preflight ;;
    deploy)                   cmd_deploy ;;
    verify)                   cmd_verify ;;
    login-verify)             cmd_login_verify ;;
    backup)                   cmd_backup ;;
    restore-verify)           cmd_restore_verify "$@" ;;
    rotate-token)             cmd_rotate_token ;;
    rotate-console-password)  cmd_rotate_console_password ;;
    upgrade)                  cmd_upgrade "$@" ;;
    rollback)                 cmd_rollback "$@" ;;
    *)                        usage; fail "未知命令: ${command}" ;;
  esac
}

main "$@"
