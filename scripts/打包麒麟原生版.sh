#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"

case "$(uname -m)" in
    aarch64|arm64) ARCH_VALUE=arm64 ;;
    x86_64|amd64) ARCH_VALUE=amd64 ;;
    *) printf '错误：暂不支持架构：%s\n' "$(uname -m)" >&2; exit 1 ;;
esac

NATIVE_BIN="dist/YUCOM-Linux-native-$ARCH_VALUE"
if [[ ! -f "$NATIVE_BIN" ]]; then
    printf '错误：请先运行 bash scripts/构建麒麟原生版.sh\n' >&2
    exit 1
fi

PACKAGE="YUCOM-V1.2.0-Linux-native-$ARCH_VALUE"
rm -rf -- "$PACKAGE" "$PACKAGE.tar.gz"
mkdir -p "$PACKAGE"
cp -- "$NATIVE_BIN" "$PACKAGE/"
cp -- scripts/启动YUCOM.sh scripts/一键运行YUCOM.sh README.txt 01-安装和使用步骤.txt 02-通用串口测试步骤.txt "$PACKAGE/"
chmod u+x "$PACKAGE/启动YUCOM.sh" "$PACKAGE/YUCOM-Linux-native-$ARCH_VALUE"
chmod u+x "$PACKAGE/一键运行YUCOM.sh"
(cd "$PACKAGE" && sha256sum * > SHA256SUMS.txt)
tar -czf "$PACKAGE.tar.gz" "$PACKAGE"
printf '已生成：%s\n' "$PACKAGE.tar.gz"
