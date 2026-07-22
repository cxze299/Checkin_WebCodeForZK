#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT_DIR/deploy/docker-compose.separated.yml}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-agp}"
ENV_FILE="${ENV_FILE:-$ROOT_DIR/.env}"

if [ -f "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi

MYSQL_DATABASE="${MYSQL_DATABASE:-agp}"
MYSQL_USER="${MYSQL_USER:-agp}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-}"
AGP_WEB_PORT="${AGP_WEB_PORT:-2973}"
AGP_MYSQL_PORT="${AGP_MYSQL_PORT:-3307}"
BOOTSTRAP_SUPERADMIN_USERNAME="${BOOTSTRAP_SUPERADMIN_USERNAME:-admin}"
BOOTSTRAP_SUPERADMIN_DISPLAY_NAME="${BOOTSTRAP_SUPERADMIN_DISPLAY_NAME:-超级管理员}"

PRIMARY_GROUP_CODE="${PRIMARY_GROUP_CODE:-}"
PRIMARY_GROUP_NAME="${PRIMARY_GROUP_NAME:-}"
PRIMARY_GROUP_DEFAULT_PASSWORD="${PRIMARY_GROUP_DEFAULT_PASSWORD:-}"
PRIMARY_NAME_MAP="${PRIMARY_NAME_MAP:-}"
PRIMARY_CONFIG_PATH="${PRIMARY_CONFIG_PATH:-$ROOT_DIR/config.json}"
PRIMARY_RECORDS_PATH="${PRIMARY_RECORDS_PATH:-$ROOT_DIR/data/records.json}"
MIGRATION_REPORT_DIR="${MIGRATION_REPORT_DIR:-$ROOT_DIR/data/migration-reports}"
RUN_PRIMARY_MIGRATION="${RUN_PRIMARY_MIGRATION:-auto}"
PRIMARY_ALLOW_DUPLICATE_AS_DELETED="${PRIMARY_ALLOW_DUPLICATE_AS_DELETED:-false}"
PRIMARY_FAIL_ON_GENERATED_USERNAMES="${PRIMARY_FAIL_ON_GENERATED_USERNAMES:-false}"
PRIMARY_DRY_RUN_ONLY="${PRIMARY_DRY_RUN_ONLY:-false}"
TMP_INPUT_DIR=""
MIGRATION_CONFIG_IN_CONTAINER=""
MIGRATION_RECORDS_IN_CONTAINER=""
MIGRATION_NAME_MAP_IN_CONTAINER=""

rand_hex() {
  local length="${1:-32}"
  local bytes=$(((length + 1) / 2))
  local value
  value="$(od -An -N "$bytes" -tx1 /dev/urandom | tr -d '[:space:]')"
  printf '%s' "${value:0:length}"
}

rand_password() {
  rand_hex "${1:-24}"
}

log() {
  printf '\n[%s] %s\n' "$(date '+%F %T')" "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "缺少命令: $1" >&2
    exit 1
  fi
}

compose() {
  docker compose --env-file "$ENV_FILE" -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
}

upsert_env_var() {
  local key="$1"
  local value="$2"
  local tmp="${ENV_FILE}.tmp.$$"
  awk -v key="$key" -v value="$value" '
    BEGIN { found = 0 }
    index($0, key "=") == 1 { print key "=" value; found = 1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "$ENV_FILE" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$ENV_FILE"
}

wait_for_mysql() {
  log "等待 MySQL 就绪"
  local retries=60
  local count=0
  until compose exec -T mysql mysqladmin ping -h 127.0.0.1 -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" >/dev/null 2>&1; do
    count=$((count + 1))
    if [ "$count" -ge "$retries" ]; then
      echo "MySQL 未在预期时间内就绪" >&2
      exit 1
    fi
    sleep 2
  done
}

wait_for_backend() {
  local retries=90
  local status
  while [ "$retries" -gt 0 ]; do
    status="$(docker inspect --format '{{.State.Health.Status}}' agp-backend 2>/dev/null || true)"
    if [ "$status" = "healthy" ]; then
      return 0
    fi
    sleep 2
    retries=$((retries - 1))
  done
  echo "backend 健康检查超时，最近日志：" >&2
  compose logs --tail=120 backend >&2 || true
  return 1
}

abs_path() {
  local target="$1"
  if [ -d "$target" ]; then
    (cd "$target" && pwd)
  else
    (cd "$(dirname "$target")" && printf '%s/%s\n' "$(pwd)" "$(basename "$target")")
  fi
}

prepare_migration_inputs() {
  TMP_INPUT_DIR="$ROOT_DIR/.tmp/migration-inputs"
  rm -rf "$TMP_INPUT_DIR"
  mkdir -p "$TMP_INPUT_DIR"
  cp "$(abs_path "$PRIMARY_CONFIG_PATH")" "$TMP_INPUT_DIR/config.json"
  cp "$(abs_path "$PRIMARY_RECORDS_PATH")" "$TMP_INPUT_DIR/records.json"
  MIGRATION_CONFIG_IN_CONTAINER="/workspace/.tmp/migration-inputs/config.json"
  MIGRATION_RECORDS_IN_CONTAINER="/workspace/.tmp/migration-inputs/records.json"
  if [ -n "$PRIMARY_NAME_MAP" ]; then
    cp "$(abs_path "$PRIMARY_NAME_MAP")" "$TMP_INPUT_DIR/name-map.json"
    MIGRATION_NAME_MAP_IN_CONTAINER="/workspace/.tmp/migration-inputs/name-map.json"
  else
    MIGRATION_NAME_MAP_IN_CONTAINER=""
  fi
}

cleanup() {
  if [ -n "$TMP_INPUT_DIR" ] && [ -d "$TMP_INPUT_DIR" ]; then
    rm -rf "$TMP_INPUT_DIR"
  fi
}

run_migrate_json() {
  local dry_run="$1"
  local dsn="${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(mysql:3306)/${MYSQL_DATABASE}?parseTime=true&multiStatements=false&charset=utf8mb4,utf8"
  local network_name="${COMPOSE_PROJECT_NAME}_default"
  local args=(
    "go" "run" "./cmd/migrate-json"
    "--dsn" "$dsn"
    "--group-code" "$PRIMARY_GROUP_CODE"
    "--group-name" "$PRIMARY_GROUP_NAME"
    "--config" "$MIGRATION_CONFIG_IN_CONTAINER"
    "--records" "$MIGRATION_RECORDS_IN_CONTAINER"
    "--default-password" "$PRIMARY_GROUP_DEFAULT_PASSWORD"
    "--report-dir" "/workspace/${MIGRATION_REPORT_DIR#$ROOT_DIR/}"
    "--dry-run=${dry_run}"
    "--allow-duplicate-as-deleted=${PRIMARY_ALLOW_DUPLICATE_AS_DELETED}"
    "--fail-on-generated-usernames=${PRIMARY_FAIL_ON_GENERATED_USERNAMES}"
  )
  if [ -n "$MIGRATION_NAME_MAP_IN_CONTAINER" ]; then
    args+=("--name-map" "$MIGRATION_NAME_MAP_IN_CONTAINER")
  fi
  docker run --rm \
    --network "$network_name" \
    -v "$ROOT_DIR:/workspace" \
    -w /workspace/backend \
    golang:1.25-bookworm \
    "${args[@]}"
}

should_run_primary_migration() {
  case "$RUN_PRIMARY_MIGRATION" in
    true) return 0 ;;
    false) return 1 ;;
    auto)
      [ -n "$PRIMARY_GROUP_CODE" ] && [ -n "$PRIMARY_GROUP_NAME" ] && [ -f "$PRIMARY_CONFIG_PATH" ] && [ -f "$PRIMARY_RECORDS_PATH" ]
      return
      ;;
    *)
      echo "RUN_PRIMARY_MIGRATION 仅支持 true/false/auto" >&2
      exit 1
      ;;
  esac
}

