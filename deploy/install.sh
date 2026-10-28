#!/bin/sh
# 收好（kept）二进制部署的安装与升级（Linux + systemd）。在解压后的发布包目录里以 root 运行：
#   sudo ./install.sh              安装或升级（配置文件已存在时不覆盖）
#   sudo ./install.sh --with-deps  同时用 apt 安装 OCR 等依赖（Debian/Ubuntu）
set -eu

cd "$(dirname "$0")"
[ "$(id -u)" -eq 0 ] || { echo "请用 root 运行：sudo $0 $*"; exit 1; }
command -v systemctl >/dev/null || { echo "没有 systemd：请按 docs/部署.md 手动运行 kept serve"; exit 1; }
[ -x ./kept ] || { echo "当前目录没有 kept 程序，请在解压后的发布包目录里运行"; exit 1; }

if [ "${1:-}" = "--with-deps" ]; then
	command -v apt-get >/dev/null || { echo "--with-deps 只支持 apt（Debian/Ubuntu）；其他系统见 docs/部署.md 的依赖表"; exit 1; }
	apt-get update
	apt-get install -y --no-install-recommends ocrmypdf tesseract-ocr tesseract-ocr-chi-sim tesseract-ocr-eng tesseract-ocr-osd \
		poppler-utils poppler-data ghostscript qpdf ca-certificates tzdata
	# HEIC 照片解码（可选）：较老的发行版没有单独的解码插件包，装不上也不影响其他格式
	apt-get install -y --no-install-recommends libheif-examples libheif-plugin-libde265 2>/dev/null \
		|| apt-get install -y --no-install-recommends libheif-examples 2>/dev/null \
		|| echo "没能安装 libheif-examples：HEIC 照片暂不支持，其他格式不受影响"
fi

# 系统用户（不能登录），数据目录由 systemd 的 StateDirectory 创建
if ! id kept >/dev/null 2>&1; then
	useradd --system --user-group --home-dir /var/lib/kept --shell /usr/sbin/nologin kept
fi
mkdir -p /var/lib/kept && chown kept:kept /var/lib/kept && chmod 750 /var/lib/kept

upgrading=false
systemctl is-active --quiet kept && upgrading=true

install -m 755 ./kept /usr/local/bin/kept
install -d -m 755 /etc/kept
if [ ! -f /etc/kept/kept.env ]; then
	install -m 640 -g kept ./kept.env /etc/kept/kept.env
	echo "已写入配置 /etc/kept/kept.env"
else
	echo "保留已有配置 /etc/kept/kept.env（新的示例在 ./kept.env）"
fi
install -m 644 ./kept.service /etc/systemd/system/kept.service
# keptctl：以服务的身份和配置运行 kept 的命令（备份、导入、建账号……）
cat > /usr/local/bin/keptctl <<'CTL'
#!/bin/sh
# 以服务的身份和配置运行 kept 命令，例如：sudo keptctl user create --admin --email 邮箱 用户名
set -a; . /etc/kept/kept.env; set +a
exec runuser -u kept -- /usr/local/bin/kept "$@"
CTL
chmod 755 /usr/local/bin/keptctl
systemctl daemon-reload

echo
/usr/local/bin/kept version
echo
# 用服务的身份和配置检查一遍
/usr/local/bin/keptctl doctor || echo "（按上面的提示补齐后再启动）"

if $upgrading; then
	systemctl restart kept
	echo "已升级并重启。数据库迁移会在启动时自动执行。"
else
	cat <<'NEXT'

下一步：
  1. 按需修改 /etc/kept/kept.env
  2. 启动后在浏览器里打开会进入初始化页面创建管理员；也可以用命令行（会提示输入密码）：
     sudo keptctl user create --admin --email 你的邮箱 用户名
  3. 启动并开机自启：sudo systemctl enable --now kept
  4. 查看状态与日志：systemctl status kept；journalctl -u kept -f
NEXT
fi
