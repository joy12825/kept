# 收好（kept）的镜像：前端构建 → Go 编译并嵌入前端 → 带 OCR 工具的运行环境。
# 在仓库根目录构建：
#   docker build -f deploy/Dockerfile -t kept:0.1.0 --build-arg VERSION=v0.1.0 --build-arg COMMIT=$(git rev-parse --short HEAD) .
# 运行（只需一个数据卷）：
#   docker run -d --name kept -p 8000:8000 -v kept-data:/data kept:0.1.0

# ---- 1. 前端 ----
FROM node:22-bookworm-slim AS web
RUN npm install -g pnpm@12.6.0
WORKDIR /src/app
COPY app/package.json app/pnpm-lock.yaml app/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY app/ ./
# v1.13: TAGS= builds the open-source web pages too (no paid modules)
ARG TAGS=
RUN if [ "${TAGS}" = ee ]; then pnpm build; else pnpm build:oss; fi

# ---- 2. 服务端（纯 Go，SQLite 以 WASM 运行，不需要 cgo）----
FROM golang:1.26-bookworm AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/app/dist/ ./internal/web/dist/
ARG VERSION=dev
ARG COMMIT=none
# v1.13: the official image has the paid modules; --build-arg TAGS= builds the open-source one
ARG TAGS=
RUN CGO_ENABLED=0 go build -tags "${TAGS}" -trimpath \
      -ldflags "-s -w -X kept/server/internal/version.Version=${VERSION} -X kept/server/internal/version.Commit=${COMMIT} -X kept/server/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /out/kept ./cmd/kept

# ---- 3. 运行环境 ----
FROM debian:trixie-slim
# ocrmypdf + tesseract（中文简体、英文）做 OCR；poppler 取文字层、生成缩略图；ghostscript、qpdf 是 ocrmypdf 的依赖；
# poppler-data 让没有内嵌字体的中文 PDF 也能正确渲染；curl 用于健康检查；
# fonts-droid-fallback 是分享加水印用的中文字体（v1.9.2，TrueType，约 4 MB）。
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ocrmypdf tesseract-ocr tesseract-ocr-chi-sim tesseract-ocr-eng tesseract-ocr-osd \
      poppler-utils poppler-data ghostscript qpdf \
      libheif-examples libheif-plugin-libde265 \
      fonts-droid-fallback ca-certificates tzdata curl \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 1000 --home-dir /data --shell /usr/sbin/nologin kept \
 && mkdir -p /data && chown kept:kept /data

COPY --from=server /out/kept /usr/local/bin/kept
# 第三方许可证清单（v1.13.4）；镜像里各程序的版权文件在 /usr/share/doc/<包名>/copyright
COPY THIRD_PARTY_NOTICES.md /usr/share/doc/kept/THIRD_PARTY_NOTICES.md

ENV KEPT_DATA_DIR=/data \
    KEPT_HTTP_ADDR=:8000 \
    TZ=Asia/Shanghai
USER kept
VOLUME /data
EXPOSE 8000
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8000/healthz >/dev/null || exit 1
# 收到 SIGTERM 后等待进行中的任务（KEPT_SHUTDOWN_TIMEOUT，默认 30s）；docker stop 请给足时间：docker stop -t 40
STOPSIGNAL SIGTERM
ENTRYPOINT ["kept"]
CMD ["serve"]