require_cmd docker
require_cmd awk
require_cmd od
mkdir -p "$ROOT_DIR/data/mysql" "$ROOT_DIR/data/assets" "$ROOT_DIR/data/backups/mysql" "$MIGRATION_REPORT_DIR"
trap cleanup EXIT

if [ -z "$MYSQL_PASSWORD" ] || [[ "$MYSQL_PASSWORD" == CHANGE_ME* ]]; then
  MYSQL_PASSWORD="$(rand_password 24)"
fi
if [ -z "$MYSQL_ROOT_PASSWORD" ] || [[ "$MYSQL_ROOT_PASSWORD" == CHANGE_ME* ]]; then
  MYSQL_ROOT_PASSWORD="$(rand_password 24)"
fi

if [ -z "${AGP_JWT_SECRET:-}" ] || [ "${#AGP_JWT_SECRET}" -lt 32 ] || [[ "$AGP_JWT_SECRET" == *CHANGE_ME* ]] || [ "$AGP_JWT_SECRET" = "please-change-this-to-a-long-random-string" ]; then
  AGP_JWT_SECRET="$(rand_hex 48)"
  export AGP_JWT_SECRET
fi

if [ -z "${BOOTSTRAP_SUPERADMIN_PASSWORD:-}" ] || [ "${#BOOTSTRAP_SUPERADMIN_PASSWORD}" -lt 12 ] || [[ "$BOOTSTRAP_SUPERADMIN_PASSWORD" == *CHANGE_ME* ]] || [ "$BOOTSTRAP_SUPERADMIN_PASSWORD" = "ChangeMe123" ]; then
  BOOTSTRAP_SUPERADMIN_PASSWORD="$(rand_password 24)"
  export BOOTSTRAP_SUPERADMIN_PASSWORD
