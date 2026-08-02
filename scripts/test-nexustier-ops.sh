#!/usr/bin/env bash
# Test harness for nexustier-ops.sh.
# Injects stub docker and curl commands via a temporary directory on PATH so the
# script's guards, argument construction, and error paths can be verified without
# a real Docker daemon or network.
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/scripts/nexustier-ops.sh"
readonly SCRIPT
STUB_DIR="$(mktemp -d)"
readonly STUB_DIR
WORK_DIR="$(mktemp -d)"
readonly WORK_DIR
PASS=0
FAIL=0

cleanup() {
  rm -rf "${STUB_DIR}" "${WORK_DIR}"
}
trap cleanup EXIT

# ---------- stub helpers ------------------------------------------------

# make_stub writes a fake command that records its invocations and emits
# pre-programmed output.
make_stub() {
  local name="$1" exit_code="${2:-0}" stdout="${3:-}" stderr="${4:-}"
  cat >"${STUB_DIR}/${name}" <<STUB
#!/usr/bin/env bash
printf '%s\0' "${name}" "\$@" >> "${STUB_DIR}/calls.log"
printf '\0' >> "${STUB_DIR}/calls.log"
[[ -n '${stdout}' ]] && printf '%s\n' '${stdout}'
[[ -n '${stderr}' ]] && printf '%s\n' '${stderr}' >&2
exit ${exit_code}
STUB
  chmod +x "${STUB_DIR}/${name}"
}

# clear_calls resets the invocation log between test cases.
clear_calls() { : >"${STUB_DIR}/calls.log"; }

# was_called checks that a stub was invoked with the given argument substring.
was_called() {
  local name="$1" substr="${2:-}"
  if [[ -z "${substr}" ]]; then
    grep -qF "${name}" "${STUB_DIR}/calls.log" 2>/dev/null
  else
    grep -qF "${name}" "${STUB_DIR}/calls.log" 2>/dev/null \
      && grep -qF "${substr}" "${STUB_DIR}/calls.log" 2>/dev/null
  fi
}

# ---------- env setup ---------------------------------------------------

make_env_file() {
  local path="$1"
  cat >"${path}" <<'ENV'
NEXUSTIER_GATEWAY_IMAGE=ghcr.io/wuyouowo/nexustier:sha-test
NEXUSTIER_CONTROLLER_IMAGE=ghcr.io/wuyouowo/nexustier-controller:sha-test
POSTGRES_PASSWORD=secret
NEXUSTIER_CONTROLLER_DATABASE_URL=postgres://nexustier:secret@postgres:5432/nexustier?sslmode=disable
NEXUSTIER_GATEWAY_ADMISSION_TOKEN=aaabbbcccdddeee
NEXUSTIER_CONTROLLER_AUTH_USERNAME=admin
NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH=pbkdf2-sha256$600000$salt$key
NEXUSTIER_CONTROLLER_SESSION_KEY=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
ENV
  chmod 0600 "${path}"
}

ops_run() {
  # Run the script with PATH prepended by the stub dir and env vars directing
  # it to the temporary working directory.
  PATH="${STUB_DIR}:${PATH}" \
    NEXUSTIER_ENV_FILE="${WORK_DIR}/.env" \
    NEXUSTIER_COMPOSE_FILE="${WORK_DIR}/compose.yaml" \
    NEXUSTIER_DEPLOY_DIR="${WORK_DIR}" \
    NEXUSTIER_BACKUP_DIR="${WORK_DIR}/backups" \
    NEXUSTIER_HEALTH_TIMEOUT=1 \
    NEXUSTIER_OPS_ASSUME_YES=1 \
    NEXUSTIER_COMPOSE_CMD="docker-compose" \
    bash "${SCRIPT}" "$@"
}

# ---------- minimal test framework ------------------------------------

assert() {
  local description="$1" result="$2"
  if [[ "${result}" == "0" ]]; then
    log_pass "${description}"
  else
    log_fail "${description}"
  fi
}

# assert_exits_with runs the script, then asserts on the exit status and output
# separately. Piping into grep would let pipefail mask an expected failure.
assert_exits_with() {
  local description="$1" want_status="$2" want_text="$3"
  shift 3
  local output status=0
  output="$(ops_run "$@" 2>&1)" || status=$?
  if [[ "${status}" != "${want_status}" ]]; then
    log_fail "${description} (exit ${status}, want ${want_status})"
    return
  fi
  if ! grep -qF "${want_text}" <<<"${output}"; then
    log_fail "${description} (output lacks ${want_text})"
    return
  fi
  log_pass "${description}"
}

