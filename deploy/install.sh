#!/usr/bin/env bash
#
# DnsCat installer. See --help for usage.
#
# Supported: linux/amd64, linux/arm64
#

set -euo pipefail

# ---------------------------- defaults ----------------------------
MODE="binary"
ROLE="master"
PREFIX="/usr/local/bin"
CONFIG_DIR="/etc/dnscat"
DATA_DIR="/var/lib/dnscat"
SERVICE_USER="dnscat"
HTTP_PORT="8080"
DNS_PORT="53"
DB_DRIVER="sqlite"
DB_DSN=""
MASTER_URL=""
NODE_ID=""
CLUSTER_TOKEN=""
PUBLIC_IP=""
BINARY_DIR=""
VERSION="${DNSCAT_VERSION:-latest}"
ASSUME_YES="no"
DO_UPGRADE="no"
DO_UNINSTALL="no"
DO_PURGE="no"
FROM_SOURCE="no"

# 预编译产物下载地址。开源发布后把 DNSCAT_REPO 换成实际仓库即可；
# 也可用 DNSCAT_RELEASE_BASE 直接指定完整前缀（例如内网镜像站）。
DNSCAT_REPO="${DNSCAT_REPO:-}"
DNSCAT_RELEASE_BASE="${DNSCAT_RELEASE_BASE:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

ARCH=""
WORK_DIR=""

# ---------------------------- output helpers ----------------------------
if [[ -t 1 ]]; then
    C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'
    C_CYA=$'\033[36m'; C_BLD=$'\033[1m'; C_RST=$'\033[0m'
else
    C_RED=""; C_GRN=""; C_YEL=""; C_CYA=""; C_BLD=""; C_RST=""
fi

log()  { printf '%s==>%s %s\n' "${C_CYA}" "${C_RST}" "$*"; }
ok()   { printf '%s  ok%s %s\n' "${C_GRN}" "${C_RST}" "$*"; }
warn() { printf '%s  !!%s %s\n' "${C_YEL}" "${C_RST}" "$*" >&2; }
die()  { printf '%s错误:%s %s\n' "${C_RED}" "${C_RST}" "$*" >&2; exit 1; }
hr()   { printf -- '--------------------------------------------------------------------------------\n'; }

usage() {
    cat <<'USAGE_EOF'
DnsCat 一键安装脚本

二进制安装（默认，无需 Docker）：
  sudo ./deploy/install.sh
  sudo ./deploy/install.sh --role node --master-url http://203.0.113.10:8080 \
       --node-id edge-fra-01 --cluster-token <主控令牌> --public-ip 203.0.113.20

Docker 安装：
  sudo ./deploy/install.sh --mode docker
  sudo ./deploy/install.sh --mode docker --role node --master-url ... \
       --node-id ... --cluster-token ... --public-ip ...

其他：
  sudo ./deploy/install.sh --upgrade      仅替换二进制并重启，保留配置与数据
  sudo ./deploy/install.sh --uninstall    卸载（默认保留数据，加 --purge 一并删除）

支持架构：linux/amd64、linux/arm64

关于 53 端口：
  · 二进制模式会自动识别占用者并让开 53。systemd-resolved 采用关闭 stub 监听的
    方式处理，宿主机自身的域名解析不受影响；无法安全处理的占用者会中止安装并
    给出处置建议。
  · Docker 模式不改动宿主服务，检测到占用即中止并打印处置步骤，由使用者先解决。

可用参数：
  --mode binary|docker      安装方式（默认 binary）
  --role master|node        安装角色（默认 master）
  --prefix DIR              二进制安装目录（默认 /usr/local/bin）
  --config-dir DIR          配置目录（默认 /etc/dnscat）
  --data-dir DIR            数据目录（默认 /var/lib/dnscat）
  --http-port PORT          Web 控制台端口（默认 8080）
  --dns-port PORT           权威 DNS 端口（默认 53）
  --db sqlite|mysql         主控数据库类型（默认 sqlite）
  --db-dsn DSN              自定义数据库连接串（指定后 --db 失效）
  --master-url URL          被控节点回连的主控地址（role=node 必填）
  --node-id ID              节点标识，集群内唯一（role=node 必填）
  --cluster-token TOKEN     集群令牌（role=node 必填；master 留空则随机生成）
  --public-ip IP            节点对外公网 IP（role=node 建议填写）
  --binary-dir DIR          使用指定目录下已有的二进制，跳过下载与编译
  --from-source             强制从源码编译（需要 Go；主控还需要 Node.js）
  --version VER             安装指定版本（默认 latest）
  -y, --yes                 非交互，自动确认所有处置动作
  --upgrade                 仅替换二进制并重启，保留配置与数据
  --uninstall               卸载服务
  --purge                   配合 --uninstall，一并删除配置与数据
  -h, --help                显示本帮助

环境变量：
  DNSCAT_REPO           预编译产物所在仓库，形如 owner/repo
  DNSCAT_RELEASE_BASE   直接指定下载地址前缀，优先于 DNSCAT_REPO
  DNSCAT_VERSION        等同 --version
USAGE_EOF
}

# ---------------------------- arg parsing ----------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode)          MODE="${2:-}"; shift 2 ;;
        --role)          ROLE="${2:-}"; shift 2 ;;
        --prefix)        PREFIX="${2:-}"; shift 2 ;;
        --config-dir)    CONFIG_DIR="${2:-}"; shift 2 ;;
        --data-dir)      DATA_DIR="${2:-}"; shift 2 ;;
        --http-port)     HTTP_PORT="${2:-}"; shift 2 ;;
        --dns-port)      DNS_PORT="${2:-}"; shift 2 ;;
        --db)            DB_DRIVER="${2:-}"; shift 2 ;;
        --db-dsn)        DB_DSN="${2:-}"; shift 2 ;;
        --master-url)    MASTER_URL="${2:-}"; shift 2 ;;
        --node-id)       NODE_ID="${2:-}"; shift 2 ;;
        --cluster-token) CLUSTER_TOKEN="${2:-}"; shift 2 ;;
        --public-ip)     PUBLIC_IP="${2:-}"; shift 2 ;;
        --binary-dir)    BINARY_DIR="${2:-}"; shift 2 ;;
        --from-source)   FROM_SOURCE="yes"; shift ;;
        --version)       VERSION="${2:-}"; shift 2 ;;
        -y|--yes)        ASSUME_YES="yes"; shift ;;
        --upgrade)       DO_UPGRADE="yes"; shift ;;
        --uninstall)     DO_UNINSTALL="yes"; shift ;;
        --purge)         DO_PURGE="yes"; shift ;;
        -h|--help)       usage; exit 0 ;;
        *)               die "未知参数: $1（--help 查看用法）" ;;
    esac
