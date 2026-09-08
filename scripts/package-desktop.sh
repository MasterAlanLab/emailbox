#!/bin/bash

# 构建并打包桌面版。只为**当前所在平台**打包：Wails 依赖各平台的原生 WebView
# （macOS WKWebView / Windows WebView2 / Linux WebKitGTK），要经 cgo 链接系统库，
# 交叉编译走不通——四个平台的产物由 CI 在四台 runner 上分别构建。
#
# 用法：scripts/package-desktop.sh [版本号]
# 环境变量：
#   SKIP_WEB=1   跳过前端构建（CI 里前端只需构建一次时用）

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-dev}"
OUT_DIR="$PROJECT_ROOT/dist-desktop"
GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"

echo "📦 打包 Emailbox 桌面版 $VERSION ($GOOS/$GOARCH)"

if [ "$SKIP_WEB" != "1" ]; then
    "$PROJECT_ROOT/scripts/embed-web.sh"
fi

# 前端产物必须在编译前就位：go:embed 在编译期读取它，缺了就得到一个
# 能启动但打不开任何页面的应用，而且要等到运行时才发现。
if [ ! -f "$PROJECT_ROOT/pkg/webui/static/index.html" ]; then
    echo "❌ pkg/webui/static/index.html 不存在，请先运行 scripts/embed-web.sh"
    exit 1
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

# 构建目录放在项目内而不是 mktemp -d：Windows runner 上脚本跑在 Git Bash 里，
# mktemp 给出的是 /tmp/xxx 这种 POSIX 路径，而 go 是原生 Windows 程序，不认它。
BUILD_DIR="$OUT_DIR/.build"
mkdir -p "$BUILD_DIR"

# -s -w 去掉调试符号；Wails 应用本身就有十几 MB，符号表没有留下的必要。
LDFLAGS="-s -w"
BIN_NAME="emailbox"
TAGS=""
if [ "$GOOS" = "windows" ]; then
    BIN_NAME="emailbox.exe"
    # -H windowsgui：不带这个标志，双击启动会连带弹出一个黑色控制台窗口。
    LDFLAGS="$LDFLAGS -H windowsgui"
fi
if [ "$GOOS" = "linux" ]; then
    # Wails v3 在 Linux 上默认链接 GTK4 + webkitgtk-6.0，gtk3 这个 tag 把它切回
    # GTK3 + webkit2gtk-4.1。选后者是因为它在 Ubuntu 22.04 / Debian 12 上就有，
    # 而 webkitgtk-6.0 只有很新的发行版才带——发出去的二进制链哪个，
    # 决定了多少人装得上。下面 README.txt 里写的运行时依赖也是按 4.1 给的。
    TAGS="gtk3"
fi

echo "🔨 编译桌面二进制..."
# 输出用相对路径，同样是为了绕开 Git Bash 与原生 go 之间的路径表示差异。
cd "$PROJECT_ROOT/desktop"
go build -trimpath ${TAGS:+-tags "$TAGS"} -ldflags "$LDFLAGS" -o "../dist-desktop/.build/$BIN_NAME" .