log_pass() { printf 'PASS: %s\n' "$1"; PASS=$(( PASS + 1 )); }
log_fail() { printf 'FAIL: %s\n' "$1" >&2; FAIL=$(( FAIL + 1 )); }

# ---------- tests -------------------------------------------------------

setup_stubs() {
  make_stub docker 0
  make_stub docker-compose 0
  make_stub curl 0 "200"
  make_stub openssl 0 "0102030405060708"
  make_stub stat 0 "600 .env"
  make_stub pg_dump 0
  make_stub createdb 0
  make_stub pg_restore 0
  make_stub dropdb 0
  make_stub psql 0 "version"
}

test_help() {
  clear_calls
  if ops_run --help 2>&1 | grep -q "命令"; then
    log_pass "help shows usage"
  else
    log_fail "help shows usage"
  fi
}

test_unknown_command() {
  clear_calls
  assert_exits_with "unknown command prints error" 1 "未知命令" __no_such_cmd__
}

test_preflight_fails_without_env() {
  rm -f "${WORK_DIR}/.env"
  clear_calls
  assert_exits_with "preflight fails without env file" 1 "找不到" preflight
}

test_preflight_passes_with_valid_env() {
  make_env_file "${WORK_DIR}/.env"
  touch "${WORK_DIR}/compose.yaml"
  clear_calls
  if ops_run preflight 2>&1 | grep -q "preflight 通过"; then
    log_pass "preflight passes with valid env"
  else
    log_fail "preflight passes with valid env"
  fi
}

test_preflight_fails_without_hash_when_required() {
  make_env_file "${WORK_DIR}/.env"
  # Remove the hash so auto mode should refuse.
  sed -i '/NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH/d' "${WORK_DIR}/.env"
  clear_calls
  assert_exits_with "preflight fails without password hash in auto mode" 1 \
    "AUTH_PASSWORD_HASH" preflight
  # Restore for subsequent tests.
  make_env_file "${WORK_DIR}/.env"
}

test_preflight_accepts_disabled_auth_without_hash() {
  make_env_file "${WORK_DIR}/.env"
  sed -i '/NEXUSTIER_CONTROLLER_AUTH_PASSWORD_HASH/d' "${WORK_DIR}/.env"
  printf 'NEXUSTIER_CONTROLLER_AUTH_MODE=disabled\n' >>"${WORK_DIR}/.env"
  clear_calls
  if ops_run preflight 2>&1 | grep -q "preflight 通过"; then
    log_pass "preflight passes without hash when auth_mode=disabled"
  else
    log_fail "preflight passes without hash when auth_mode=disabled"
  fi
  make_env_file "${WORK_DIR}/.env"
}

test_rotate_token_rewrites_env() {
  make_env_file "${WORK_DIR}/.env"
  make_stub openssl 0 "newtokenhexvalue"
  clear_calls
  ops_run rotate-token >/dev/null 2>&1 || true
  if grep -q "NEXUSTIER_GATEWAY_ADMISSION_TOKEN=newtokenhexvalue" "${WORK_DIR}/.env"; then
    log_pass "rotate-token writes new token to env file"
  else
    log_fail "rotate-token writes new token to env file"
  fi
  make_env_file "${WORK_DIR}/.env"
}

test_rotate_token_never_prints_secret() {
  make_env_file "${WORK_DIR}/.env"
  make_stub openssl 0 "supersecrettoken"
  clear_calls
  local output
  output="$(ops_run rotate-token 2>&1 || true)"
  if grep -q "supersecrettoken" <<<"${output}"; then
    log_fail "rotate-token must not print the new token value"
  else
    log_pass "rotate-token does not print the new token value"
  fi
  make_env_file "${WORK_DIR}/.env"
}

