#!/usr/bin/env bash
set -euo pipefail

BASE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
DEB_DIR="$BASE_DIR/offline-debs"

# In a packaged bundle the installer and offline-debs are siblings. In the
# source tree the installer is under scripts/ while offline-debs is at the
# project root, so accept that layout too.
if [[ ! -d "$DEB_DIR" && -d "$BASE_DIR/../offline-debs" ]]; then
    DEB_DIR="$BASE_DIR/../offline-debs"
fi

if [[ ! -d "$DEB_DIR" ]] || ! compgen -G "$DEB_DIR/*.deb" >/dev/null; then
    printf '未找到离线依赖包目录：%s\n' "$DEB_DIR" >&2
    printf '请在联网的同型号麒麟开发机准备 offline-debs 后重新打包。\n' >&2
    exit 2
fi
if ! command -v dpkg >/dev/null 2>&1; then
    printf '错误：系统没有 dpkg，无法安装离线依赖。\n' >&2
    exit 2
fi

printf '即将从本地 offline-debs 安装运行依赖，不访问网络。\n'
printf '影响：可能修改系统软件包数据库；不会修改串口配置。\n'
if [[ "${YUCOM_AUTO_INSTALL:-0}" != "1" ]]; then
    read -r -p '是否继续？[Y/n] ' answer
    case "$answer" in
        [nN]*) printf '已取消。\n'; exit 0 ;;
    esac
fi

if [[ "$EUID" -eq 0 ]]; then
    DPKG=(dpkg)
else
    if ! command -v sudo >/dev/null 2>&1; then
        printf '错误：当前用户不是root且没有sudo。\n' >&2
        exit 2
    fi
    DPKG=(sudo dpkg)
fi

# Unpack all files first, then let dpkg configure them in dependency order.
safe_debs=()
safe_names=()
for deb in "$DEB_DIR"/*.deb; do
    package_name="$(dpkg-deb -f "$deb" Package 2>/dev/null || true)"
    case "$package_name" in
        libc6|libc-bin|libgcc-s*|libstdc++*|linux-libc-dev)
            printf '跳过核心系统包：%s\n' "$package_name"
            ;;
        *)
            safe_debs+=("$deb")
            safe_names+=("$package_name")
            ;;
    esac
done
if (( ${#safe_debs[@]} == 0 )); then
    printf '错误：离线目录中没有可安全安装的用户态依赖。\n' >&2
    exit 2
fi
"${DPKG[@]}" --unpack "${safe_debs[@]}"

# Configure only packages supplied by this offline bundle. Do not use
# `dpkg --configure -a`, which could also modify unrelated pending packages.
pending=1
for attempt in 1 2 3; do
    pending=0
    for package_name in "${safe_names[@]}"; do
        if ! "${DPKG[@]}" --configure "$package_name"; then
            pending=1
        fi
    done
    (( pending == 0 )) && break
done
if (( pending != 0 )); then
    printf '错误：离线依赖未能全部配置完成，请保留终端输出交给管理员处理。\n' >&2
    exit 3
fi
printf '离线依赖安装完成。\n'
