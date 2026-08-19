#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"

if ! command -v go >/dev/null 2>&1; then
    printf '错误：未找到 Go 编译器。\n' >&2
    exit 1
fi
if ! command -v pkg-config >/dev/null 2>&1 || ! pkg-config --exists gtk+-3.0; then
    printf '错误：未找到 GTK3 开发库。请由管理员安装 build-essential、pkg-config、libgtk-3-dev 后重试。\n' >&2
    printf '该脚本不会自动执行 sudo 或修改系统。\n' >&2
    exit 1
fi

case "$(uname -m)" in
    aarch64|arm64) GOARCH_VALUE=arm64 ;;
    x86_64|amd64) GOARCH_VALUE=amd64 ;;
    *) printf '错误：暂不支持架构：%s\n' "$(uname -m)" >&2; exit 1 ;;
esac

mkdir -p dist
CGO_ENABLED=1 GOOS=linux GOARCH="$GOARCH_VALUE" go test -count=1 ./internal/...
CGO_ENABLED=1 GOOS=linux GOARCH="$GOARCH_VALUE" go build -tags nativegui -trimpath -ldflags '-s -w' \
    -o "dist/YUCOM-Linux-native-$GOARCH_VALUE" ./cmd/yucom

chmod u+x "dist/YUCOM-Linux-native-$GOARCH_VALUE"
printf '已生成：%s\n' "dist/YUCOM-Linux-native-$GOARCH_VALUE"
printf '运行方式：bash scripts/启动YUCOM.sh\n'
