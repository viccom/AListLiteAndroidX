# AListLiteAndroid 本地编译指南

本文档基于项目对话历史整理，指导如何在 Linux（Ubuntu 22.04）上本地编译 AListLiteAndroid 项目，生成 Android APK 安装包。

## 环境要求

### 操作系统

- **推荐**：Ubuntu 22.04 LTS 或更高版本（其他 Debian-based 发行版也可，但路径可能不同）。
- **架构**：x86_64（AMD64）。
- **权限**：需要 root 权限安装系统包（使用 `sudo`），但编译过程可在普通用户下运行。

### 硬件要求

- **CPU**：至少 2 核。
- **内存**：至少 4GB RAM（推荐 8GB+，因 Go 编译和 Gradle 构建占用内存）。
- **存储**：至少 10GB 可用空间（SDK 下载和构建产物占用约 5-8GB）。

### 软件依赖

- **JDK (Java Development Kit)**：版本 17（必须，Gradle 和 Android 构建工具需要）。
  - 推荐：OpenJDK 17（`openjdk-17-jdk`）。
- **Go (Golang)**：版本 1.24.1 或更高（必须，用于 gomobile 构建 AAR）。
- **Android SDK Command-line Tools**：版本 19.0 或更高（必须，用于管理 SDK 组件）。
  - 需要安装的组件：
    - `platform-tools`（ADB 等工具）。
    - `platforms;android-34`（Android 34 平台 API）。
    - `build-tools;30.0.2`（构建工具，包括 apksigner）。
    - `platforms;android-21`（gomobile bind 需要的最低 API）。
    - `ndk;25.2.9519653`（Android NDK，用于 gomobile）。
- **Gradle**：项目自带（`gradlew`），无需单独安装。
- **其他工具**：
  - `curl`、`wget`、`unzip`、`git`（下载和解压）。
  - `build-essential`（编译基础工具）。

### 依赖关系

- JDK → Gradle（Gradle 需要 Java）。
- Go → gomobile → AAR 生成（gomobile 依赖 Go 和 Android NDK）。
- Android SDK → 所有 Android 相关工具（platform-tools、platforms、build-tools、NDK）。
- 网络：需要互联网访问下载依赖（Go modules、Android SDK 组件、OpenList 源代码）。

## 依赖检查

### 检查手段

使用命令行工具检查版本和路径。以下是检查脚本示例（可复制到终端运行）：

```bash
# 1. 检查操作系统版本
lsb_release -a

# 2. 检查 JDK
java -version  # 输出应包含 "openjdk version \"17.x.x\""
which java     # 应输出路径，如 /usr/bin/java

# 3. 检查 Go
go version     # 输出应为 "go version go1.24.1 linux/amd64" 或更高
which go       # 应输出路径，如 /usr/local/go/bin/go

# 4. 检查 Android SDK
echo $ANDROID_SDK_ROOT  # 应输出路径，如 /root/Android
ls -la $ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager  # 文件应存在
sdkmanager --version    # 输出版本号，如 "19.0"

# 5. 检查 SDK 组件
ls -la $ANDROID_SDK_ROOT/platforms/android-34  # android-34 目录存在
ls -la $ANDROID_SDK_ROOT/platforms/android-21  # android-21 目录存在
ls -la $ANDROID_SDK_ROOT/build-tools/30.0.2    # 30.0.2 目录存在
ls -la $ANDROID_SDK_ROOT/ndk/25.2.9519653      # NDK 目录存在

# 6. 检查 gomobile
which gomobile  # 应输出路径，如 /root/go/bin/gomobile
gomobile version  # 输出应无错误（注意：gomobile version 可能报 module 错误，但不影响使用）

# 7. 检查其他工具
which curl wget unzip git  # 均应输出路径
```

### 判断正常的标准

- **版本匹配**：输出版本号符合要求（例如 JDK 17.x.x）。
- **路径存在**：`which` 和 `ls` 命令输出有效路径/文件。
- **无错误**：命令无报错（如 "command not found" 表示未安装）。
- **网络**：`curl -I https://dl.google.com` 返回 HTTP 200（可选，用于验证网络）。

如果任何检查失败，按“详细步骤”安装相应依赖。

## 详细步骤

### 步骤 1: 安装系统依赖

**目的**：安装 JDK、Go 和基础工具。

**命令**：

```bash
sudo apt update
sudo apt install -y openjdk-17-jdk curl wget unzip git build-essential

# 安装 Go 1.24.1
GO_VER=1.24.1
wget -q https://go.dev/dl/go${GO_VER}.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go${GO_VER}.linux-amd64.tar.gz
rm go${GO_VER}.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
```

**检查标准**：

- `java -version` 输出 OpenJDK 17。
- `go version` 输出 go1.24.1。
- `which curl wget unzip git` 均有输出。

### 步骤 2: 设置 Android SDK

**目的**：下载并配置 Android SDK command-line tools。

**命令**：

