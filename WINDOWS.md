# Windows support

Barnard can be built for Windows with a native CGO toolchain. Audio support uses native OpenAL Soft, libopus, libopusfile/libogg, and RNNoise; `ffmpeg.exe` must also be on `PATH` for recording and file playback.

The easiest supported setup is MSYS2 MinGW-w64. Install the matching architecture's Go toolchain and development packages for OpenAL Soft, opus, opusfile, libogg, RNNoise, FFmpeg, and pkg-config. Build from its MinGW shell:

```
go build -o barnard.exe .
```

For cross-compilation from Linux, use the matching MinGW compiler and its pkg-config environment, for example:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
CC=x86_64-w64-mingw32-gcc \
PKG_CONFIG=x86_64-w64-mingw32-pkg-config \
go build -o barnard.exe .
```

The dependency include and library paths must point to Windows builds, not host Linux libraries.

The legacy `--fifo` command control is unavailable on Windows because it uses POSIX filesystem FIFOs. Windows defaults to no notification command; an explicitly configured notification command is run by `cmd.exe /C`.