done

[[ "${MODE}" == "binary" || "${MODE}" == "docker" ]] || die "--mode 只能是 binary 或 docker"
[[ "${ROLE}" == "master" || "${ROLE}" == "node" ]]   || die "--role 只能是 master 或 node"

SERVICE_NAME="dnscat-server"
if [[ "${ROLE}" == "node" ]]; then
    SERVICE_NAME="dnscat-node"
fi

# ---------------------------- environment ----------------------------
require_root() {
    [[ "${EUID}" -eq 0 ]] || die "需要 root 权限，请用 sudo 执行"
}

detect_os() {
    [[ "$(uname -s)" == "Linux" ]] || die "本脚本仅支持 Linux（检测到 $(uname -s)）"
}

detect_arch() {
    local m
    m="$(uname -m)"
    case "${m}" in
        x86_64|amd64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l|armv7|armhf)
            die "检测到 32 位 ARM ${m}，当前仅提供 amd64 与 arm64 产物，请用 --from-source 自行编译"
            ;;
        *)
            die "不支持的 CPU 架构 ${m}，仅支持 amd64 与 arm64"
            ;;
    esac
}

has_systemd() { [[ -d /run/systemd/system ]]; }

confirm() {
    [[ "${ASSUME_YES}" == "yes" ]] && return 0
    local reply=""
    printf '%s [y/N] ' "$1"
    if [[ -r /dev/tty ]]; then
        read -r reply < /dev/tty || reply=""
    else
        read -r reply || reply=""
    fi
    [[ "${reply}" == "y" || "${reply}" == "Y" ]]
}

rand_hex() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex "$1"
    else
        head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
    fi
}

# ---------------------------- port 53 handling ----------------------------

# port_listeners 输出占用指定端口的 "进程名:PID" 列表（去重）。
port_listeners() {
    local port="$1" out=""
    if command -v ss >/dev/null 2>&1; then
        out="$(ss -Hlnptu 2>/dev/null \
              | awk -v pat=":${port}\$" '$5 ~ pat' \
              | grep -oE '\("[^"]+",pid=[0-9]+' \
              | sed -E 's/^\("([^"]+)",pid=([0-9]+)/\1:\2/' || true)"
    elif command -v netstat >/dev/null 2>&1; then
        out="$(netstat -lnptu 2>/dev/null \
              | awk -v pat=":${port}\$" '$4 ~ pat {print $NF}' \
              | grep -E '^[0-9]+/' \
              | sed -E 's#^([0-9]+)/(.*)$#\2:\1#' || true)"
    elif command -v lsof >/dev/null 2>&1; then
        out="$(lsof -nP -iTCP:"${port}" -iUDP:"${port}" 2>/dev/null \
              | awk 'NR>1 {print $1":"$2}' || true)"
    fi
    printf '%s\n' "${out}" | sed '/^$/d' | sort -u
}

port_is_busy() {
    [[ -n "$(port_listeners "$1")" ]]
}

describe_port() {
    local port="$1" line=""
    log "端口 ${port} 当前占用情况:"
    while IFS= read -r line; do
        [[ -z "${line}" ]] && continue
        printf '     %s\n' "${line}"
    done < <(port_listeners "${port}")
}

# free_systemd_resolved 关闭 systemd-resolved 的 53 stub 监听。
# 只关 stub，不停用服务本身，这样宿主机的域名解析完全不受影响。
free_systemd_resolved() {
    log "占用者是 systemd-resolved，将关闭其 53 stub 监听（保留宿主机解析能力）"
    if ! confirm "  写入 /etc/systemd/resolved.conf.d/dnscat.conf 并重启 systemd-resolved?"; then
        die "已取消。可改用 --dns-port 指定其他端口，或手动关闭 DNSStubListener 后重试"
    fi

    install -d -m 0755 /etc/systemd/resolved.conf.d
    cat > /etc/systemd/resolved.conf.d/dnscat.conf <<'RESOLVED_EOF'
# 由 DnsCat install.sh 写入。
# DnsCat 需要独占 53 端口对外提供权威解析，故关闭 systemd-resolved 的本地 stub 监听。
# systemd-resolved 服务本身继续运行，宿主机域名解析不受影响
# （/etc/resolv.conf 指向 /run/systemd/resolve/resolv.conf 即可）。
# 卸载 DnsCat 时删除本文件并重启 systemd-resolved 即可恢复原状。
[Resolve]
DNSStubListener=no
RESOLVED_EOF
    ok "已写入 /etc/systemd/resolved.conf.d/dnscat.conf"

    # stub 关闭后 /etc/resolv.conf 若仍指向 127.0.0.53 就解析不了域名，
    # 必须切到 systemd-resolved 提供的真实上游列表。
    if [[ -e /etc/resolv.conf ]] && grep -qE '^[[:space:]]*nameserver[[:space:]]+127\.0\.0\.53' /etc/resolv.conf 2>/dev/null; then
        if [[ -e /run/systemd/resolve/resolv.conf ]]; then
            local backup
            backup="/etc/resolv.conf.dnscat-backup.$(date +%Y%m%d%H%M%S)"
            cp -aL /etc/resolv.conf "${backup}" 2>/dev/null || true
            ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf
            ok "已将 /etc/resolv.conf 指向 /run/systemd/resolve/resolv.conf，原文件备份为 ${backup}"
        else
            warn "/etc/resolv.conf 指向 127.0.0.53 但未找到 /run/systemd/resolve/resolv.conf"
            warn "关闭 stub 监听后宿主机可能无法解析域名，请手动把 nameserver 改为可用上游"
        fi
    fi

    systemctl restart systemd-resolved \
        || die "重启 systemd-resolved 失败，请检查 systemctl status systemd-resolved"
    ok "systemd-resolved 已重启，stub 监听已关闭"

    if command -v getent >/dev/null 2>&1; then
        if getent hosts cloudflare.com >/dev/null 2>&1 || getent hosts debian.org >/dev/null 2>&1; then
            ok "宿主机域名解析正常"
        else
            warn "宿主机域名解析测试未通过，请检查 /etc/resolv.conf 是否指向可用上游"
        fi
    fi
    return 0
}