fi

touch "$ENV_FILE"
chmod 600 "$ENV_FILE"
upsert_env_var MYSQL_DATABASE "$MYSQL_DATABASE"
upsert_env_var MYSQL_USER "$MYSQL_USER"
upsert_env_var MYSQL_PASSWORD "$MYSQL_PASSWORD"
upsert_env_var MYSQL_ROOT_PASSWORD "$MYSQL_ROOT_PASSWORD"
upsert_env_var AGP_JWT_SECRET "$AGP_JWT_SECRET"
upsert_env_var BOOTSTRAP_SUPERADMIN_USERNAME "$BOOTSTRAP_SUPERADMIN_USERNAME"
upsert_env_var BOOTSTRAP_SUPERADMIN_PASSWORD "$BOOTSTRAP_SUPERADMIN_PASSWORD"
upsert_env_var BOOTSTRAP_SUPERADMIN_DISPLAY_NAME "$BOOTSTRAP_SUPERADMIN_DISPLAY_NAME"
upsert_env_var AGP_WEB_PORT "$AGP_WEB_PORT"
upsert_env_var AGP_MYSQL_PORT "$AGP_MYSQL_PORT"

if should_run_primary_migration; then
  if [ -z "$PRIMARY_GROUP_DEFAULT_PASSWORD" ]; then
    PRIMARY_GROUP_DEFAULT_PASSWORD="$(rand_password 12)"
  fi
  if [ ! -f "$PRIMARY_CONFIG_PATH" ]; then
    echo "迁移配置文件不存在: $PRIMARY_CONFIG_PATH" >&2
    exit 1
  fi
  if [ ! -f "$PRIMARY_RECORDS_PATH" ]; then
    echo "迁移记录文件不存在: $PRIMARY_RECORDS_PATH" >&2
    exit 1
  fi
  if [ -n "$PRIMARY_NAME_MAP" ] && [ ! -f "$PRIMARY_NAME_MAP" ]; then
    echo "姓名映射文件不存在: $PRIMARY_NAME_MAP" >&2
    exit 1
  fi
  prepare_migration_inputs
fi

export COMPOSE_PROJECT_NAME MYSQL_DATABASE MYSQL_USER MYSQL_PASSWORD MYSQL_ROOT_PASSWORD
export AGP_WEB_PORT AGP_MYSQL_PORT AGP_JWT_SECRET BOOTSTRAP_SUPERADMIN_USERNAME BOOTSTRAP_SUPERADMIN_PASSWORD BOOTSTRAP_SUPERADMIN_DISPLAY_NAME

log "启动 AGP 服务栈"
compose up -d --build
wait_for_mysql
wait_for_backend

if should_run_primary_migration; then
  log "执行首个小组 dry-run 迁移"
  run_migrate_json true
  if [ "$PRIMARY_DRY_RUN_ONLY" != "true" ]; then
    log "执行首个小组正式迁移"
    run_migrate_json false
  else
    log "已按 PRIMARY_DRY_RUN_ONLY=true 跳过正式迁移"
  fi
else
  log "未检测到首组迁移参数，跳过数据迁移"
fi

cat <<EOF

部署完成。

访问地址:
  前端: http://127.0.0.1:${AGP_WEB_PORT}
  MySQL: 127.0.0.1:${AGP_MYSQL_PORT}

超级管理员:
  用户名: ${BOOTSTRAP_SUPERADMIN_USERNAME}
  显示名: ${BOOTSTRAP_SUPERADMIN_DISPLAY_NAME}
  密码及数据库密钥已安全写入: ${ENV_FILE}

EOF

if should_run_primary_migration; then
  cat <<EOF
首组迁移:
  group_code: ${PRIMARY_GROUP_CODE}
  group_name: ${PRIMARY_GROUP_NAME}
  imported_default_password: ${PRIMARY_GROUP_DEFAULT_PASSWORD}
  report_dir: ${MIGRATION_REPORT_DIR}

EOF
fi
