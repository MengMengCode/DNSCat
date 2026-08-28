#!/usr/bin/env bash
#
# DNSCat 发布脚本
#
# 打 tag 触发 .github/workflows/release.yml，跟踪运行状态，完成后核对
# Release 资产名与 deploy/install.sh 期望的下载名是否逐项对应。
#
# 用法:
#   scripts/release.sh v1.0.0              打 tag 并推送，触发构建与发布
#   scripts/release.sh v1.0.0 --dry-run    只做检查，不打 tag 不推送
#   scripts/release.sh v1.0.0 --retag      tag 已存在时先删除再重建（会删远端 tag）
#   scripts/release.sh --verify v1.0.0     不发布，只核对已有 Release 的资产
#   scripts/release.sh --dispatch v1.0.0   对已存在的 tag 手动触发 workflow 重跑
#
# 其他参数:
#   --no-watch    推送 tag 后不等待 Actions，立即退出
#
# 依赖: git、gh（GitHub CLI，需已 gh auth login）
#
set -euo pipefail

C_RST=$'\033[0m'; C_RED=$'\033[31m'; C_GRN=$'\033[32m'
C_YEL=$'\033[33m'; C_CYN=$'\033[36m'; C_BLD=$'\033[1m'
if [[ ! -t 1 ]]; then C_RST=""; C_RED=""; C_GRN=""; C_YEL=""; C_CYN=""; C_BLD=""; fi

step() { printf '\n%s==>%s %s%s%s\n' "${C_CYN}" "${C_RST}" "${C_BLD}" "$*" "${C_RST}"; }
ok()   { printf '%s  ok%s  %s\n' "${C_GRN}" "${C_RST}" "$*"; }
warn() { printf '%s warn%s %s\n' "${C_YEL}" "${C_RST}" "$*"; }
die()  { printf '\n%s错误:%s %s\n' "${C_RED}" "${C_RST}" "$*" >&2; exit 1; }

TAG=""
DRY_RUN="no"
RETAG="no"
VERIFY_ONLY="no"
DISPATCH="no"
WATCH="yes"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)  DRY_RUN="yes"; shift ;;
        --retag)    RETAG="yes"; shift ;;
        --verify)   VERIFY_ONLY="yes"; shift ;;
        --dispatch) DISPATCH="yes"; shift ;;
        --no-watch) WATCH="no"; shift ;;
        # 打印文件头注释块，遇到第一行非注释即停。
        # 不写死行号：注释增删后 sed -n '2,20p' 那种写法会把代码行也打出来。
        -h|--help)  awk 'NR>1 { if ($0 !~ /^#/) exit; sub(/^# ?/, ""); print }' "$0"; exit 0 ;;
        -*)         die "未知参数: $1" ;;
        *)          TAG="$1"; shift ;;
    esac
done