# stop_host_dns_service 停用并禁用宿主机上的某个 DNS 服务。
stop_host_dns_service() {
    local svc="$1"
    if ! systemctl list-unit-files 2>/dev/null | grep -q "^${svc}\.service"; then
        return 1
    fi
    log "占用者是 ${svc}，需要停用它才能让 DnsCat 绑定 ${DNS_PORT}"
    warn "停用后本机将不再由 ${svc} 提供解析服务"
    if ! confirm "  执行 systemctl disable --now ${svc}?"; then
        die "已取消。请手动处理 ${svc} 后重试，或用 --dns-port 换端口"
    fi
    systemctl disable --now "${svc}" || warn "停用 ${svc} 返回非零，继续复验端口"
    ok "已停用 ${svc}"

    if [[ -e /etc/resolv.conf ]] && grep -qE '^[[:space:]]*nameserver[[:space:]]+127\.0\.0\.1' /etc/resolv.conf 2>/dev/null; then
        warn "/etc/resolv.conf 仍指向 127.0.0.1，而本机解析服务刚被停用"
        warn "请把 nameserver 改为可用的上游 DNS，否则宿主机将无法解析域名"
    fi
    return 0
}

# free_port_53_binary 在二进制模式下让开 53。
# 只处理能安全、可逆地让开的已知占用者，其余一律中止，不擅自杀进程。
free_port_53_binary() {
    if ! port_is_busy "${DNS_PORT}"; then
        ok "端口 ${DNS_PORT} 空闲"
        return 0
    fi

    describe_port "${DNS_PORT}"

    local names
    names="$(port_listeners "${DNS_PORT}" | cut -d: -f1 | sort -u)"

    local unknown=()
    local n
    while IFS= read -r n; do
        [[ -z "${n}" ]] && continue
        case "${n}" in
            systemd-resolve|systemd-resolved)
                free_systemd_resolved || unknown+=("${n}")
                ;;
            dnsmasq)
                stop_host_dns_service dnsmasq || unknown+=("${n}")
                ;;
            named|bind9)
                # Debian 系单元名是 bind9，RHEL 系是 named，两个都试
                stop_host_dns_service named || stop_host_dns_service bind9 || unknown+=("${n}")
                ;;
            unbound)
                stop_host_dns_service unbound || unknown+=("${n}")
                ;;
            pdns_server|pdns)
                stop_host_dns_service pdns || unknown+=("${n}")
                ;;
            coredns)
                stop_host_dns_service coredns || unknown+=("${n}")
                ;;
            dnscat-server|dnscat-node)
                # 占用者是 DnsCat 自己（上一次安装留下的服务）。
                # 重装/升级时本来就要替换它，直接停掉，无需向使用者确认。
                log "端口被上一次安装的 ${n} 占用，重装前先停止它"
                systemctl stop "${n}" 2>/dev/null || true
                # 兜底：可能是手工拉起的进程而非 systemd 服务
                pkill -x "${n}" 2>/dev/null || true
                ok "已停止 ${n}"
                ;;
            pihole-FTL)
                warn "检测到 Pi-hole (pihole-FTL) 占用 ${DNS_PORT}"
                warn "Pi-hole 是完整的 DNS 服务栈，自动停用可能连带影响其界面与拦截功能"
                warn "脚本不做自动处置，请先决定二者取舍，或用 --dns-port 换端口"
                unknown+=("${n}")
                ;;
            *)
                unknown+=("${n}")
                ;;
        esac
    done <<< "${names}"

    if [[ ${#unknown[@]} -gt 0 ]]; then
        hr
        warn "以下占用者无法自动安全处理: ${unknown[*]}"
        warn "请任选其一后重跑本脚本:"
        warn "  1) 停用该服务，并确认宿主机仍能正常解析域名"
        warn "  2) 让该服务只监听内网地址，把 ${DNS_PORT} 让给 DnsCat"
        warn "  3) 用 --dns-port 把 DnsCat 换到其他端口。注意权威 DNS 对外必须是 53，"
        warn "     换端口只适用于前面另有转发或负载均衡的场景"
        hr
        die "端口 ${DNS_PORT} 仍被占用，已中止安装"
    fi

    # 处置后复验，给内核一点时间回收监听
    local i
    for i in 1 2 3 4 5; do
        if ! port_is_busy "${DNS_PORT}"; then
            ok "端口 ${DNS_PORT} 已释放"
            return 0
        fi
        sleep 1
    done

    describe_port "${DNS_PORT}"
    die "已尝试释放但端口 ${DNS_PORT} 仍被占用，请手动处理后重试"
}

# check_port_53_docker 只检测不改动，按要求在日志中给出处置指引。
check_port_53_docker() {
    if ! port_is_busy "${DNS_PORT}"; then
        ok "端口 ${DNS_PORT} 空闲"
        return 0
    fi

    local compose_name="docker-compose.master.yml"
    if [[ "${ROLE}" == "node" ]]; then
        compose_name="docker-compose.node.yml"
    fi

    describe_port "${DNS_PORT}"
    hr
    {
        printf '%s%s端口 %s 已被占用，Docker 模式不会自动改动宿主机服务。%s\n' \
            "${C_BLD}" "${C_YEL}" "${DNS_PORT}" "${C_RST}"
        printf '容器映射 %s 端口会直接失败，请先自行解决占用，再重跑本脚本。\n\n' "${DNS_PORT}"
        printf '常见处置方式:\n'
        printf '  1) systemd-resolved（Ubuntu / Debian 最常见）\n'
        printf '       sudo mkdir -p /etc/systemd/resolved.conf.d\n'
        printf '       printf "[Resolve]\\nDNSStubListener=no\\n" | sudo tee /etc/systemd/resolved.conf.d/dnscat.conf\n'
        printf '       sudo ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf\n'
        printf '       sudo systemctl restart systemd-resolved\n'
        printf '     只关本地 stub 监听，systemd-resolved 继续运行，宿主机解析不受影响。\n\n'
        printf '  2) dnsmasq / named / bind9 / unbound 等\n'
        printf '       sudo systemctl disable --now <服务名>\n'
        printf '     停用后请确认 /etc/resolv.conf 指向可用的上游 DNS。\n\n'
        printf '  3) 该端口必须留给现有服务时\n'
        printf '       改用二进制安装并换端口: sudo ./deploy/install.sh --dns-port 5353\n'
        printf '       或自行修改 deploy/%s 的端口映射\n\n' "${compose_name}"
        printf '确认端口已空闲: ss -lnptu | grep ":%s"\n' "${DNS_PORT}"
    } >&2
    hr
    die "请先解决 ${DNS_PORT} 端口占用后重新执行安装"
}

# ---------------------------- binary acquisition ----------------------------

BIN_SERVER=""
BIN_NODE=""
BIN_CLI=""

release_base() {
    if [[ -n "${DNSCAT_RELEASE_BASE}" ]]; then
        printf '%s' "${DNSCAT_RELEASE_BASE%/}"
        return 0
    fi
    if [[ -n "${DNSCAT_REPO}" ]]; then
        if [[ "${VERSION}" == "latest" ]]; then
            printf 'https://github.com/%s/releases/latest/download' "${DNSCAT_REPO}"
        else
            printf 'https://github.com/%s/releases/download/%s' "${DNSCAT_REPO}" "${VERSION}"
        fi
        return 0
    fi
    return 1
}

download_to() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 --connect-timeout 15 -o "${dest}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -T 15 -t 3 -O "${dest}" "${url}"
    else
        die "需要 curl 或 wget 才能下载预编译产物，也可用 --from-source 从源码编译"
    fi
}

# arch_matches 判断 ELF 文件是否与本机架构一致。
# bin/ 里常有为别的架构交叉编译的产物，直接拿来用会装出一个跑不起来的服务，
# 且报错信息极具迷惑性，所以必须先确认架构。
arch_matches() {
    local f="$1" desc=""
    command -v file >/dev/null 2>&1 || return 1
    desc="$(file -bL "${f}" 2>/dev/null)" || return 1
    case "${ARCH}" in
        amd64) [[ "${desc}" == *"x86-64"* ]] ;;
        arm64) [[ "${desc}" == *"aarch64"* ]] ;;
        *)     return 1 ;;
    esac
}

