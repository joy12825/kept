# 收好（kept）二进制发布包

一个程序文件 `kept`，网页端已经包含在里面，数据库默认是内置的 SQLite。完整说明见仓库 `docs/部署.md`“二进制部署”。

## Linux（systemd）

```sh
tar xzf kept-<版本>-linux-amd64.tar.gz && cd kept-<版本>-linux-amd64
sudo ./install.sh --with-deps   # Debian/Ubuntu：同时安装 OCR 等依赖；其他发行版先自己装好依赖，再运行 sudo ./install.sh
```

安装脚本会：创建系统用户 `kept`，把程序装到 `/usr/local/bin/kept`（另有 `keptctl`：以服务的身份和配置运行 kept 的命令，如 `sudo keptctl backup /备份目录`），配置放在 `/etc/kept/kept.env`（已存在时不覆盖），数据放在 `/var/lib/kept`，安装 systemd 服务，并运行 `kept doctor` 检查依赖。升级时重新运行同一个脚本即可，数据和配置不动。

## macOS

```sh
brew install ocrmypdf tesseract-lang poppler libheif
tar xzf kept-<版本>-darwin-arm64.tar.gz && cd kept-<版本>-darwin-arm64
xattr -d com.apple.quarantine kept   # 解除下载隔离
export KEPT_DATA_DIR=~/kept-data
./kept doctor && ./kept user create --admin --email 你的邮箱 用户名 && ./kept serve
```

浏览器打开 http://127.0.0.1:8000 。
