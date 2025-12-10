#!/bin/bash

# AListLiteAndroid 本地编译脚本
# 编译 APK 并签名。前提：环境依赖已安装。
# 检查失败则退出。日志记录到 build.log。

set -e  # 任何命令失败则退出

LOG_FILE="build.log"
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT_DIR="$PROJECT_ROOT/AListLib/scripts"

# 日志函数
log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$LOG_FILE"
}

# 检查函数
check_dependencies() {
    log "开始检查环境和依赖..."

    # 检查 JDK
    if ! java -version 2>&1 | grep -q "openjdk version \"17"; then
        log "错误：JDK 17 未安装或版本不匹配"
        exit 1
    fi
    log "JDK 检查通过"

    # 检查 Go
    if ! go version 2>&1 | grep -q "go1.25.3"; then
        log "错误：Go 1.25.3 未安装或版本不匹配"
        exit 1
    fi
    log "Go 检查通过"

    # 检查 Android SDK
    if [ -z "$ANDROID_SDK_ROOT" ]; then
        log "错误：ANDROID_SDK_ROOT 未设置"
        exit 1
    fi
    if [ ! -f "$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager" ]; then
        log "错误：sdkmanager 未找到"
        exit 1
    fi
    if ! sdkmanager --version >/dev/null 2>&1; then
        log "错误：sdkmanager 不可用"
        exit 1
    fi
    log "Android SDK 检查通过"

    # 检查 SDK 组件
    if [ ! -d "$ANDROID_SDK_ROOT/platforms/android-34" ]; then
        log "错误：platforms;android-34 未安装"
        exit 1
    fi
    if [ ! -d "$ANDROID_SDK_ROOT/platforms/android-21" ]; then
        log "错误：platforms;android-21 未安装"
        exit 1
    fi
    if [ ! -d "$ANDROID_SDK_ROOT/build-tools/30.0.2" ]; then
        log "错误：build-tools;30.0.2 未安装"
        exit 1
    fi
    if [ ! -d "$ANDROID_SDK_ROOT/ndk" ]; then
        log "错误：NDK 未安装"
        exit 1
    fi
    log "SDK 组件检查通过"

    # 检查 gomobile
    if ! which gomobile >/dev/null 2>&1; then
        log "错误：gomobile 未安装"
        exit 1
    fi
    log "gomobile 检查通过"

    # 检查 keystore（如果不存在则生成）
    KEYSTORE_PATH="$HOME/release_keystore.jks"
    if [ -f "$KEYSTORE_PATH" ]; then
        # 验证密码
        if ! keytool -list -keystore "$KEYSTORE_PATH" -storepass MyStorePass123 >/dev/null 2>&1; then
            log "keystore 密码不正确，删除并重新生成..."
            rm -f "$KEYSTORE_PATH"
        else
            log "keystore 已存在且密码正确"
        fi
    fi
    if [ ! -f "$KEYSTORE_PATH" ]; then
        log "keystore 不存在，正在生成..."
        keytool -genkeypair -v -keystore "$KEYSTORE_PATH" -storetype JKS -keyalg RSA -keysize 2048 -validity 10000 -alias releasekey -storepass MyStorePass123 -keypass MyKeyPass123 -dname "CN=AListLite, OU=Dev, O=MyCompany, L=Beijing, ST=Beijing, C=CN" -noprompt
        log "keystore 生成完成"
        log "keystore 生成完成"
    fi

    log "所有依赖检查通过"
}

# 编译步骤
build_aar() {
    log "开始构建 AAR..."
    cd "$SCRIPT_DIR"
    sh install_alist.sh
    sh build_aar.sh
    if [ ! -f "$PROJECT_ROOT/app/libs/alistlib.aar" ]; then
        log "错误：AAR 构建失败"
        exit 1
    fi
    log "AAR 构建成功"
}

build_apk() {
    log "开始构建 APK..."
    cd "$PROJECT_ROOT"
    chmod +x gradlew
    ./gradlew clean assembleRelease -q
    APK_COUNT=$(find app/build/outputs/apk/release -name "*.apk" | wc -l)
    if [ "$APK_COUNT" -eq 0 ]; then
        log "错误：APK 构建失败"
        exit 1
    fi
    log "APK 构建成功，共生成 $APK_COUNT 个文件"
}

sign_apk() {
    log "开始签名 APK..."
    log "调试 keystore..."
    keytool -list -v -keystore "$KEYSTORE_PATH" -storepass MyStorePass123
    UNSIGNED_APK=$(find app/build/outputs/apk/release -name "*-arm64-v8a-release.apk" | head -n1)
    if [ -z "$UNSIGNED_APK" ]; then
        log "错误：未找到 unsigned APK"
        exit 1
    fi
    SIGNED_APK="${UNSIGNED_APK%.apk}_signed.apk"
    $ANDROID_SDK_ROOT/build-tools/30.0.2/apksigner sign --ks "$KEYSTORE_PATH" --ks-key-alias releasekey --ks-pass pass:MyStorePass123 --key-pass pass:MyKeyPass123 --out "$SIGNED_APK" "$UNSIGNED_APK"
    $ANDROID_SDK_ROOT/build-tools/30.0.2/apksigner verify "$SIGNED_APK" >/dev/null 2>&1
    if [ $? -ne 0 ]; then
        log "错误：APK 签名验证失败"
        exit 1
    fi
    log "APK 签名成功：$SIGNED_APK"
}

# 主流程
main() {
    log "开始本地编译流程"
    check_dependencies
    build_aar
    build_apk
    sign_apk
    log "编译完成！签名 APK 位于 app/build/outputs/apk/release/"
}

main "$@"