# pick_binary 在目录中挑一个可用的二进制。
# 带架构后缀的名字直接采信；不带后缀的必须经 file 确认架构一致才用，
# 无法确认时跳过，宁可回退到下载或源码编译。
pick_binary() {
    local dir="$1" suffixed="$2"
    shift 2
    if [[ -f "${dir}/${suffixed}" ]]; then
        printf '%s' "${dir}/${suffixed}"
        return 0
    fi
    local cand
    for cand in "$@"; do
        if [[ -f "${dir}/${cand}" ]] && arch_matches "${dir}/${cand}"; then
            printf '%s' "${dir}/${cand}"
            return 0
        fi
    done
    return 1
}

use_local_binaries() {
    local dir="$1"
    [[ -d "${dir}" ]] || return 1

    if [[ "${ROLE}" == "master" ]]; then
        local s c
        s="$(pick_binary "${dir}" "dnscat-server_linux_${ARCH}" "dnscat-server" "server_linux" "server")" || return 1
        c="$(pick_binary "${dir}" "dnscat_linux_${ARCH}" "dnscat" "dnscat_linux")" || c=""
        BIN_SERVER="${s}"
        BIN_CLI="${c}"
    else
        local nd
        nd="$(pick_binary "${dir}" "dnscat-node_linux_${ARCH}" "dnscat-node" "node_linux" "node")" || return 1
        BIN_NODE="${nd}"
    fi
    ok "使用已有二进制: ${dir} (linux/${ARCH})"
    return 0
}

download_release() {
    local base
    base="$(release_base)" || return 1

    log "下载预编译产物: ${base} (linux/${ARCH})"
    local out="${WORK_DIR}/bin"
    install -d -m 0755 "${out}"

    local main_name
    if [[ "${ROLE}" == "master" ]]; then
        main_name="dnscat-server_linux_${ARCH}"
    else
        main_name="dnscat-node_linux_${ARCH}"
    fi

    if ! download_to "${base}/${main_name}" "${out}/${main_name}"; then
        warn "下载 ${main_name} 失败"
        return 1
    fi
    chmod +x "${out}/${main_name}"

    if [[ "${ROLE}" == "master" ]]; then
        BIN_SERVER="${out}/${main_name}"
        # CLI 只是辅助工具，下载失败不影响主服务安装，降级为警告
        local cli_name="dnscat_linux_${ARCH}"
        if download_to "${base}/${cli_name}" "${out}/${cli_name}" 2>/dev/null; then
            chmod +x "${out}/${cli_name}"
            BIN_CLI="${out}/${cli_name}"
        else
            warn "未能下载管理 CLI ${cli_name}，将跳过。重置管理员口令等操作需另行处理"
            BIN_CLI=""
        fi
    else
        BIN_NODE="${out}/${main_name}"
    fi
    ok "下载完成"
    return 0
}

build_from_source() {
    log "从源码编译 (linux/${ARCH})"
    command -v go >/dev/null 2>&1 || die "未找到 go，无法从源码编译，请安装 Go 1.25 或更高版本"
    [[ -f "${REPO_ROOT}/go.mod" ]] || die "未在 ${REPO_ROOT} 找到 go.mod，请在仓库目录内执行本脚本"

    local out="${WORK_DIR}/bin"
    install -d -m 0755 "${out}"

    if [[ "${ROLE}" == "master" ]]; then
        if [[ ! -f "${REPO_ROOT}/web/dist/index.html" ]]; then
            command -v npm >/dev/null 2>&1 \
                || die "主控需要 web/dist 静态资源，但既无已构建产物也没有 npm。请先执行 (cd web && npm ci && npm run build)"
            log "构建前端资源"
            ( cd "${REPO_ROOT}/web" && npm ci --no-audit --no-fund && npm run build ) \
                || die "前端构建失败"
        else
            ok "复用已存在的 web/dist"
        fi
        log "编译 server 与 dnscat CLI"
        ( cd "${REPO_ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" \
            go build -trimpath -ldflags "-s -w" -o "${out}/dnscat-server" ./cmd/server ) \
            || die "编译 server 失败"
        ( cd "${REPO_ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" \
            go build -trimpath -ldflags "-s -w" -o "${out}/dnscat" ./cmd/cli ) \
            || die "编译 CLI 失败"
        BIN_SERVER="${out}/dnscat-server"
        BIN_CLI="${out}/dnscat"
    else
        log "编译 node"
        ( cd "${REPO_ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" \
            go build -trimpath -ldflags "-s -w" -o "${out}/dnscat-node" ./cmd/node ) \
            || die "编译 node 失败"
        BIN_NODE="${out}/dnscat-node"
    fi
    ok "编译完成"
}

