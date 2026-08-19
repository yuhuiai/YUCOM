#!/usr/bin/env bash
set -euo pipefail

APP_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
MACHINE_ARCH="$(uname -m)"
case "$MACHINE_ARCH" in
    aarch64|arm64)
        NATIVE_BIN="$APP_DIR/YUCOM-Linux-native-arm64"
        FALLBACK_BIN="$APP_DIR/YUCOM-Linux-arm64"
        ;;
    x86_64|amd64)
        NATIVE_BIN="$APP_DIR/YUCOM-Linux-native-amd64"
        FALLBACK_BIN="$APP_DIR/YUCOM-Linux-amd64"
        ;;
    *)
        printf '错误：暂不支持当前CPU架构：%s\n' "$MACHINE_ARCH" >&2
        printf '当前安装包支持：aarch64/arm64、x86_64/amd64\n' >&2
        read -r -p '按回车键退出…' _
        exit 1
        ;;
esac

if [[ -f "$NATIVE_BIN" ]]; then
    APP_BIN="$NATIVE_BIN"
else
    APP_BIN="$FALLBACK_BIN"
fi

if [[ ! -f "$APP_BIN" ]]; then
    printf '错误：找不到 %s\n' "$APP_BIN" >&2
    read -r -p '按回车键退出…' _
    exit 1
fi

if [[ ! -x "$APP_BIN" ]]; then
    chmod u+x "$APP_BIN"
fi

exec "$APP_BIN"
