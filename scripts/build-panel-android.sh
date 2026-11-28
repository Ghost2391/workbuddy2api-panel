#!/usr/bin/env bash
# 构建签到 App 内嵌的手机面板二进制（linux/arm64 与 linux/armv7）。
# 产物放入 jniLibs，随 App 打包；App 从 nativeLibraryDir 直接 exec。
set -euo pipefail
cd "$(dirname "$0")/.."

command -v go >/dev/null || { echo "go not found in PATH" >&2; exit 1; }

out="android/app/src/main/jniLibs"
mkdir -p "$out/arm64-v8a" "$out/armeabi-v7a"

build() {
    local goarch="$1" goarm="$2" dst="$3"
    echo "building panel for linux/${goarch}${goarm:+ armv$goarm} -> $dst"
    if [ -n "$goarm" ]; then
        CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
            go build -trimpath -ldflags "-s -w" -o "$dst" ./cmd/server
    else
        CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" \
            go build -trimpath -ldflags "-s -w" -o "$dst" ./cmd/server
    fi
}

build arm64 "" "$out/arm64-v8a/libwbpanel.so"
build arm 7 "$out/armeabi-v7a/libwbpanel.so"

echo "---"
file "$out/arm64-v8a/libwbpanel.so" "$out/armeabi-v7a/libwbpanel.so"
