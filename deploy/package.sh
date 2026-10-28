#!/bin/sh
# 编译二进制发布包（由 server/Makefile 的 make release 调用，网页端须已构建并复制到 internal/web/dist）。
# 产物：dist/kept-<版本>-<系统>-<架构>.tar.gz 与 dist/SHA256SUMS
set -eu
VERSION=${1:?版本}
LDFLAGS=${2:?ldflags}
root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
rm -rf "$dist" && mkdir -p "$dist"

for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
	os=${target%/*}; arch=${target#*/}
	name="kept-$VERSION-$os-$arch"
	dir="$dist/$name"
	mkdir -p "$dir"
	echo "编译 $name"
	(cd "$root/server" && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -tags "${TAGS-}" -trimpath -ldflags "$LDFLAGS" -o "$dir/kept" ./cmd/kept)
	cp "$root/deploy/kept.env" "$dir/kept.env"
	cp "$root/deploy/README-binary.md" "$dir/README.md"
	cp "$root/THIRD_PARTY_NOTICES.md" "$dir/"  # 第三方许可证（v1.13.4）
	if [ "$os" = linux ]; then
		cp "$root/deploy/kept.service" "$root/deploy/install.sh" "$dir/"
	fi
	tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	rm -rf "$dir"
done

(cd "$dist" && shasum -a 256 *.tar.gz > SHA256SUMS)
echo "完成：$dist"
ls -lh "$dist"