acquire_binaries() {
    if [[ -n "${BINARY_DIR}" ]]; then
        use_local_binaries "${BINARY_DIR}" \
            || die "在 --binary-dir ${BINARY_DIR} 中未找到 ${ROLE} 所需的二进制"
        return 0
    fi
    if [[ "${FROM_SOURCE}" == "yes" ]]; then
        build_from_source
        return 0
    fi
    if use_local_binaries "${REPO_ROOT}/bin"; then
        return 0
    fi
    if download_release; then
        return 0
    fi
    warn "未配置预编译产物地址 DNSCAT_REPO 或 DNSCAT_RELEASE_BASE，改为从源码编译"
    build_from_source
}

# ---------------------------- config and systemd ----------------------------

ensure_service_user() {
    if id -u "${SERVICE_USER}" >/dev/null 2>&1; then
        return 0
    fi
    log "创建系统用户 ${SERVICE_USER}"
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}" \
            || useradd --system --no-create-home "${SERVICE_USER}"
    elif command -v adduser >/dev/null 2>&1; then
        adduser --system --no-create-home --disabled-login "${SERVICE_USER}" 2>/dev/null \
            || adduser -S -H -D "${SERVICE_USER}"
    else
        die "既无 useradd 也无 adduser，无法创建服务账号"
    fi
    ok "已创建 ${SERVICE_USER}"
}