```bash
# 设置 SDK 根目录
export ANDROID_SDK_ROOT=$HOME/Android
mkdir -p $ANDROID_SDK_ROOT/cmdline-tools

# 下载 command-line tools
cd /tmp
CMDZIP=commandlinetools-linux-9477386_latest.zip
wget -q https://dl.google.com/android/repository/${CMDZIP} -O ${CMDZIP}
unzip -q ${CMDZIP} -d $ANDROID_SDK_ROOT/cmdline-tools
mv $ANDROID_SDK_ROOT/cmdline-tools/cmdline-tools $ANDROID_SDK_ROOT/cmdline-tools/latest
rm /tmp/${CMDZIP}

# 加入 PATH
export PATH=$ANDROID_SDK_ROOT/cmdline-tools/latest/bin:$ANDROID_SDK_ROOT/platform-tools:$PATH
echo "export ANDROID_SDK_ROOT=$ANDROID_SDK_ROOT" >> ~/.bashrc
echo 'export PATH=$ANDROID_SDK_ROOT/cmdline-tools/latest/bin:$ANDROID_SDK_ROOT/platform-tools:$PATH' >> ~/.bashrc
source ~/.bashrc

# 接受许可并安装组件
yes | sdkmanager --sdk_root=$ANDROID_SDK_ROOT --licenses
sdkmanager --sdk_root=$ANDROID_SDK_ROOT "platform-tools" "platforms;android-34" "platforms;android-21" "build-tools;30.0.2" "ndk;25.2.9519653"
```

**检查标准**：

- `sdkmanager --version` 输出版本号。
- `ls -la $ANDROID_SDK_ROOT/platforms/android-34` 目录存在。
- `ls -la $ANDROID_SDK_ROOT/build-tools/30.0.2` 目录存在。
- `ls -la $ANDROID_SDK_ROOT/ndk/25.2.9519653` 目录存在。

### 步骤 3: 安装 gomobile 并下载 OpenList 源

**目的**：准备 Go 环境，下载项目依赖的 OpenList 源代码。

**命令**：

```bash
# 安装 gomobile
go install golang.org/x/mobile/cmd/gomobile@latest
export PATH="$(go env GOPATH)/bin:$PATH"
echo 'export PATH=$(go env GOPATH)/bin:$PATH' >> ~/.bashrc
source ~/.bashrc

# 设置 NDK 环境
NDK_DIR=$(ls -d $ANDROID_SDK_ROOT/ndk/* | sort -V | tail -n1)
export ANDROID_NDK_HOME=$NDK_DIR
export ANDROID_NDK_ROOT=$NDK_DIR
echo "export ANDROID_NDK_HOME=$NDK_DIR" >> ~/.bashrc
echo "export ANDROID_NDK_ROOT=$NDK_DIR" >> ~/.bashrc
source ~/.bashrc

# 初始化 gomobile
gomobile init

# 进入项目脚本目录，下载 OpenList
cd /root/github.com/AListLiteAndroid/AListLib/scripts
sh install_alist.sh
```

**检查标准**：

- `which gomobile` 有输出。
- `gomobile init` 无错误输出。
- `ls -la ../sources/go.mod` 文件存在（OpenList 源下载成功）。

### 步骤 4: 构建 AAR

**目的**：使用 gomobile 生成 Android AAR 库。

**命令**：

```bash
cd /root/github.com/AListLiteAndroid/AListLib/sources
go get golang.org/x/mobile/bind@latest
go mod download
cd /root/github.com/AListLiteAndroid/AListLib/scripts
sh build_aar.sh
```

**检查标准**：

- `ls -l ../../app/libs/alistlib.aar` 文件存在，大小约 200MB。

### 步骤 5: 构建 APK

**目的**：使用 Gradle 编译 Android APK。

**命令**：

```bash
cd /root/github.com/AListLiteAndroid
chmod +x gradlew

# 构建 debug 版本（快速测试）
./gradlew assembleDebug -q

# 构建 release 版本（生产用）
./gradlew assembleRelease -q
```

**检查标准**：

- `find app/build/outputs/apk -name "*.apk"` 输出多个 APK 文件（debug 和 release 变体）。
- `./gradlew assembleDebug` 无 "BUILD FAILED"。

### 步骤 6: 签名 APK（可选）

**目的**：为 release APK 添加签名，便于安装。

**命令**：

```bash
# 生成 keystore（如果没有）
keytool -genkeypair -v -keystore /root/release_keystore.jks -keyalg RSA -keysize 2048 -validity 10000 -alias releasekey -storepass MyStorePass123 -keypass MyKeyPass123 -dname "CN=AListLite, OU=Dev, O=MyCompany, L=Beijing, ST=Beijing, C=CN"

# 签名 APK（选择一个 release APK）
UNSIGNED_APK=app/build/outputs/apk/release/AListLite-v2.0.7-beta5-auto2-universal-release.apk
$ANDROID_SDK_ROOT/build-tools/30.0.2/apksigner sign --ks /root/release_keystore.jks --ks-key-alias releasekey --ks-pass pass:MyStorePass123 --key-pass pass:MyKeyPass123 --out signed.apk "$UNSIGNED_APK"

# 验证签名
$ANDROID_SDK_ROOT/build-tools/30.0.2/apksigner verify signed.apk
```

**检查标准**：

- `apksigner verify signed.apk` 输出无 "Verification failed"，只有警告（正常）。

## 故障排除

- **网络问题**：如果 `sdkmanager` 下载失败，使用 `--no_https` 或代理。
- **gomobile 错误**：确保 `ANDROID_NDK_HOME` 指向正确 NDK 目录。
- **Gradle 错误**：运行 `./gradlew --stacktrace` 查看详情。
- **权限问题**：以 root 运行可能导致文件权限问题，推荐普通用户。

## 总结

按照以上步骤，可在 Ubuntu 22.04 上完整编译项目。总时间约 30-60 分钟（取决于网络）。构建产物在 `app/build/outputs/apk/`。