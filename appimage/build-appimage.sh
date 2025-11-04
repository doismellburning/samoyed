#!/bin/bash
set -e

# Build AppImage for Samoyed
# This script creates an AppDir structure and packages it into an AppImage

ARCH="${ARCH:-x86_64}"
APP_NAME="Samoyed"
APPDIR="AppDir"

echo "Building AppImage for $APP_NAME ($ARCH)..."

# Clean up any previous builds
rm -rf "$APPDIR" *.AppImage

# Create AppDir structure
mkdir -p "$APPDIR/usr/bin"
mkdir -p "$APPDIR/usr/share/applications"
mkdir -p "$APPDIR/usr/share/icons/hicolor/scalable/apps"
mkdir -p "$APPDIR/usr/share/metainfo"
mkdir -p "$APPDIR/usr/share/samoyed"

# Copy binaries
echo "Copying binaries..."
cp dist/* "$APPDIR/usr/bin/"

# Copy data files
echo "Copying data files..."
cp -r data/* "$APPDIR/usr/share/samoyed/"

# Copy desktop file, icon, and metadata
echo "Copying metadata files..."
cp appimage/io.github.doismellburning.samoyed.desktop "$APPDIR/usr/share/applications/"
cp appimage/io.github.doismellburning.samoyed.desktop "$APPDIR/"
cp appimage/samoyed.svg "$APPDIR/usr/share/icons/hicolor/scalable/apps/"
cp appimage/samoyed.svg "$APPDIR/"
cp appimage/io.github.doismellburning.samoyed.appdata.xml "$APPDIR/usr/share/metainfo/"

# Create AppRun script
echo "Creating AppRun script..."
cat > "$APPDIR/AppRun" << 'EOF'
#!/bin/bash
SELF=$(readlink -f "$0")
HERE=${SELF%/*}
export PATH="${HERE}/usr/bin:${PATH}"
export LD_LIBRARY_PATH="${HERE}/usr/lib:${LD_LIBRARY_PATH}"
export XDG_DATA_DIRS="${HERE}/usr/share:${XDG_DATA_DIRS}"

# Execute the main binary with all arguments
exec "${HERE}/usr/bin/direwolf" "$@"
EOF
chmod +x "$APPDIR/AppRun"

# Download appimagetool if not present
if [ ! -f appimagetool-${ARCH}.AppImage ]; then
    echo "Downloading appimagetool..."
    wget -q "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-${ARCH}.AppImage"
    chmod +x appimagetool-${ARCH}.AppImage
fi

# Download runtime if not present
if [ ! -f runtime-${ARCH} ]; then
    echo "Downloading runtime..."
    wget -q "https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-${ARCH}" -O runtime-${ARCH}
fi

# Build the AppImage
echo "Building AppImage..."
ARCH=$ARCH ./appimagetool-${ARCH}.AppImage --runtime-file runtime-${ARCH} "$APPDIR" "Samoyed-${ARCH}.AppImage"

echo "AppImage built successfully: Samoyed-${ARCH}.AppImage"
