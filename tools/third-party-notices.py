#!/usr/bin/env python3
"""Writes THIRD_PARTY_NOTICES.md (v1.13.4): what Kept is built from and
ships with, under which licenses, with their texts.

  - Go modules linked into the server (both the open-source and the official
    build), read from the module cache;
  - the web pages' production packages, from `pnpm licenses list --prod`;
  - the programs and fonts in the Docker image, from the list below.

Run from the repository root after `go mod download` and `pnpm install`:

  python3 tools/third-party-notices.py > THIRD_PARTY_NOTICES.md
"""
import glob
import hashlib
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# Programs in the Docker image (deploy/Dockerfile), run as separate programs.
# The image's /usr/share/doc/<package>/copyright files are authoritative.
IMAGE = [
    ("ocrmypdf", "MPL-2.0", "OCR 流程"),
    ("tesseract-ocr、tesseract-ocr-chi-sim、tesseract-ocr-eng、tesseract-ocr-osd", "Apache-2.0（个别文件 MIT）", "文字识别及中英文语言包"),
    ("poppler-utils", "GPL-2.0 或 GPL-3.0", "取文字层、生成缩略图"),
    ("poppler-data", "GPL-2.0、MIT、BSD-3-Clause、AGPL-3.0-or-later（各文件不同）", "中文 PDF 的字符映射"),
    ("ghostscript", "AGPL-3.0-or-later（部分文件 GPL、Apache-2.0）", "ocrmypdf 的依赖"),
    ("qpdf", "Apache-2.0", "ocrmypdf 的依赖"),
    ("libheif-examples、libheif-plugin-libde265", "LGPL-3.0-or-later（部分文件 GPL-3.0-or-later、MIT、BSL-1.0）", "HEIC 照片"),
    ("fonts-droid-fallback", "Apache-2.0", "分享水印用的中文字体"),
    ("ca-certificates", "MPL-2.0 / GPL-2.0-or-later", "根证书"),
    ("tzdata", "公有领域", "时区数据"),
    ("curl", "curl 许可证（类 MIT）", "健康检查"),
]

LICENSE_FILES = re.compile(r"^(LICEN[CS]E|COPYING|NOTICE|UNLICENSE)([.-].*)?$", re.I)


def run(cmd, cwd):
    return subprocess.run(cmd, cwd=cwd, check=True, capture_output=True, text=True).stdout


def spdx(text):
    t = " ".join(text.split())
    if "GNU AFFERO GENERAL PUBLIC LICENSE" in t:
        return "AGPL-3.0"
    if "GNU LESSER GENERAL PUBLIC LICENSE" in t:
        return "LGPL"
    if "GNU GENERAL PUBLIC LICENSE" in t:
        return "GPL"
    if "Mozilla Public License" in t and "2.0" in t:
        return "MPL-2.0"
    if "Apache License" in t and "Version 2.0" in t:
        return "Apache-2.0"
    if "Redistribution and use in source and binary forms" in t:
        return "BSD-3-Clause" if ("Neither the name" in t or "names of its contributors" in t) else "BSD-2-Clause"
    if "Permission is hereby granted, free of charge" in t:
        return "MIT"
    if "Permission to use, copy, modify, and/or distribute" in t or "ISC License" in t:
        return "ISC"
    if "This is free and unencumbered software released into the public domain" in t:
        return "Unlicense"
    return "见许可证原文"


def license_texts(d):
    out = []
    for p in sorted(glob.glob(os.path.join(d, "*"))):
        if os.path.isfile(p) and LICENSE_FILES.match(os.path.basename(p)):
            with open(p, encoding="utf-8", errors="replace") as f:
                out.append(f.read().strip())
    return out


def go_modules():
    server = os.path.join(ROOT, "server")
    mods = set()
    for tags in ([], ["-tags", "ee"]):
        out = run(["go", "list", "-deps", *tags, "-f", "{{with .Module}}{{.Path}} {{.Version}}{{end}}", "./cmd/kept"], server)
        mods |= {l for l in out.splitlines() if l and not l.startswith("kept/server")}
    rows = []
    for m in sorted(mods):
        path, version = m.split(" ", 1)
        d = run(["go", "list", "-m", "-f", "{{.Dir}}", f"{path}@{version}"], server).strip()
        texts = license_texts(d)
        rows.append((path, version, spdx(texts[0]) if texts else "未找到许可证文件", texts))
    return rows


def web_packages():
    data = json.loads(run(["pnpm", "licenses", "list", "--prod", "--json"], os.path.join(ROOT, "app")))
    rows = []
    for lic, pkgs in data.items():
        for p in pkgs:
            texts = []
            for d in p.get("paths", []):
                texts = license_texts(d)
                if texts:
                    break
            rows.append((p["name"], ", ".join(p.get("versions", [])), lic, texts))
    return sorted(rows)


def main():
    go = go_modules()
    web = web_packages()
    texts = {}  # hash -> (text, [names])

    def note(name, ts):
        for t in ts:
            h = hashlib.sha256(t.encode()).hexdigest()[:12]
            texts.setdefault(h, (t, []))[1].append(name)

    w = sys.stdout.write
    w("# 第三方许可证\n\n")
    w("收好（Kept）由下面这些第三方软件构建，或在镜像里附带它们。每一项按各自的许可证使用；"
      "这份清单由 `tools/third-party-notices.py` 生成，相同的许可证原文只列一次。\n\n")
    w("## 服务端（Go 模块，编译进 kept 程序）\n\n| 模块 | 版本 | 许可证 |\n|---|---|---|\n")
    for path, version, lic, ts in go:
        w(f"| {path} | {version} | {lic} |\n")
        note(f"{path} {version}", ts)
    w("\n## 网页端（打包进网页的 npm 包）\n\n| 包 | 版本 | 许可证 |\n|---|---|---|\n")
    for name, version, lic, ts in web:
        w(f"| {name} | {version} | {lic} |\n")
        note(f"{name} {version}", ts)
    w("\n## 镜像里附带的程序\n\n"
      "官方 Docker 镜像基于 Debian，下面这些程序以独立程序的方式被调用，没有和收好的代码链接在一起。"
      "许可证以镜像内 `/usr/share/doc/<包名>/copyright` 为准；源码可以从 Debian 获取"
      "（`apt-get source <包名>`，或 https://sources.debian.org）。\n\n| Debian 软件包 | 许可证 | 用途 |\n|---|---|---|\n")
    for name, lic, use in IMAGE:
        w(f"| {name} | {lic} | {use} |\n")
    w("\n## 嵌入的字体\n\n| 字体 | 许可证 | 用途 |\n|---|---|---|\n"
      "| Noto Sans SC（子集，`server/internal/demo/fonts`） | SIL Open Font License 1.1 | 演示数据的 PDF |\n\n")
    with open(os.path.join(ROOT, "server/internal/demo/fonts/OFL.txt"), encoding="utf-8") as f:
        note("Noto Sans SC", [f.read().strip()])
    w("## 许可证原文\n")
    for h, (t, names) in sorted(texts.items(), key=lambda kv: kv[1][1][0].lower()):
        w(f"\n### {spdx(t)}：{'、'.join(sorted(set(names)))}\n\n```text\n{t}\n```\n")


if __name__ == "__main__":
    main()
