# AppImage Build and Usage

This directory contains the necessary files and scripts to build Samoyed as an AppImage.

## What is AppImage?

AppImage is a format for distributing portable software on Linux without requiring installation or root permissions. AppImages are self-contained applications that include all dependencies and can run on most Linux distributions.

## Building the AppImage

To build the AppImage locally:

1. Make sure you have built all the binaries first:
   ```bash
   make cmds
   ```

2. Run the AppImage build script:
   ```bash
   ./appimage/build-appimage.sh
   ```

This will:
- Create an AppDir structure with all binaries and metadata
- Download the necessary AppImage tools (appimagetool and runtime)
- Package everything into a `Samoyed-x86_64.AppImage` file

The build artifacts are excluded from git via `.gitignore`.

## Using the AppImage

Once you have the AppImage (either built locally or downloaded from a release):

1. Make it executable (if not already):
   ```bash
   chmod +x Samoyed-*.AppImage
   ```

2. Run it:
   ```bash
   ./Samoyed-*.AppImage [arguments]
   ```

The AppImage runs the `direwolf` binary by default. All command-line arguments are passed through to it.

## Files in this directory

- `build-appimage.sh` - Script to build the AppImage
- `io.github.doismellburning.samoyed.desktop` - Desktop entry file for application integration
- `samoyed.svg` - Application icon
- `io.github.doismellburning.samoyed.appdata.xml` - AppStream metadata

## CI/CD Integration

The GitHub Actions workflow automatically builds AppImages for each release:
- Builds all binaries
- Creates the AppImage
- Uploads it as a release artifact alongside the tarball

## Requirements

The AppImage includes all binaries from the `dist/` directory and data files from `data/`.

## Learn More

For more information about AppImage, visit: https://appimage.org/