case "$GOOS" in
darwin)
    APP="$BUILD_DIR/Emailbox.app"
    mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
    mv "$BUILD_DIR/$BIN_NAME" "$APP/Contents/MacOS/emailbox"

    # 图标取自 favicon.ico——它内部就是一张 256x256 的 PNG。
    # sips 读不了 ICO 容器，得先按目录项里的偏移把 PNG 取出来：
    # ICO 头 6 字节，随后每个目录项 16 字节，其中第 12-15 字节是图像数据偏移。
    ICONSET="$BUILD_DIR/icon.iconset"
    ICON_SRC="$BUILD_DIR/icon-source.png"
    ICON_OFFSET=$(od -An -tu4 -j 18 -N 4 "$PROJECT_ROOT/web/public/favicon.ico" | tr -d ' ')
    tail -c "+$((ICON_OFFSET + 1))" "$PROJECT_ROOT/web/public/favicon.ico" > "$ICON_SRC"
    # 确认取出来的确实是 PNG（魔数 89 50 4E 47）再往下走，
    # 否则 iconutil 会拿着一坨二进制生成一个看不出问题的空图标。
    if [ "$(od -An -tx1 -N 4 "$ICON_SRC" | tr -d ' ')" = "89504e47" ]; then
        mkdir -p "$ICONSET"
        # iconutil 只认这套固定命名，写错的条目会被静默忽略。
        sips -z 16 16 "$ICON_SRC" --out "$ICONSET/icon_16x16.png" >/dev/null 2>&1
        sips -z 32 32 "$ICON_SRC" --out "$ICONSET/icon_16x16@2x.png" >/dev/null 2>&1
        cp "$ICONSET/icon_16x16@2x.png" "$ICONSET/icon_32x32.png"
        sips -z 64 64 "$ICON_SRC" --out "$ICONSET/icon_32x32@2x.png" >/dev/null 2>&1
        sips -z 128 128 "$ICON_SRC" --out "$ICONSET/icon_128x128.png" >/dev/null 2>&1
        sips -z 256 256 "$ICON_SRC" --out "$ICONSET/icon_128x128@2x.png" >/dev/null 2>&1
        cp "$ICONSET/icon_128x128@2x.png" "$ICONSET/icon_256x256.png"
        iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns" ||
            echo "⚠️  图标生成失败，应用将使用系统默认图标"
    else
        echo "⚠️  favicon.ico 不是 PNG 格式的图标，跳过图标生成"
    fi

    cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>Emailbox</string>
    <key>CFBundleDisplayName</key><string>Emailbox</string>
    <key>CFBundleExecutable</key><string>emailbox</string>
    <key>CFBundleIdentifier</key><string>com.masteralanlab.emailbox</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>${VERSION#v}</string>
    <key>CFBundleVersion</key><string>${VERSION#v}</string>
    <key>CFBundleIconFile</key><string>icon</string>
    <key>LSMinimumSystemVersion</key><string>10.15</string>
    <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

    ARCHIVE="$OUT_DIR/emailbox-$VERSION-macos-$GOARCH.zip"
    # ditto 而不是 zip：它保留 .app 需要的资源分叉与符号链接
    (cd "$BUILD_DIR" && ditto -c -k --sequesterRsrc --keepParent "Emailbox.app" "$ARCHIVE")
    ;;
windows)
    ARCHIVE="$OUT_DIR/emailbox-$VERSION-windows-$GOARCH.zip"
    # Git Bash 不自带 zip，回落到 PowerShell。cygpath 负责把 POSIX 路径
    # 翻译成 PowerShell 认识的 Windows 路径。
    if command -v zip >/dev/null 2>&1; then
        (cd "$BUILD_DIR" && zip -q -r "$ARCHIVE" "$BIN_NAME")
    elif command -v powershell >/dev/null 2>&1; then
        powershell -NoProfile -Command "Compress-Archive -Path '$(cygpath -w "$BUILD_DIR/$BIN_NAME")' -DestinationPath '$(cygpath -w "$ARCHIVE")' -Force"
    else
        echo "❌ 打包需要 zip 或 powershell，两者都不可用"
        exit 1
    fi
    ;;
linux)
    # 打包目录必须套一层 stage/：包内的顶层目录要叫 emailbox，而编译出来的
    # 二进制此刻正占着 $BUILD_DIR/emailbox 这个名字，直接对它 mkdir -p 会以
    # 「File exists」失败——mkdir -p 只容忍已存在的**目录**，不容忍同名文件。
    STAGE="$BUILD_DIR/stage"
    PKG="$STAGE/emailbox"
    mkdir -p "$PKG"
    mv "$BUILD_DIR/$BIN_NAME" "$PKG/emailbox"
    cp "$PROJECT_ROOT/assets/logo.svg" "$PKG/emailbox.svg" 2>/dev/null || true
    cat > "$PKG/emailbox.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Emailbox
Comment=批量邮箱托管与收信
Exec=emailbox
Icon=emailbox
Categories=Network;Email;
Terminal=false
DESKTOP
    cat > "$PKG/README.txt" <<README
Emailbox 桌面版 $VERSION

运行：./emailbox

依赖：本程序使用系统 WebKitGTK 渲染界面，需要先安装运行库。
  Debian / Ubuntu: sudo apt install libwebkit2gtk-4.1-0
  Fedora:          sudo dnf install webkit2gtk4.1
  Arch:            sudo pacman -S webkit2gtk-4.1

安装到菜单（可选）：
  cp emailbox ~/.local/bin/
  cp emailbox.svg ~/.local/share/icons/emailbox.svg
  cp emailbox.desktop ~/.local/share/applications/

数据存放在 ~/.config/emailbox/，其中 encryption.key 是解开全部邮箱凭据的
唯一凭证，换机器时请一并迁移，且不要放进任何会被同步或分享的目录。
README
    ARCHIVE="$OUT_DIR/emailbox-$VERSION-linux-$GOARCH.tar.gz"
    tar -C "$STAGE" -czf "$ARCHIVE" emailbox
    ;;
*)
    echo "❌ 不支持的平台: $GOOS"
    exit 1
    ;;
esac

rm -rf "$BUILD_DIR"

echo ""
echo "🎉 打包完成: $ARCHIVE"
echo "   大小: $(du -h "$ARCHIVE" | cut -f1)"
