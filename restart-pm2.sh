#!/usr/bin/env bash
# restart-pm2.sh stops CLIProxyAPI, rebuilds the local binary, and restarts it under PM2.
set -euo pipefail

APP_NAME="${APP_NAME:-cli-proxy-api}"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_PATH="${CONFIG_PATH:-$PROJECT_ROOT/config.yaml}"
OUTPUT_PATH="${OUTPUT_PATH:-$PROJECT_ROOT/cli-proxy-api}"
ECOSYSTEM_FILE="${ECOSYSTEM_FILE:-$PROJECT_ROOT/ecosystem.config.cjs}"
HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-30}"
CGO_ENABLED="${CGO_ENABLED:-1}"
TEMP_PATH="$OUTPUT_PATH.tmp"
OUTPUT_NAME="$(basename "$OUTPUT_PATH")"

if [[ ! -f "$ECOSYSTEM_FILE" ]]; then
    echo "PM2 ecosystem file not found: $ECOSYSTEM_FILE" >&2
    exit 1
fi

if [[ ! -f "$CONFIG_PATH" ]]; then
    echo "Config file not found: $CONFIG_PATH" >&2
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "Go is not available in PATH" >&2
    exit 1
fi

if ! command -v pm2 >/dev/null 2>&1; then
    echo "PM2 is not available in PATH" >&2
    exit 1
fi

get_service_port() {
    local port
    port="$(sed -n -E 's/^[[:space:]]*port:[[:space:]]*([0-9]+).*/\1/p' "$CONFIG_PATH" | head -n 1)"
    printf '%s' "${port:-8317}"
}

stop_registered_process() {
    local registered=0
    if pm2 describe "$APP_NAME" >/dev/null 2>&1; then
        registered=1
        echo "Stopping PM2 process: $APP_NAME"
        pm2 stop "$APP_NAME"
    fi
    printf '%s' "$registered"
}

stop_standalone_process() {
    local port
    port="$(get_service_port)"

    if ! command -v lsof >/dev/null 2>&1; then
        return
    fi

    local pids pid command_path
    pids="$(lsof -ti "tcp:$port" -sTCP:LISTEN 2>/dev/null || true)"
    while IFS= read -r pid; do
        [[ -n "$pid" ]] || continue
        command_path="$(ps -p "$pid" -o command= 2>/dev/null | awk '{print $1}' || true)"
        case "$command_path" in
            "$OUTPUT_PATH"|*"/$OUTPUT_NAME")
                echo "Stopping standalone process: $command_path (PID $pid)"
                kill "$pid" 2>/dev/null || continue
                sleep 1
                if kill -0 "$pid" 2>/dev/null; then
                    kill -9 "$pid" 2>/dev/null || true
                fi
                ;;
        esac
    done <<< "$pids"
}

registered="$(stop_registered_process)"
stop_standalone_process

rm -f "$TEMP_PATH"

build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
version="dev"
if version_output="$(git -C "$PROJECT_ROOT" describe --tags --always 2>/dev/null)"; then
    version="$(printf '%s\n' "$version_output" | head -n 1)"
fi

commit="none"
if commit_output="$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null)"; then
    commit="$(printf '%s\n' "$commit_output" | head -n 1)"
fi

ldflags="-s -w -X main.Version=$version -X main.Commit=$commit -X main.BuildDate=$build_date"

echo "Building $OUTPUT_NAME $version ($commit)"
if ! CGO_ENABLED="$CGO_ENABLED" go build -C "$PROJECT_ROOT" -trimpath -ldflags "$ldflags" -o "$TEMP_PATH" ./cmd/server; then
    if [[ "$registered" == "1" ]]; then
        echo "Build failed; restarting the previous PM2 process" >&2
        pm2 restart "$APP_NAME" --update-env
    fi
    echo "Go build failed" >&2
    exit 1
fi

mv -f "$TEMP_PATH" "$OUTPUT_PATH"

echo "Starting PM2 process: $APP_NAME"
if [[ "$registered" == "1" ]]; then
    pm2 restart "$APP_NAME" --update-env
else
    pm2 start "$ECOSYSTEM_FILE" --only "$APP_NAME"
fi

port="$(get_service_port)"
health_url="http://127.0.0.1:$port/"
healthy=0
elapsed=0
while (( elapsed < HEALTH_TIMEOUT_SECONDS )); do
    if curl -fsS --max-time 3 "$health_url" >/dev/null 2>&1; then
        healthy=1
        break
    fi
    sleep 1
    elapsed=$((elapsed + 1))
done

pm2 list

if [[ "$healthy" == "1" ]]; then
    echo "Service is healthy: $health_url"
else
    echo "Service did not become healthy within $HEALTH_TIMEOUT_SECONDS seconds. Check: pm2 logs $APP_NAME" >&2
    exit 1
fi