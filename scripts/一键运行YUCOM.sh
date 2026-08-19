#!/usr/bin/env bash
set -euo pipefail

# YUCOM one-click launcher for Kylin/Linux native builds.
# It runs an existing native binary immediately. If this is a development
# directory without a native binary, it can install build dependencies once,
# compile the GTK native window, and then launch it.

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$SCRIPT_DIR/YUCOM-Linux-native-arm64" || \
      -f "$SCRIPT_DIR/YUCOM-Linux-native-amd64" || \
      -d "$SCRIPT_DIR/runtime" ]]; then
    # Packaged layout: the launcher is at the package root.
    PROJECT_DIR="$SCRIPT_DIR"
else
    # Development layout: the launcher lives below the project root.
    PROJECT_DIR="$(cd -- "$SCRIPT_DIR/.." && pwd)"
fi
cd "$PROJECT_DIR"

case "$(uname -m)" in
    aarch64|arm64) ARCH_VALUE=arm64 ;;
    x86_64|amd64) ARCH_VALUE=amd64 ;;
    *)
        printf '错误：暂不支持当前CPU架构：%s\n' "$(uname -m)" >&2
        exit 1
        ;;
esac

NATIVE_BIN=""
for candidate in \
    "$PROJECT_DIR/YUCOM-Linux-native-$ARCH_VALUE" \
    "$PROJECT_DIR/dist/YUCOM-Linux-native-$ARCH_VALUE"; do
    if [[ -f "$candidate" ]]; then
        NATIVE_BIN="$candidate"
        break
    fi
done

if [[ -n "$NATIVE_BIN" ]]; then
    if [[ ! -x "$NATIVE_BIN" ]]; then
        chmod u+x "$NATIVE_BIN"
    fi

    # Offline test machines must never try to install packages or contact a
    # repository. Report missing runtime libraries clearly instead.
    if [[ -d "$PROJECT_DIR/runtime/lib" ]]; then
        export LD_LIBRARY_PATH="$PROJECT_DIR/runtime/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
    fi
    check_runtime() {
        if command -v ldd >/dev/null 2>&1; then
            ldd "$NATIVE_BIN" 2>/dev/null | awk '/not found/{print $1}'
        fi
    }
    missing_runtime="$(check_runtime)"
    if [[ -n "$missing_runtime" ]]; then
        dependency_installer="$PROJECT_DIR/安装麒麟离线依赖.sh"
        if [[ ! -f "$dependency_installer" && -f "$PROJECT_DIR/scripts/安装麒麟离线依赖.sh" ]]; then
            dependency_installer="$PROJECT_DIR/scripts/安装麒麟离线依赖.sh"
        fi
        if [[ -f "$dependency_installer" ]]; then
            # The launcher is the single user action. Keep the unavoidable
            # sudo password prompt, but do not add a second Y/n confirmation.
            YUCOM_AUTO_INSTALL=1 bash "$dependency_installer"
            missing_runtime="$(check_runtime)"
        fi
        if [[ -n "$missing_runtime" ]]; then
            printf '错误：麒麟运行环境仍缺少动态库：%s\n' "$missing_runtime" >&2
            printf '请把完整离线安装包复制到本设备后重试。\n' >&2
            exit 3
        fi
    fi
    exec "$NATIVE_BIN"
fi

if [[ "${YUCOM_INSTALL_DEPS:-0}" != "1" ]]; then
    printf '错误：未找到麒麟原生程序。\n' >&2
    printf '离线测试设备不会自动联网或安装依赖。请先在联网开发机编译并打包，\n' >&2
    printf '再把完整安装包通过U盘复制到本设备后运行本脚本。\n' >&2
    printf '如需在开发机执行依赖安装和编译，请使用：YUCOM_INSTALL_DEPS=1 bash scripts/一键运行YUCOM.sh\n' >&2
    exit 2
fi

need_install=()
command -v go >/dev/null 2>&1 || need_install+=(golang-go)
command -v gcc >/dev/null 2>&1 || need_install+=(build-essential)
if ! command -v pkg-config >/dev/null 2>&1; then
    need_install+=(pkg-config libgtk-3-dev)
elif ! pkg-config --exists gtk+-3.0; then
    need_install+=(libgtk-3-dev)
fi

if (( ${#need_install[@]} > 0 )); then
    if ! command -v apt-get >/dev/null 2>&1; then
        printf '错误：缺少开发依赖（%s），且系统没有 apt-get。\n' "${need_install[*]}" >&2
        printf '请由管理员准备 Go、gcc、pkg-config 和 GTK3 开发库后重试。\n' >&2
        exit 2
    fi

    printf '\nYUCOM 首次开发启动需要安装：%s\n' "${need_install[*]}"
    printf '影响：仅安装编译依赖，不修改串口配置、驱动或桌面设置。\n'
    printf '风险：需要管理员权限，会更新系统软件包；运行已编译程序不需要此步骤。\n'
    if [[ "${YUCOM_AUTO_INSTALL:-0}" != "1" ]]; then
        read -r -p '是否继续安装并编译？[Y/n] ' answer
        case "$answer" in
            [nN]*) printf '已取消。\n'; exit 0 ;;
        esac
    fi

    if [[ "$EUID" -eq 0 ]]; then
        APT=(apt-get)
    else
        APT=(sudo apt-get)
    fi
    "${APT[@]}" update
    "${APT[@]}" install -y "${need_install[@]}"
fi

bash "$PROJECT_DIR/scripts/构建麒麟原生版.sh"
NATIVE_BIN="$PROJECT_DIR/dist/YUCOM-Linux-native-$ARCH_VALUE"
exec "$NATIVE_BIN"