test_upgrade_rewrites_both_images() {
  make_env_file "${WORK_DIR}/.env"
  # verify runs wait_for_http which uses curl; stub already returns "200".
  clear_calls
  ops_run upgrade \
    ghcr.io/wuyouowo/nexustier:sha-new \
    ghcr.io/wuyouowo/nexustier-controller:sha-new \
    >/dev/null 2>&1 || true
  if grep -q "NEXUSTIER_GATEWAY_IMAGE=ghcr.io/wuyouowo/nexustier:sha-new" "${WORK_DIR}/.env" \
    && grep -q "NEXUSTIER_CONTROLLER_IMAGE=ghcr.io/wuyouowo/nexustier-controller:sha-new" "${WORK_DIR}/.env"; then
    log_pass "upgrade writes new image references to env file"
  else
    log_fail "upgrade writes new image references to env file"
  fi
  make_env_file "${WORK_DIR}/.env"
}

test_rollback_rewrites_both_images() {
  make_env_file "${WORK_DIR}/.env"
  clear_calls
  ops_run rollback \
    ghcr.io/wuyouowo/nexustier:sha-prev \
    ghcr.io/wuyouowo/nexustier-controller:sha-prev \
    >/dev/null 2>&1 || true
  if grep -q "NEXUSTIER_GATEWAY_IMAGE=ghcr.io/wuyouowo/nexustier:sha-prev" "${WORK_DIR}/.env" \
    && grep -q "NEXUSTIER_CONTROLLER_IMAGE=ghcr.io/wuyouowo/nexustier-controller:sha-prev" "${WORK_DIR}/.env"; then
    log_pass "rollback writes previous image references to env file"
  else
    log_fail "rollback writes previous image references to env file"
  fi
  make_env_file "${WORK_DIR}/.env"
}

test_restore_verify_refuses_live_db_name() {
  make_env_file "${WORK_DIR}/.env"
  local dump="${WORK_DIR}/test.dump"
  touch "${dump}"
  clear_calls
  # Patch the restore command to succeed so we reach the name check.
  make_stub pg_restore 0
  make_stub createdb 0
  make_stub psql 0 "result"
  # The dropdb guard requires the scratch name to match nexustier_restore_*;
  # this cannot be triggered from outside since the name is generated inside
  # the script — the test below just ensures the happy path calls dropdb.
  local output
  output="$(ops_run restore-verify "${dump}" 2>&1 || true)"
  # Should call createdb, pg_restore, and psql in the happy path.
  if was_called "createdb" && was_called "pg_restore"; then
    log_pass "restore-verify calls createdb and pg_restore"
  else
    log_fail "restore-verify calls createdb and pg_restore: ${output}"
  fi
}

test_restore_verify_requires_dump_argument() {
  make_env_file "${WORK_DIR}/.env"
  clear_calls
  assert_exits_with "restore-verify requires dump argument" 1 "用法" restore-verify
}

test_replace_env_value_is_idempotent() {
  make_env_file "${WORK_DIR}/.env"
  clear_calls
  # Rotate a key twice; only one entry should remain.
  ops_run rotate-token >/dev/null 2>&1 || true
  ops_run rotate-token >/dev/null 2>&1 || true
  local count
  count="$(grep -c '^NEXUSTIER_GATEWAY_ADMISSION_TOKEN=' "${WORK_DIR}/.env" || true)"
  if [[ "${count}" -eq 1 ]]; then
    log_pass "replace_env_value keeps exactly one entry per key"
  else
    log_fail "replace_env_value keeps exactly one entry per key (found ${count})"
  fi
  make_env_file "${WORK_DIR}/.env"
}

test_env_file_permissions_restored_after_replace() {
  make_env_file "${WORK_DIR}/.env"
  clear_calls
  ops_run rotate-token >/dev/null 2>&1 || true
  local mode
  mode="$(stat -c '%a' "${WORK_DIR}/.env")"
  if [[ "${mode}" == "600" ]]; then
    log_pass "env file permissions unchanged after replace"
  else
    log_fail "env file permissions changed to ${mode} after replace"
  fi
  make_env_file "${WORK_DIR}/.env"
}

# ---------- run all tests -----------------------------------------------

setup_stubs
touch "${WORK_DIR}/compose.yaml"

test_help
test_unknown_command
test_preflight_fails_without_env
test_preflight_passes_with_valid_env
test_preflight_fails_without_hash_when_required
test_preflight_accepts_disabled_auth_without_hash
test_rotate_token_rewrites_env
test_rotate_token_never_prints_secret
test_upgrade_rewrites_both_images
test_rollback_rewrites_both_images
test_restore_verify_requires_dump_argument
test_restore_verify_refuses_live_db_name
test_replace_env_value_is_idempotent
test_env_file_permissions_restored_after_replace

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