write_master_config() {
    local cfg="${CONFIG_DIR}/config.yaml"
    if [[ -f "${cfg}" ]]; then
        ok "保留已有配置 ${cfg}，如需重建请先删除该文件"
        local existing
        existing="$(sed -nE 's/^[[:space:]]*secret_token:[[:space:]]*"?([^"#]*)"?.*/\1/p' "${cfg}" \
                    | head -1 | tr -d '[:space:]')"
        if [[ -n "${existing}" ]]; then
            CLUSTER_TOKEN="${existing}"
        fi
        return 0
    fi

    if [[ -z "${CLUSTER_TOKEN}" ]]; then
        CLUSTER_TOKEN="$(rand_hex 32)"
    fi
    local jwt
    jwt="$(rand_hex 32)"

    local driver dsn
    if [[ -n "${DB_DSN}" ]]; then
        dsn="${DB_DSN}"
        if [[ "${dsn}" == *"@tcp("* ]]; then
            driver="mysql"
        else
            driver="sqlite"
        fi
    elif [[ "${DB_DRIVER}" == "mysql" ]]; then
        die "--db mysql 需要同时用 --db-dsn 指定连接串"
    else
        driver="sqlite"
        dsn="${DATA_DIR}/dnscat.db"
    fi

    install -d -m 0750 "${CONFIG_DIR}"
    cat > "${cfg}" <<CONFIG_EOF
# 由 DnsCat install.sh 于 $(date '+%Y-%m-%d %H:%M:%S %z') 生成。
# 下列密钥为本机随机生成，请勿外传。被控节点安装时需要用到 cluster.secret_token。

server:
  http_port: ${HTTP_PORT}
  host: "0.0.0.0"
  mode: "release"

dns:
  udp_port: ${DNS_PORT}
  tcp_port: ${DNS_PORT}
  tls_port: 853
  enable_tls: false
  tls_cert_file: ""
  tls_key_file: ""
  enable_doh: true
  # 留空：请在控制台「权威 NS 服务器」页添加本部署实际的 NS 主机名
  default_ns: []
  default_ttl: 300
  rate_limit: 1000

database:
  driver: "${driver}"
  dsn: "${dsn}"

redis:
  enabled: false
  addr: "127.0.0.1:6379"
  password: ""
  db: 0

dnssec:
  auto_sign: true
  algorithm: "ECDSAP256SHA256"

cluster:
  node_id: "master-node-01"
  node_name: "Master Primary"
  is_master: true
  secret_token: "${CLUSTER_TOKEN}"
  sync_interval: 10

prober:
  enabled: true
  check_interval: 15
  timeout_sec: 5

jwt_secret: "${jwt}"
CONFIG_EOF
    chmod 0640 "${cfg}"
    chown "root:${SERVICE_USER}" "${cfg}" 2>/dev/null || true
    ok "已生成 ${cfg}，含随机 jwt_secret 与 cluster.secret_token"
}

write_master_unit() {
    cat > /etc/systemd/system/dnscat-server.service <<UNIT_EOF
[Unit]
Description=DnsCat Authoritative DNS Server (master)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${DATA_DIR}
ExecStart=${PREFIX}/dnscat-server --config ${CONFIG_DIR}/config.yaml
Restart=always
RestartSec=3
LimitNOFILE=65535

# 以非 root 运行仍需绑定 53 特权端口，并为源站 ICMP 探测保留 raw socket 能力
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_RAW
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
UNIT_EOF
    ok "已写入 /etc/systemd/system/dnscat-server.service"
}

write_node_unit() {
    # 令牌写在 unit 里会被 systemctl cat 与 ps 看到，改放进 0640 的 EnvironmentFile
    install -d -m 0750 "${CONFIG_DIR}"
    cat > "${CONFIG_DIR}/node.env" <<NODEENV_EOF
# 由 DnsCat install.sh 生成。集群令牌须与主控 cluster.secret_token 完全一致。
DNSCAT_MASTER_URL=${MASTER_URL}
DNSCAT_NODE_ID=${NODE_ID}
DNSCAT_CLUSTER_TOKEN=${CLUSTER_TOKEN}
DNSCAT_PUBLIC_IP=${PUBLIC_IP}
DNSCAT_DNS_PORT=${DNS_PORT}
NODEENV_EOF
    chmod 0640 "${CONFIG_DIR}/node.env"
    chown "root:${SERVICE_USER}" "${CONFIG_DIR}/node.env" 2>/dev/null || true
    ok "已生成 ${CONFIG_DIR}/node.env，权限 0640"

    # 未提供 public-ip 时不能传空值，否则会被当成参数缺失
    local pubarg=""
    if [[ -n "${PUBLIC_IP}" ]]; then
        pubarg=' --public-ip ${DNSCAT_PUBLIC_IP}'
    fi

    cat > /etc/systemd/system/dnscat-node.service <<UNIT_EOF
[Unit]
Description=DnsCat Edge Node
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${DATA_DIR}
EnvironmentFile=${CONFIG_DIR}/node.env
# 令牌不放命令行：systemd 会把 EnvironmentFile 的变量展开进 ExecStart，
# 那样 ps 输出里同机任何用户都能看到令牌。node 会直接读 DNSCAT_CLUSTER_TOKEN 环境变量。
ExecStart=${PREFIX}/dnscat-node --master \${DNSCAT_MASTER_URL} --node-id \${DNSCAT_NODE_ID} --udp \${DNSCAT_DNS_PORT} --tcp \${DNSCAT_DNS_PORT}${pubarg}
Restart=always
RestartSec=3
LimitNOFILE=65535

AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_NET_RAW
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
UNIT_EOF
    ok "已写入 /etc/systemd/system/dnscat-node.service"
}

install_binaries() {
    install -d -m 0755 "${PREFIX}"
    if [[ "${ROLE}" == "master" ]]; then
        install -m 0755 "${BIN_SERVER}" "${PREFIX}/dnscat-server"
        ok "已安装 ${PREFIX}/dnscat-server"
        if [[ -n "${BIN_CLI}" && -f "${BIN_CLI}" ]]; then
            install -m 0755 "${BIN_CLI}" "${PREFIX}/dnscat"
            ok "已安装 ${PREFIX}/dnscat 管理 CLI"
        else
            warn "未获取到 dnscat CLI，已跳过。重置管理员口令等操作将不可用"
        fi
    else
        install -m 0755 "${BIN_NODE}" "${PREFIX}/dnscat-node"
        ok "已安装 ${PREFIX}/dnscat-node"
    fi
}

# print_initial_credentials 从服务日志提取首次安装生成的随机管理员口令。
# 只有首次安装才有这段输出；库中已有管理员时不会再打印。
print_initial_credentials() {
    local pw="" user="" i logs=""
    for i in $(seq 1 30); do
        if command -v journalctl >/dev/null 2>&1; then
            logs="$(journalctl -u dnscat-server --no-pager -n 200 2>/dev/null || true)"
            pw="$(printf '%s\n' "${logs}" \
                  | sed -nE 's/.*初始随机口令[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
            user="$(printf '%s\n' "${logs}" \
                  | sed -nE 's/.*管理员账号[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
        fi
        [[ -n "${pw}" ]] && break
        sleep 1
    done

    hr
    if [[ -n "${pw}" ]]; then
        printf '%s首次安装已生成随机管理员口令，请立即保存，此口令仅显示这一次%s\n' "${C_BLD}" "${C_RST}"
        printf '  管理员账号 : %s\n' "${user:-admin}"
        printf '  初始口令   : %s%s%s\n' "${C_BLD}" "${pw}" "${C_RST}"
    else
        printf '未从日志中提取到初始口令，可能原因:\n'
        printf '  · 本机此前已安装过，数据库中已有管理员（口令仅存 bcrypt 摘要，无法回显）\n'
        printf '  · 服务尚未完成启动\n'
        printf '查看完整日志: journalctl -u dnscat-server -n 100 --no-pager\n'
        printf '忘记口令时重置: sudo dnscat reset-admin\n'
    fi
    hr
}

# ---------------------------- summaries ----------------------------

host_ip() {
    local ip=""
    ip="$(hostname -I 2>/dev/null | awk '{print $1}')" || true
    if [[ -z "${ip}" ]]; then
        ip="<本机IP>"
    fi
    printf '%s' "${ip}"
}

summary_binary() {
    local ip
    ip="$(host_ip)"

    printf '\n'
    hr
    printf '%sDnsCat 安装完成（二进制 / systemd，%s）%s\n' "${C_BLD}" "${ROLE}" "${C_RST}"
    hr
    if [[ "${ROLE}" == "master" ]]; then
        print_initial_credentials
        printf '  Web 控制台 : http://%s:%s\n' "${ip}" "${HTTP_PORT}"
        printf '  权威 DNS   : %s:%s (UDP/TCP)\n' "${ip}" "${DNS_PORT}"
        printf '  配置文件   : %s/config.yaml\n' "${CONFIG_DIR}"
        printf '  数据目录   : %s\n' "${DATA_DIR}"
        printf '  集群令牌   : %s\n' "${CLUSTER_TOKEN}"
        printf '               被控节点安装时用 --cluster-token 传入这个值\n'
        printf '\n下一步:\n'
        printf '  1. 打开控制台，在「权威 NS 服务器」页添加 ns1/ns2.<你的域名> 与对应 IP\n'
        printf '  2. 在域名注册商处把 NS 指向上一步的主机名，并登记同样的 Glue 记录\n'
        printf '  3. 添加域名与解析记录\n'
        printf '\n提示: %s 是 HTTP 明文，公网长期使用请加 TLS 反代并用安全组限制来源 IP。\n' "${HTTP_PORT}"
    else
        printf '  节点标识   : %s\n' "${NODE_ID}"
        printf '  回连主控   : %s\n' "${MASTER_URL}"
        printf '  对外地址   : %s\n' "${PUBLIC_IP:-<未指定>}"
        printf '  权威 DNS   : %s:%s (UDP/TCP)\n' "${ip}" "${DNS_PORT}"
        printf '  凭据文件   : %s/node.env\n' "${CONFIG_DIR}"
        printf '\n在主控控制台的「集群节点」页应能看到本节点上线，心跳约 3 秒一次。\n'
    fi
    printf '\n常用命令:\n'
    printf '  systemctl status %s\n' "${SERVICE_NAME}"
    printf '  journalctl -u %s -f\n' "${SERVICE_NAME}"
    if [[ "${ROLE}" == "master" ]]; then
        printf '  dnscat                 交互式管理菜单\n'
        printf '  dnscat reset-admin     重置管理员口令\n'
    fi
    printf '  sudo ./deploy/install.sh --upgrade     升级到新版本\n'
    printf '  sudo ./deploy/install.sh --uninstall   卸载\n'
    hr
}

summary_docker() {
    local compose_bin="$1" compose_file="$2"
    local ip
    ip="$(host_ip)"

    printf '\n'
    hr
    printf '%sDnsCat 安装完成（Docker，%s）%s\n' "${C_BLD}" "${ROLE}" "${C_RST}"
    hr
    if [[ "${ROLE}" == "master" ]]; then
        printf '  Web 控制台 : http://%s:%s\n' "${ip}" "${HTTP_PORT}"
        printf '  权威 DNS   : %s:%s (UDP/TCP)\n' "${ip}" "${DNS_PORT}"
        printf '  密钥文件   : %s/.env  权限 0600，勿提交版本库\n' "${SCRIPT_DIR}"
        printf '  集群令牌   : %s\n' "${CLUSTER_TOKEN}"
        printf '               被控节点安装时用 --cluster-token 传入这个值\n'
    else
        printf '  节点标识   : %s\n' "${NODE_ID}"
        printf '  回连主控   : %s\n' "${MASTER_URL}"
    fi
    printf '\n常用命令，在 %s 目录下执行:\n' "${SCRIPT_DIR}"
    printf '  %s -f %s ps\n' "${compose_bin}" "${compose_file}"
    printf '  %s -f %s logs -f\n' "${compose_bin}" "${compose_file}"
    printf '  %s -f %s restart\n' "${compose_bin}" "${compose_file}"
    printf '  %s -f %s down\n' "${compose_bin}" "${compose_file}"
    hr
}

# ---------------------------- install flows ----------------------------

install_binary_mode() {
    require_root
    has_systemd || die "未检测到 systemd，二进制模式暂不支持当前 init 系统，可改用 --mode docker"

    if [[ "${ROLE}" == "node" ]]; then
        [[ -n "${MASTER_URL}" ]]    || die "role=node 必须指定 --master-url，例如 http://203.0.113.10:8080"
        [[ -n "${NODE_ID}" ]]       || die "role=node 必须指定 --node-id，集群内唯一"
        [[ -n "${CLUSTER_TOKEN}" ]] || die "role=node 必须指定 --cluster-token，须与主控 cluster.secret_token 一致"
        if [[ -z "${PUBLIC_IP}" ]]; then
            warn "未指定 --public-ip，若本机位于 NAT 之后，控制台会显示错误的节点地址"
        fi
    fi

    # 重装/升级场景：先停掉本机已有的 DnsCat 服务，否则它自己占着 53 与 8080，
    # 端口检查会把「上一次安装」当成陌生占用者而中止。
    if [[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]]; then
        if systemctl is-active --quiet "${SERVICE_NAME}"; then
            log "检测到已安装的 ${SERVICE_NAME} 正在运行，先停止以便替换"
            systemctl stop "${SERVICE_NAME}" || true
            ok "已停止 ${SERVICE_NAME}"
        fi
    fi

    free_port_53_binary

    if [[ "${ROLE}" == "master" ]] && port_is_busy "${HTTP_PORT}"; then
        describe_port "${HTTP_PORT}"
        die "Web 控制台端口 ${HTTP_PORT} 已被占用，请用 --http-port 换端口或先释放该端口"
    fi

    acquire_binaries
    ensure_service_user

    install -d -m 0750 "${DATA_DIR}"
    chown -R "${SERVICE_USER}:${SERVICE_USER}" "${DATA_DIR}"

    install_binaries

    if [[ "${ROLE}" == "master" ]]; then
        write_master_config
        write_master_unit
    else
        write_node_unit
    fi

    systemctl daemon-reload
    systemctl enable "${SERVICE_NAME}" >/dev/null 2>&1 || true
    systemctl restart "${SERVICE_NAME}"

    log "等待服务就绪"
    local i
    for i in $(seq 1 20); do
        if systemctl is-active --quiet "${SERVICE_NAME}"; then
            break
        fi
        sleep 1
    done
    if ! systemctl is-active --quiet "${SERVICE_NAME}"; then
        warn "${SERVICE_NAME} 未处于运行状态，最近日志:"
        journalctl -u "${SERVICE_NAME}" -n 40 --no-pager 2>/dev/null || true
        die "服务启动失败"
    fi
    ok "${SERVICE_NAME} 运行中"

    summary_binary
}

install_docker_mode() {
    require_root
    command -v docker >/dev/null 2>&1 || die "未找到 docker，请先安装 Docker 后重试"

    local compose_bin=""
    if docker compose version >/dev/null 2>&1; then
        compose_bin="docker compose"
    elif command -v docker-compose >/dev/null 2>&1; then
        compose_bin="docker-compose"
    else
        die "未找到 Docker Compose，请先安装 docker compose 插件或 docker-compose"
    fi

    local compose_file="docker-compose.master.yml"
    if [[ "${ROLE}" == "node" ]]; then
        compose_file="docker-compose.node.yml"
    fi
    [[ -f "${SCRIPT_DIR}/${compose_file}" ]] \
        || die "未找到 ${SCRIPT_DIR}/${compose_file}，请在仓库目录内执行本脚本"

    # 按要求：Docker 模式只检测端口占用并给出指引，不自动改动宿主机服务
    check_port_53_docker

    local env_file="${SCRIPT_DIR}/.env"
    if [[ -f "${env_file}" ]]; then
        ok "复用已有 ${env_file}"
        # 复用已有 .env 时把值读回脚本变量，否则安装总结里这些字段会是空白
        CLUSTER_TOKEN="$(sed -nE 's/^CLUSTER_TOKEN=(.*)$/\1/p' "${env_file}" | head -1)"
        if [[ "${ROLE}" == "node" ]]; then
            [[ -n "${MASTER_URL}" ]] || MASTER_URL="$(sed -nE 's/^MASTER_URL=(.*)$/\1/p' "${env_file}" | head -1)"
            [[ -n "${NODE_ID}" ]]    || NODE_ID="$(sed -nE 's/^NODE_ID=(.*)$/\1/p' "${env_file}" | head -1)"
            [[ -n "${PUBLIC_IP}" ]]  || PUBLIC_IP="$(sed -nE 's/^NODE_PUBLIC_IP=(.*)$/\1/p' "${env_file}" | head -1)"
        fi
    else
        if [[ "${ROLE}" == "master" ]]; then
            if [[ -z "${CLUSTER_TOKEN}" ]]; then
                CLUSTER_TOKEN="$(rand_hex 32)"
            fi
            cat > "${env_file}" <<ENV_EOF
# 由 DnsCat install.sh 生成，请勿提交到版本库。
MYSQL_ROOT_PASSWORD=$(rand_hex 24)
JWT_SECRET=$(rand_hex 32)
CLUSTER_TOKEN=${CLUSTER_TOKEN}
ENV_EOF
        else
            [[ -n "${MASTER_URL}" ]]    || die "role=node 必须指定 --master-url"
            [[ -n "${NODE_ID}" ]]       || die "role=node 必须指定 --node-id"
            [[ -n "${CLUSTER_TOKEN}" ]] || die "role=node 必须指定 --cluster-token，须与主控一致"
            [[ -n "${PUBLIC_IP}" ]]     || die "role=node 必须指定 --public-ip，节点对外公网 IP"
            cat > "${env_file}" <<ENV_EOF
# 由 DnsCat install.sh 生成，请勿提交到版本库。
MASTER_URL=${MASTER_URL}
NODE_ID=${NODE_ID}
CLUSTER_TOKEN=${CLUSTER_TOKEN}
NODE_PUBLIC_IP=${PUBLIC_IP}
ENV_EOF
        fi
        chmod 0600 "${env_file}"
        ok "已生成 ${env_file}，权限 0600"
    fi

    log "构建镜像并启动容器，首次构建需要几分钟"
    ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" up -d --build ) \
        || die "容器启动失败，请查看上方构建日志"

    ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" ps ) || true

    if [[ "${ROLE}" == "master" ]]; then
        log "等待主控就绪并提取初始管理员口令"
        local i pw="" user="" logs=""
        for i in $(seq 1 60); do
            logs="$( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" logs --no-color server 2>/dev/null || true )"
            pw="$(printf '%s\n' "${logs}" \
                  | sed -nE 's/.*初始随机口令[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
            user="$(printf '%s\n' "${logs}" \
                  | sed -nE 's/.*管理员账号[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
            [[ -n "${pw}" ]] && break
            sleep 2
        done
        hr
        if [[ -n "${pw}" ]]; then
            printf '%s首次安装已生成随机管理员口令，请立即保存，此口令仅显示这一次%s\n' "${C_BLD}" "${C_RST}"
            printf '  管理员账号 : %s\n' "${user:-admin}"
            printf '  初始口令   : %s%s%s\n' "${C_BLD}" "${pw}" "${C_RST}"
        else
            printf '未从容器日志提取到初始口令，可能此前已安装过，或服务仍在启动。\n'
            printf '查看日志: %s -f %s logs server\n' "${compose_bin}" "${compose_file}"
        fi
        hr
    fi

    summary_docker "${compose_bin}" "${compose_file}"
}

do_upgrade() {
    require_root
    has_systemd || die "--upgrade 仅支持 systemd 二进制安装方式"
    [[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]] \
        || die "未找到 ${SERVICE_NAME}.service，看起来尚未安装过，请先执行常规安装"

    log "升级 ${SERVICE_NAME}，保留配置与数据"
    acquire_binaries
    systemctl stop "${SERVICE_NAME}" || true
    install_binaries
    systemctl daemon-reload
    systemctl start "${SERVICE_NAME}"
    sleep 2
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        ok "${SERVICE_NAME} 已重启并运行中"
    else
        die "${SERVICE_NAME} 启动失败: journalctl -u ${SERVICE_NAME} -n 50 --no-pager"
    fi
    log "升级完成"
}

do_uninstall() {
    require_root
    log "卸载 DnsCat"

    local u
    for u in dnscat-server dnscat-node; do
        if [[ -f "/etc/systemd/system/${u}.service" ]]; then
            systemctl disable --now "${u}" 2>/dev/null || true
            rm -f "/etc/systemd/system/${u}.service"
            ok "已移除 ${u}.service"
        fi
    done
    systemctl daemon-reload 2>/dev/null || true

    rm -f "${PREFIX}/dnscat-server" "${PREFIX}/dnscat-node" "${PREFIX}/dnscat"
    ok "已移除二进制"

    if [[ -f /etc/systemd/resolved.conf.d/dnscat.conf ]]; then
        if confirm "  恢复 systemd-resolved 的 53 stub 监听?"; then
            rm -f /etc/systemd/resolved.conf.d/dnscat.conf
            systemctl restart systemd-resolved 2>/dev/null || true
            ok "已恢复 systemd-resolved stub 监听"
        fi
    fi

    if [[ "${DO_PURGE}" == "yes" ]]; then
        warn "--purge 将删除配置与数据，含 sqlite 数据库、DNSSEC 私钥与全部解析记录"
        if confirm "  确认删除 ${CONFIG_DIR} 与 ${DATA_DIR}? 此操作不可恢复"; then
            rm -rf "${CONFIG_DIR}" "${DATA_DIR}"
            ok "已删除配置与数据"
        fi
    else
        ok "已保留配置 ${CONFIG_DIR} 与数据 ${DATA_DIR}，需一并删除请加 --purge"
    fi
    log "卸载完成"
}

main() {
    detect_os
    ARCH="$(detect_arch)"

    WORK_DIR="$(mktemp -d)"
    trap 'rm -rf "${WORK_DIR}"' EXIT

    printf '\n'
    hr
    printf '%sDnsCat 安装脚本%s  架构 linux/%s  模式 %s  角色 %s\n' \
        "${C_BLD}" "${C_RST}" "${ARCH}" "${MODE}" "${ROLE}"
    hr

    if [[ "${DO_UNINSTALL}" == "yes" ]]; then
        do_uninstall
        return
    fi
    if [[ "${DO_UPGRADE}" == "yes" ]]; then
        do_upgrade
        return
    fi

    if [[ "${MODE}" == "docker" ]]; then
        install_docker_mode
    else
        install_binary_mode
    fi
}

main "$@"
