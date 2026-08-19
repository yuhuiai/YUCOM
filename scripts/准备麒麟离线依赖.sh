#!/usr/bin/env bash
set -euo pipefail

# Run this on an online Kylin development machine of the same release and
# architecture. It downloads runtime .deb files into offline-debs; the test
# machine never runs apt-get or contacts a repository.

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"
DEB_DIR="$PROJECT_DIR/offline-debs"
mkdir -p "$DEB_DIR"

if ! command -v apt-cache >/dev/null 2>&1 || ! command -v apt-get >/dev/null 2>&1; then
    printf '错误：需要 apt-cache 和 apt-get。\n' >&2
    exit 2
fi

ROOT_PACKAGES=(libgtk-3-0 libgtk-3-common)
mapfile -t PACKAGES < <(
    apt-cache depends --recurse --no-recommends --no-suggests "${ROOT_PACKAGES[@]}" 2>/dev/null |
        awk '/^[A-Za-z0-9][A-Za-z0-9+.-]*(:[A-Za-z0-9]+)?$/ {print $1}' |
        grep -Ev '^(libc6|libc-bin|libgcc-s[0-9-]+|libstdc\+\+[0-9-]+|linux-libc-dev)(:.*)?$' |
        sort -u
)
if (( ${#PACKAGES[@]} == 0 )); then
    printf '错误：没有解析到GTK3运行依赖，请检查本机APT源。\n' >&2
    exit 2
fi

printf '将下载 %d 个运行依赖到 %s（不安装、不修改当前系统）。\n' "${#PACKAGES[@]}" "$DEB_DIR"
for package in "${PACKAGES[@]}"; do
    (cd "$DEB_DIR" && apt-get download "$package")
done
printf '离线依赖准备完成，请继续执行：bash scripts/打包麒麟离线运行包.sh\n'