[[ -n "${TAG}" ]] || die "缺少 tag，例如: scripts/release.sh v1.0.0"
[[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] \
    || die "tag 必须形如 v1.2.3 或 v1.2.3-rc1（workflow 只监听 'v*'）"

command -v git >/dev/null 2>&1 || die "需要 git"
command -v gh  >/dev/null 2>&1 || die "需要 GitHub CLI (gh)：https://cli.github.com/"

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || die "不在 git 仓库里"
cd "${REPO_ROOT}"

# install.sh 期望的资产名。这份清单是本脚本的核对基准，
# 与 deploy/install.sh 的 download_release() 和 detect_arch() 保持一致。
ARCHES=(amd64 386 arm64 armv7)
PROGS=(dnscat-server dnscat-node dnscat)

expected_assets() {
    local a p
    for a in "${ARCHES[@]}"; do
        for p in "${PROGS[@]}"; do
            printf '%s_linux_%s\n' "${p}" "${a}"
        done
    done
    printf 'SHA256SUMS.txt\n'
}

# ---------------------------------------------------------------------------
# 静态一致性检查：确认 install.sh 与 release.yml 对得上
# 名字对不上不会报错，只会让一键安装静默退化成源码编译（要求目标机有 Go 和 Node），
# 所以在发布前先把这层契约验一遍。
# ---------------------------------------------------------------------------
check_contract() {
    step "核对 install.sh 与 release.yml 的产物命名契约"
    local sh="deploy/install.sh" yml=".github/workflows/release.yml"
    [[ -f "${sh}"  ]] || die "找不到 ${sh}"
    [[ -f "${yml}" ]] || die "找不到 ${yml}"

    local bad=0 a p name
    for a in "${ARCHES[@]}"; do
        grep -q -- "arch: '\{0,1\}${a}'\{0,1\}" "${yml}" \
            || { printf '  %s缺失%s release.yml 的 matrix 里没有架构 %s\n' "${C_RED}" "${C_RST}" "${a}"; bad=1; }
        grep -q -- "\"${a}\"" "${sh}" \
            || { printf '  %s缺失%s install.sh 的 detect_arch 不产出 %s\n' "${C_RED}" "${C_RST}" "${a}"; bad=1; }
    done
    for p in "${PROGS[@]}"; do
        name="${p}_linux_"
        grep -q -- "dist/${name}\${A}" "${yml}" \
            || { printf '  %s缺失%s release.yml 不产出 %s<arch>\n' "${C_RED}" "${C_RST}" "${name}"; bad=1; }
    done
    grep -q 'main_name="dnscat-server_linux_${ARCH}"' "${sh}" \
        || { printf '  %s不符%s install.sh 主控下载名与约定不一致\n' "${C_RED}" "${C_RST}"; bad=1; }
    grep -q 'main_name="dnscat-node_linux_${ARCH}"' "${sh}" \
        || { printf '  %s不符%s install.sh 边缘端下载名与约定不一致\n' "${C_RED}" "${C_RST}"; bad=1; }
    grep -q 'cli_name="dnscat_linux_${ARCH}"' "${sh}" \
        || { printf '  %s不符%s install.sh CLI 下载名与约定不一致\n' "${C_RED}" "${C_RST}"; bad=1; }
    grep -q 'SHA256SUMS.txt' "${sh}" \
        || { printf '  %s缺失%s install.sh 不校验 SHA256SUMS.txt\n' "${C_RED}" "${C_RST}"; bad=1; }
    grep -q 'releases/latest/download' "${sh}" \
        || { printf '  %s缺失%s install.sh 的 latest 下载地址不对\n' "${C_RED}" "${C_RST}"; bad=1; }

    [[ "${bad}" -eq 0 ]] || die "命名契约不一致，修好再发布"
    ok "4 架构 × 3 程序 + SHA256SUMS.txt，两边一致"

    if command -v bash >/dev/null 2>&1; then
        bash -n "${sh}" || die "${sh} 语法错误"
        ok "install.sh 语法检查通过"
    fi
}

# ---------------------------------------------------------------------------
# 发布前置检查
# ---------------------------------------------------------------------------
check_repo_state() {
    step "检查仓库状态"

    local dirty
    dirty="$(git status --porcelain --untracked-files=no)"
    if [[ -n "${dirty}" ]]; then
        printf '%s\n' "${dirty}"
        die "工作区有未提交改动。tag 会指向当前 HEAD，先提交或 stash"
    fi
    ok "工作区干净"

    local branch
    branch="$(git rev-parse --abbrev-ref HEAD)"
    printf '  当前分支: %s\n' "${branch}"

    git fetch --tags --quiet origin || warn "git fetch 失败，本地 tag 信息可能过期"

    local local_head remote_head
    local_head="$(git rev-parse HEAD)"
    remote_head="$(git rev-parse "origin/${branch}" 2>/dev/null || echo '')"
    if [[ -z "${remote_head}" ]]; then
        warn "远端没有 origin/${branch}"
    elif [[ "${local_head}" != "${remote_head}" ]]; then
        die "HEAD 与 origin/${branch} 不一致。CI 检出的是远端提交，先 git push"
    else
        ok "HEAD 与 origin/${branch} 一致 (${local_head:0:7})"
    fi

    if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
        if [[ "${RETAG}" == "yes" ]]; then
            warn "tag ${TAG} 已存在，--retag 将删除本地与远端的该 tag"
            if [[ "${DRY_RUN}" == "no" ]]; then
                git tag -d "${TAG}"
                git push origin ":refs/tags/${TAG}" || warn "远端 tag 删除失败（可能本就不存在）"
            fi
        else
            die "tag ${TAG} 已存在。换个版本号，或加 --retag 重建，或用 --dispatch 对该 tag 重跑 workflow"
        fi
    fi
}

check_gh_auth() {
    step "检查 gh 登录状态"
    gh auth status >/dev/null 2>&1 || die "gh 未登录，先执行: gh auth login"
    local slug
    slug="$(gh repo view --json nameWithOwner --jq .nameWithOwner)" \
        || die "无法读取仓库信息，确认当前目录关联了 GitHub 远端"
    ok "已登录，目标仓库: ${slug}"

    # install.sh 的默认下载仓库必须就是这个仓库，否则装的是别人的产物
    local want
    want="$(grep -o 'DNSCAT_REPO:-[^}]*' deploy/install.sh | head -1 | sed 's/DNSCAT_REPO:-//')"
    if [[ -n "${want}" && "${want}" != "${slug}" ]]; then
        warn "install.sh 默认下载仓库是 ${want}，当前仓库是 ${slug}，两者不一致"
    else
        ok "install.sh 默认下载仓库与当前仓库一致"
    fi
}

# ---------------------------------------------------------------------------
# 触发
# ---------------------------------------------------------------------------
push_tag() {
    step "创建并推送 tag ${TAG}"
    if [[ "${DRY_RUN}" == "yes" ]]; then
        warn "--dry-run：跳过 git tag 与 git push"
        return 0
    fi
    git tag -a "${TAG}" -m "DNSCat ${TAG}"
    git push origin "refs/tags/${TAG}"
    ok "已推送 ${TAG}，Actions 将自动开始构建"
}

dispatch_workflow() {
    step "手动触发 workflow（tag=${TAG}）"
    if [[ "${DRY_RUN}" == "yes" ]]; then
        warn "--dry-run：跳过 workflow_dispatch"
        return 0
    fi
    gh workflow run release.yml -f "tag=${TAG}" \
        || die "触发失败。确认 release.yml 已推送到默认分支"
    ok "已触发"
}

watch_run() {
    [[ "${WATCH}" == "yes" && "${DRY_RUN}" == "no" ]] || return 0
    step "等待 Actions 运行"

    # workflow 在 push 事件后需要几秒才能被排上队，拿不到 run id 时重试
    local run_id="" i
    for i in $(seq 1 20); do
        run_id="$(gh run list --workflow=release.yml --limit 1 --json databaseId --jq '.[0].databaseId' 2>/dev/null || echo '')"
        [[ -n "${run_id}" && "${run_id}" != "null" ]] && break
        sleep 3
    done
    if [[ -z "${run_id}" || "${run_id}" == "null" ]]; then
        warn "没取到 run id，去网页看: $(gh repo view --json url --jq .url)/actions"
        return 0
    fi

    printf '  run id: %s\n' "${run_id}"
    # 4 个架构并行构建 + 前端构建，通常 5~10 分钟
    if gh run watch "${run_id}" --exit-status; then
        ok "Actions 运行成功"
    else
        printf '\n'
        gh run view "${run_id}" --log-failed 2>/dev/null | tail -60 || true
        die "Actions 运行失败，详情: gh run view ${run_id} --log-failed"
    fi
}

# ---------------------------------------------------------------------------
# 核对 Release 资产
# ---------------------------------------------------------------------------
verify_release() {
    step "核对 Release ${TAG} 的资产"

    gh release view "${TAG}" >/dev/null 2>&1 || die "Release ${TAG} 不存在"

    local actual
    actual="$(gh release view "${TAG}" --json assets --jq '.assets[].name' | sort)"
    printf '  实际资产 %d 个:\n' "$(printf '%s\n' "${actual}" | grep -c . || true)"
    printf '%s\n' "${actual}" | sed 's/^/    /'
    printf '\n'

    local missing=0 name
    while IFS= read -r name; do
        if printf '%s\n' "${actual}" | grep -qxF "${name}"; then
            printf '  %s✓%s %s\n' "${C_GRN}" "${C_RST}" "${name}"
        else
            printf '  %s✗%s %s %s(install.sh 会因此退化成源码编译)%s\n' \
                "${C_RED}" "${C_RST}" "${name}" "${C_YEL}" "${C_RST}"
            missing=1
        fi
    done < <(expected_assets)

    [[ "${missing}" -eq 0 ]] || die "Release 资产不全，install.sh 无法下载安装"

    # latest 必须指向本次 tag：install.sh 默认从 /releases/latest/download 取
    local latest
    latest="$(gh release view --json tagName --jq .tagName 2>/dev/null || echo '')"
    if [[ "${latest}" == "${TAG}" ]]; then
        ok "latest 指向 ${TAG}，install.sh 的默认下载地址可用"
    else
        warn "latest 当前指向 ${latest}，而非 ${TAG}。install.sh 默认装的是 ${latest}"
    fi

    local prerelease
    prerelease="$(gh release view "${TAG}" --json isPrerelease --jq .isPrerelease 2>/dev/null || echo '')"
    if [[ "${prerelease}" == "true" ]]; then
        warn "${TAG} 被标记为 prerelease，/releases/latest/download 不会指向它"
    fi
}

# 实际发一次 HTTP 请求确认下载地址真的能取到东西，
# 而不是只看 API 里资产名齐全就下结论。
smoke_check_download() {
    step "实测下载地址可达性"
    command -v curl >/dev/null 2>&1 || { warn "没有 curl，跳过"; return 0; }

    local slug base name code
    slug="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
    base="https://github.com/${slug}/releases/latest/download"

    for name in "dnscat-server_linux_amd64" "dnscat-node_linux_arm64" "SHA256SUMS.txt"; do
        code="$(curl -sSL -o /dev/null -w '%{http_code}' "${base}/${name}" 2>/dev/null || echo '000')"
        if [[ "${code}" == "200" ]]; then
            printf '  %s✓%s %s -> HTTP 200\n' "${C_GRN}" "${C_RST}" "${name}"
        else
            printf '  %s✗%s %s -> HTTP %s\n' "${C_RED}" "${C_RST}" "${name}" "${code}"
            die "下载地址不可用，一键安装会失败"
        fi
    done
    ok "latest 下载地址可用"
}

print_next_steps() {
    local slug
    slug="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || echo 'OWNER/REPO')"
    local raw="https://raw.githubusercontent.com/${slug}/master/deploy/install.sh"

    printf '\n%s发布完成。在干净机器上验证一键安装：%s\n\n' "${C_BLD}" "${C_RST}"
    printf '  主控:\n'
    printf '    curl -fsSL %s | sudo bash -s -- --yes --role master\n\n' "${raw}"
    printf '  边缘端（主控装完后会打印填好令牌的现成命令）:\n'
    printf '    curl -fsSL %s | sudo bash -s -- --yes --role node --master-url http://<MASTER_IP>:8080 --node-id edge-01 --cluster-token <TOKEN> --public-ip <EDGE_IP>\n\n' "${raw}"
    printf '  日志里应出现「下载预编译产物」与「校验和核对通过」；\n'
    printf '  若出现「从源码编译」说明没走到下载路径，需回查资产名。\n'
}

main() {
    printf '%sDNSCat 发布: %s%s\n' "${C_BLD}" "${TAG}" "${C_RST}"

    check_contract

    if [[ "${VERIFY_ONLY}" == "yes" ]]; then
        check_gh_auth
        verify_release
        smoke_check_download
        return 0
    fi

    check_gh_auth

    if [[ "${DISPATCH}" == "yes" ]]; then
        git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null \
            || die "tag ${TAG} 不存在，--dispatch 只用于对已有 tag 重跑"
        dispatch_workflow
    else
        check_repo_state
        push_tag
    fi

    if [[ "${DRY_RUN}" == "yes" ]]; then
        printf '\n%s--dry-run 完成，未做任何改动。%s\n' "${C_YEL}" "${C_RST}"
        return 0
    fi

    watch_run
    verify_release
    smoke_check_download
    print_next_steps
}

main "$@"
