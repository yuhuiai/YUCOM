#!/usr/bin/env bash
set -euo pipefail

# Run on an online Kylin machine matching the target release and architecture.
# One command installs missing build tools, builds the native GTK window,
# downloads offline runtime packages, and creates the delivery archive.

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"

case "$(uname -m)" in
    aarch64|arm64) ARCH_VALUE=arm64 ;;
    x86_64|amd64) ARCH_VALUE=amd64 ;;
    *) printf '错误：暂不支持CPU架构：%s\n' "$(uname -m)" >&2; exit 1 ;;
esac

printf 'YUCOM麒麟离线包一键制作\n'
printf '系统：%s\n' "$(. /etc/os-release 2>/dev/null; printf '%s %s' "${PRETTY_NAME:-Linux}" "${VERSION_ID:-}")"
printf '架构：%s\n' "$ARCH_VALUE"
printf '影响：缺少工具时会通过APT安装Go、GCC、pkg-config和GTK3开发包。\n'
printf '风险：中等；会修改本开发机的软件包数据库，但不修改串口、网络或驱动。\n'

need_install=()
command -v go >/dev/null 2>&1 || need_install+=(golang-go)
command -v gcc >/dev/null 2>&1 || need_install+=(build-essential)
command -v pkg-config >/dev/null 2>&1 || need_install+=(pkg-config)
if ! command -v pkg-config >/dev/null 2>&1 || ! pkg-config --exists gtk+-3.0; then
    need_install+=(libgtk-3-dev)
fi

if (( ${#need_install[@]} > 0 )); then
    if ! command -v apt-get >/dev/null 2>&1; then
        printf '错误：缺少编译依赖，且系统没有apt-get。\n' >&2
        exit 2
    fi
    if [[ "$EUID" -eq 0 ]]; then
        APT=(apt-get)
    else
        APT=(sudo apt-get)
    fi
    "${APT[@]}" update
    "${APT[@]}" install -y "${need_install[@]}"
fi

bash scripts/构建麒麟原生版.sh
bash scripts/准备麒麟离线依赖.sh
bash scripts/打包麒麟离线运行包.sh

printf '\n完成：%s\n' "$PROJECT_DIR/YUCOM-V1.2.0-Linux-offline-$ARCH_VALUE.tar.gz"
printf '把这个压缩包复制到同版本、同架构的离线麒麟设备即可。\n'
