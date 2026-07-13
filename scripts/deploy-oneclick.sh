#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT_DIR/deploy/docker-compose.separated.yml}"
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-agp}"

MYSQL_DATABASE="${MYSQL_DATABASE:-agp}"
MYSQL_USER="${MYSQL_USER:-agp}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-agp}"
MYSQL_ROOT_PASSWORD="${MYSQL_ROOT_PASSWORD:-agp-root}"
AGP_WEB_PORT="${AGP_WEB_PORT:-5112}"
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
  LC_ALL=C tr -dc 'a-f0-9' </dev/urandom | head -c "${1:-32}"
}

rand_password() {
  LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${1:-16}"
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
  docker compose -p "$COMPOSE_PROJECT_NAME" -f "$COMPOSE_FILE" "$@"
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
  local dsn="agp:agp@tcp(mysql:3306)/${MYSQL_DATABASE}?parseTime=true&multiStatements=false&charset=utf8mb4,utf8"
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
mkdir -p "$ROOT_DIR/data/mysql" "$ROOT_DIR/data/assets" "$ROOT_DIR/data/backups/mysql" "$MIGRATION_REPORT_DIR"
trap cleanup EXIT

if [ -z "${AGP_JWT_SECRET:-}" ]; then
  AGP_JWT_SECRET="$(rand_hex 48)"
  export AGP_JWT_SECRET
fi

if [ -z "${BOOTSTRAP_SUPERADMIN_PASSWORD:-}" ]; then
  BOOTSTRAP_SUPERADMIN_PASSWORD="$(rand_password 16)"
  export BOOTSTRAP_SUPERADMIN_PASSWORD
fi

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
  密码: ${BOOTSTRAP_SUPERADMIN_PASSWORD}

数据库:
  DB: ${MYSQL_DATABASE}
  User: ${MYSQL_USER}
  Password: ${MYSQL_PASSWORD}

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
