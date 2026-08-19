#!/usr/bin/env bash
set -euo pipefail

# Build an offline runtime package on the same Kylin architecture/image as
# the target. The package contains the native binary and non-core shared
# libraries required by GTK. It never replaces system libraries.

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"

case "$(uname -m)" in
    aarch64|arm64) ARCH_VALUE=arm64 ;;
    x86_64|amd64) ARCH_VALUE=amd64 ;;
    *) printf '错误：暂不支持架构：%s\n' "$(uname -m)" >&2; exit 1 ;;
esac

NATIVE_BIN="dist/YUCOM-Linux-native-$ARCH_VALUE"
if [[ ! -f "$NATIVE_BIN" ]]; then
    printf '错误：找不到 %s，请先运行 scripts/构建麒麟原生版.sh\n' "$NATIVE_BIN" >&2
    exit 1
fi
if ! command -v ldd >/dev/null 2>&1; then
    printf '错误：未找到 ldd，无法收集运行库。\n' >&2
    exit 1
fi

PACKAGE="YUCOM-V1.2.0-Linux-offline-$ARCH_VALUE"
rm -rf -- "$PACKAGE" "$PACKAGE.tar.gz"
mkdir -p "$PACKAGE/runtime/lib"
cp -- "$NATIVE_BIN" "$PACKAGE/YUCOM-Linux-native-$ARCH_VALUE"
chmod u+x "$PACKAGE/YUCOM-Linux-native-$ARCH_VALUE"

is_core_library() {
    case "$(basename -- "$1")" in
        libc.so.*|libm.so.*|libpthread.so.*|libdl.so.*|librt.so.*|ld-linux*.so.*|libresolv.so.*|libutil.so.*|libnsl.so.*|libgcc_s.so.*|linux-vdso.so.*)
            return 0 ;;
        *) return 1 ;;
    esac
}

declare -A seen=()
queue=("$(readlink -f -- "$NATIVE_BIN")")
while (( ${#queue[@]} > 0 )); do
    current="${queue[0]}"
    queue=("${queue[@]:1}")
    real_current="$(readlink -f -- "$current" 2>/dev/null || true)"
    [[ -n "$real_current" && -f "$real_current" ]] || continue
    [[ -n "${seen[$real_current]:-}" ]] && continue
    seen["$real_current"]=1

    dependencies="$(ldd "$real_current" 2>/dev/null || true)"
    missing="$(printf '%s\n' "$dependencies" | awk '/not found/{print $1}')"
    if [[ -n "$missing" ]]; then
        printf '错误：开发机缺少运行库：%s\n' "$missing" >&2
        printf '请先在联网开发机补齐 GTK3 运行环境，再重新打包。\n' >&2
        exit 2
    fi

    while IFS= read -r library; do
        [[ -n "$library" && -f "$library" ]] || continue
        real_library="$(readlink -f -- "$library" 2>/dev/null || true)"
        [[ -n "$real_library" && -f "$real_library" ]] || continue
        is_core_library "$real_library" && continue

        cp -L -- "$library" "$PACKAGE/runtime/lib/$(basename -- "$library")"
        if [[ "$(basename -- "$real_library")" != "$(basename -- "$library")" ]]; then
            cp -L -- "$real_library" "$PACKAGE/runtime/lib/$(basename -- "$real_library")"
        fi
        queue+=("$real_library")
    done < <(printf '%s\n' "$dependencies" | awk '/=>/ && $3 ~ /^\// {print $3}')
done

cp -- scripts/一键运行YUCOM.sh scripts/安装麒麟离线依赖.sh scripts/YUCOM.desktop README.txt 01-安装和使用步骤.txt 02-通用串口测试步骤.txt "$PACKAGE/"
chmod u+x "$PACKAGE/一键运行YUCOM.sh" "$PACKAGE/安装麒麟离线依赖.sh" "$PACKAGE/YUCOM.desktop"
if [[ -d offline-debs ]]; then
    cp -a -- offline-debs "$PACKAGE/"
fi

(cd "$PACKAGE" && find YUCOM-Linux-native-* runtime/lib 一键运行YUCOM.sh 安装麒麟离线依赖.sh YUCOM.desktop offline-debs README.txt 01-安装和使用步骤.txt 02-通用串口测试步骤.txt -type f -print0 2>/dev/null | sort -z | xargs -0 sha256sum > SHA256SUMS.txt)
tar -czf "$PACKAGE.tar.gz" "$PACKAGE"
printf '已生成离线运行包：%s\n' "$PACKAGE.tar.gz"
printf '包内运行库数量：%s\n' "$(find "$PACKAGE/runtime/lib" -type f | wc -l)"
