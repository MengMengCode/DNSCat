#!/usr/bin/env bash
#
# DNSCat installer / 安装脚本
# https://github.com/MengMengCode/DNSCat
#
# Supported: linux/amd64, linux/386, linux/arm64, linux/armv7
#
# Quick start (one command, nothing to clone) / 一键安装（无需克隆仓库）:
#
#   Interactive / 交互式（先选语言，再选安装方式与角色）:
#     curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash
#
#   Master / 主控:
#     curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --role master
#
#   Edge node / 边缘端（主控安装完成后会打印填好令牌的现成命令）:
#     curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --role node --master-url http://<MASTER_IP>:8080 --node-id edge-01 --cluster-token <TOKEN> --public-ip <EDGE_PUBLIC_IP>
#
# See --help for all options.
#

set -euo pipefail

# ---------------------------- defaults ----------------------------
UI_LANG=""            # zh | en，留空表示尚未选择
MODE=""               # binary | docker，留空表示进入交互选择
ROLE=""               # master | node
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
NON_INTERACTIVE="no"

# 发布产物所在仓库。可用 DNSCAT_RELEASE_BASE 覆盖为内网镜像站。
DNSCAT_REPO="${DNSCAT_REPO:-MengMengCode/DNSCat}"
DNSCAT_RELEASE_BASE="${DNSCAT_RELEASE_BASE:-}"

# 通过 curl | bash 执行时 BASH_SOURCE[0] 不是真实文件（可能是 /dev/fd/63 或 "bash"），
# 直接 cd 会在 set -e 下当场失败，所以这里允许两者为空，后续按需由
# ensure_repo() 下载源码并回填。
SCRIPT_DIR=""
REPO_ROOT=""
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
fi
# 标记是否为「独立运行」（手头没有仓库文件）
STANDALONE="yes"
if [[ -n "${REPO_ROOT}" && -f "${REPO_ROOT}/go.mod" ]]; then
    STANDALONE="no"
fi

ARCH=""
WORK_DIR=""
BIN_SERVER=""
BIN_NODE=""
BIN_CLI=""

# ---------------------------- colors ----------------------------
if [[ -t 1 ]]; then
    C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'
    C_CYA=$'\033[36m'; C_BLD=$'\033[1m'; C_RST=$'\033[0m'
else
    C_RED=""; C_GRN=""; C_YEL=""; C_CYA=""; C_BLD=""; C_RST=""
fi

# ---------------------------- i18n ----------------------------
# 消息以 printf 格式串存放，参数由 t() 透传，因此顺序敏感的占位符
# 在两种语言里必须保持一致的个数与含义。
declare -A M_ZH=(
    [need_root]="需要 root 权限，请用 sudo 执行"
    [only_linux]="本脚本仅支持 Linux（检测到 %s）"
    [arch_unsupported]="不支持的 CPU 架构 %s。支持 amd64、386、arm64、armv7"
    [no_systemd]="未检测到 systemd，二进制模式暂不支持当前 init 系统，可改用 Docker 模式"
    [banner]="DNSCat 安装脚本"
    [banner_info]="架构 linux/%s   模式 %s   角色 %s"

    [lang_prompt]="请选择语言 / Select language"
    [lang_zh]="中文"
    [lang_en]="English"
    [lang_choice]="请输入序号"
    [lang_invalid]="输入无效，请重新选择"
    [lang_set]="已选择：中文"

    [mode_prompt]="请选择安装方式"
    [mode_binary]="二进制安装（systemd 管理，不需要 Docker，推荐）"
    [mode_docker]="Docker 安装（需要已装好 Docker 与 Compose）"
    [role_prompt]="请选择本机角色"
    [role_master]="主控（Web 控制台 + API + 权威 DNS）"
    [role_node]="被控节点（仅权威 DNS，回连主控同步区域）"

    [node_need_master]="被控节点必须指定主控地址 --master-url，例如 http://203.0.113.10:8080"
    [node_need_id]="被控节点必须指定 --node-id，集群内唯一"
    [node_need_token]="被控节点必须指定 --cluster-token，须与主控的 cluster.secret_token 一致"
    [node_need_pubip]="被控节点必须指定 --public-ip，即本机对外公网 IP"
    [node_no_pubip_warn]="未指定 --public-ip：本机若位于 NAT 之后，控制台会显示错误的节点地址"
    [ask_master_url]="请输入主控地址（形如 http://203.0.113.10:8080）"
    [ask_node_id]="请输入节点标识（集群内唯一，例如 edge-fra-01）"
    [ask_cluster_token]="请输入集群令牌（与主控 cluster.secret_token 一致）"
    [ask_public_ip]="请输入本机对外公网 IP"
    [input_empty]="不能为空，请重新输入"

    [port_free]="端口 %s 空闲"
    [port_busy_list]="端口 %s 当前占用情况:"
    [port_released]="端口 %s 已释放"
    [port_still_busy]="已尝试释放但端口 %s 仍被占用，请手动处理后重试"
    [port_http_busy]="Web 控制台端口 %s 已被占用，请用 --http-port 换端口或先释放该端口"
    [stop_self]="检测到已安装的 %s 正在运行，先停止以便替换"
    [stopped]="已停止 %s"
    [port_owner_self]="端口被上一次安装的 %s 占用，重装前先停止它"

    [resolved_found]="占用者是 systemd-resolved，将关闭其 53 stub 监听（宿主机解析能力不受影响）"
    [resolved_confirm]="  写入 /etc/systemd/resolved.conf.d/dnscat.conf 并重启 systemd-resolved?"
    [resolved_cancel]="已取消。可用 --dns-port 指定其他端口，或手动关闭 DNSStubListener 后重试"
    [resolved_written]="已写入 /etc/systemd/resolved.conf.d/dnscat.conf"
    [resolved_resolvconf_static]="已把原先生效的上游 DNS 固化进 /etc/resolv.conf，原文件备份为 %s"
    [resolved_no_upstream]="/etc/resolv.conf 当前指向本机 stub (127.0.0.53)，但没能找到任何可用的上游 DNS 地址"
    [resolved_no_upstream_abort]="为避免关闭 stub 后宿主机无法解析域名，已中止。请先在 /etc/resolv.conf 或 systemd-resolved 中配置可用的上游 DNS，再重新执行安装"
    [resolved_restart_fail]="重启 systemd-resolved 失败，请检查 systemctl status systemd-resolved"
    [resolved_restarted]="systemd-resolved 已重启，stub 监听已关闭"
    [dns_ok]="宿主机域名解析正常"
    [dns_check_fail]="宿主机域名解析测试未通过，请检查 /etc/resolv.conf 是否指向可用上游"

    [svc_found]="占用者是 %s，需要停用它才能让 DNSCat 绑定 %s"
    [svc_warn]="停用后本机将不再由 %s 提供解析服务"
    [svc_confirm]="  执行 systemctl disable --now %s?"
    [svc_cancel]="已取消。请手动处理 %s 后重试，或用 --dns-port 换端口"
    [resolvconf_localhost]="/etc/resolv.conf 仍指向 127.0.0.1，而本机解析服务刚被停用，请改为可用的上游 DNS"

    [pihole_found]="检测到 Pi-hole (pihole-FTL) 占用 %s"
    [pihole_warn]="Pi-hole 是完整的 DNS 服务栈，自动停用可能连带影响其界面与拦截功能，脚本不做自动处置"
    [unknown_owner]="以下占用者无法自动安全处理: %s"
    [unknown_hint_title]="请任选其一后重跑本脚本:"
    [unknown_hint1]="  1) 停用该服务，并确认宿主机仍能正常解析域名"
    [unknown_hint2]="  2) 让该服务只监听内网地址，把 %s 让给 DNSCat"
    [unknown_hint3]="  3) 用 --dns-port 换端口。注意权威 DNS 对外必须是 53，换端口仅适用于前面另有转发或负载均衡的场景"
    [port_abort]="端口 %s 仍被占用，已中止安装"

    [docker_port_title]="端口 %s 已被占用。Docker 模式不会自动改动宿主机服务。"
    [docker_port_body]="容器映射 %s 端口会直接失败，请先自行解决占用后重跑本脚本。"
    [docker_port_ways]="常见处置方式:"
    [docker_port_w1]="  1) systemd-resolved（Ubuntu / Debian 最常见）"
    [docker_port_w1n]="     只关本地 stub 监听，systemd-resolved 继续运行，宿主机解析不受影响。"
    [docker_port_w2]="  2) dnsmasq / named / bind9 / unbound 等"
    [docker_port_w2n]="     停用后请确认 /etc/resolv.conf 指向可用的上游 DNS。"
    [docker_port_w3]="  3) 该端口必须留给现有服务时"
    [docker_port_w3a]="     改用二进制安装并换端口: sudo ./deploy/install.sh --mode binary --dns-port 5353"
    [docker_port_w3b]="     或自行修改 deploy/%s 的端口映射"
    [docker_port_verify]="确认端口已空闲: ss -lnptu | grep \":%s\""
    [docker_port_abort]="请先解决 %s 端口占用后重新执行安装"
    [docker_missing]="未找到 docker，请先安装 Docker 后重试"
    [compose_missing]="未找到 Docker Compose，请先安装 docker compose 插件或 docker-compose"
    [compose_file_missing]="未找到 %s，请在仓库目录内执行本脚本"
    [docker_building]="构建镜像并启动容器，首次构建需要几分钟"
    [docker_pulling]="拉取已发布镜像 (tag: %s) 并启动容器"
    [docker_pull_fail]="镜像拉取失败，退回本地构建（需要几分钟）"
    [docker_up_fail]="容器启动失败，请查看上方构建日志"
    [env_reuse]="复用已有 %s"
    [env_written]="已生成 %s，权限 0600"

    [dl_start]="下载预编译产物: %s (linux/%s)"
    [dl_fail]="下载 %s 失败"
    [dl_done]="下载完成"
    [dl_need_tool]="需要 curl 或 wget 才能下载预编译产物，也可用 --from-source 从源码编译"
    [dl_cli_fail]="未能下载管理 CLI %s，已跳过。重置管理员口令等操作需另行处理"
    [sum_fetch]="获取校验和清单: %s"
    [sum_no_tool]="系统没有 sha256sum 或 shasum，跳过校验和核对"
    [sum_missing_official]="无法从官方 Release 获取 %s。可能是网络问题或该版本尚未发布完成，已中止以免安装来源不明的文件"
    [sum_missing_mirror]="镜像站 %s 没有校验和清单，已跳过核对（自定义 DNSCAT_RELEASE_BASE）"
    [sum_no_entry]="校验和清单里没有 %s 的条目，已跳过该文件的核对"
    [sum_ok]="校验和核对通过: %s"
    [sum_mismatch]="校验和不匹配: %s\n  期望 %s\n  实际 %s\n下载物已损坏或被篡改，已中止安装"
    [sum_abort]="下载物完整性校验未通过，已中止。可重试安装，或用 --from-source 从源码编译"
    [use_local_bin]="使用已有二进制: %s (linux/%s)"
    [build_start]="从源码编译 (linux/%s)"
    [build_no_go]="未找到 go，无法从源码编译，请安装 Go 1.25 或更高版本"
    [build_no_mod]="未在 %s 找到 go.mod，请在仓库目录内执行本脚本"
    [build_web]="构建前端资源"
    [build_web_reuse]="复用已存在的 web/dist"
    [build_web_fail]="前端构建失败"
    [build_web_no_npm]="主控需要前端产物，但既无已构建的 web/dist 也没有 npm。请先执行 (cd web && npm ci && npm run build)"
    [build_go]="编译 server 与 dnscat CLI"
    [build_go_node]="编译 node"
    [build_fail]="编译 %s 失败"
    [build_done]="编译完成"
    [no_release_cfg]="未配置预编译产物地址，改为从源码编译"
    [repo_fetch]="本机没有仓库文件，正在获取源码快照: %s"
    [repo_fetch_fail]="下载源码快照失败: %s。请检查网络，或先 git clone 仓库后在其目录内执行本脚本"
    [repo_extract_fail]="源码快照解包失败或内容不完整"
    [repo_ready]="源码已就绪: %s"
    [bindir_missing]="在 --binary-dir %s 中未找到 %s 所需的二进制"

    [user_create]="创建系统用户 %s"
    [user_created]="已创建 %s"
    [user_no_tool]="既无 useradd 也无 adduser，无法创建服务账号"
    [cfg_keep]="保留已有配置 %s，如需重建请先删除该文件"
    [cfg_written]="已生成 %s，含随机 jwt_secret 与 cluster.secret_token"
    [cfg_unreadable]="服务账号 %s 读不到 %s，将放宽权限（否则服务会静默回退到默认配置）"
    [cfg_perm_relaxed]="已把 %s 及其目录调整为服务账号可读"
    [cfg_mysql_need_dsn]="--db mysql 需要同时用 --db-dsn 指定连接串"
    [unit_written]="已写入 %s"
    [nodeenv_written]="已生成 %s，权限 0640"
    [bin_installed]="已安装 %s"
    [cli_installed]="已安装 %s（管理 CLI）"
    [cli_skipped]="未获取到 dnscat CLI，已跳过。重置管理员口令等操作将不可用"
    [waiting_svc]="等待服务就绪"
    [svc_running]="%s 运行中"
    [svc_start_fail]="服务启动失败"
    [svc_recent_log]="%s 未处于运行状态，最近日志:"

    [cred_title]="首次安装已生成随机管理员口令，请立即保存，此口令仅显示这一次"
    [cred_user]="  管理员账号 : %s"
    [cred_pass]="  初始口令   : %s"
    [cred_kept]="数据库中已有管理员，沿用原有账号与口令（口令仅存 bcrypt 摘要，无法回显）"
    [cred_none]="未从日志中提取到初始口令，可能原因:"
    [cred_none1]="  · 本机此前已安装过，数据库中已有管理员（口令仅存 bcrypt 摘要，无法回显）"
    [cred_none2]="  · 服务尚未完成启动"
    [cred_none_log]="查看完整日志: journalctl -u dnscat-server -n 100 --no-pager"
    [cred_none_reset]="忘记口令时重置: sudo dnscat reset-admin"

    [done_binary]="DNSCat 安装完成（二进制 / systemd，%s）"
    [done_docker]="DNSCat 安装完成（Docker，%s）"
    [sum_web]="  Web 控制台 : http://%s:%s"
    [sum_dns]="  权威 DNS   : %s:%s (UDP/TCP)"
    [sum_cfg]="  配置文件   : %s"
    [sum_data]="  数据目录   : %s"
    [sum_token]="  集群令牌   : %s"
    [sum_token_note]="               被控节点安装时用 --cluster-token 传入这个值"
    [sum_envfile]="  密钥文件   : %s（权限 0600，请勿提交版本库）"
    [sum_nodeid]="  节点标识   : %s"
    [sum_master]="  回连主控   : %s"
    [sum_pubip]="  对外地址   : %s"
    [sum_credfile]="  凭据文件   : %s"
    [edge_cmd_title]="在边缘机器上执行下面这一条命令即可加入本集群（令牌与主控地址已填好）:"
    [edge_cmd_note]="  把 --node-id 换成该机器的唯一标识，--public-ip 换成它的对外公网 IP。"
    [next_title]="下一步:"
    [next1]="  1. 打开控制台，在「权威 NS 服务器」页添加 ns1/ns2.<你的域名> 与对应 IP"
    [next2]="  2. 在域名注册商处把 NS 指向上一步的主机名，并登记同样的 Glue 记录"
    [next3]="  3. 添加域名与解析记录"
    [tls_hint]="提示: %s 是 HTTP 明文，公网长期使用请加 TLS 反向代理并用安全组限制来源 IP"
    [node_online_hint]="在主控控制台的「集群节点」页应能看到本节点上线，心跳约 3 秒一次"
    [cmds]="常用命令:"
    [cmds_docker]="常用命令（在 %s 目录下执行）:"
    [cmd_cli]="  dnscat                 交互式管理菜单"
    [cmd_reset]="  dnscat reset-admin     重置管理员口令"
    [cmd_upgrade]="  升级: curl -fsSL %s | sudo bash -s -- --yes --upgrade --role %s"
    [cmd_uninstall]="  卸载: curl -fsSL %s | sudo bash -s -- --yes --uninstall"

    [upgrade_only_systemd]="--upgrade 仅支持 systemd 二进制安装方式"
    [upgrade_not_installed]="未找到 %s，看起来尚未安装过，请先执行常规安装"
    [upgrade_start]="升级 %s，保留配置与数据"
    [upgrade_restarted]="%s 已重启并运行中"
    [upgrade_fail]="%s 启动失败: journalctl -u %s -n 50 --no-pager"
    [upgrade_done]="升级完成"

    [uninstall_start]="卸载 DNSCat"
    [uninstall_unit]="已移除 %s"
    [uninstall_bin]="已移除二进制"
    [uninstall_resolved]="  恢复 systemd-resolved 的 53 stub 监听?"
    [uninstall_resolved_ok]="已恢复 systemd-resolved stub 监听"
    [purge_warn]="--purge 将删除配置与数据，含数据库、DNSSEC 私钥与全部解析记录"
    [purge_confirm]="  确认删除 %s 与 %s? 此操作不可恢复"
    [purge_done]="已删除配置与数据"
    [purge_kept]="已保留配置 %s 与数据 %s，需一并删除请加 --purge"
    [uninstall_done]="卸载完成"

    [unknown_arg]="未知参数: %s（--help 查看用法）"
    [bad_mode]="--mode 只能是 binary 或 docker"
    [bad_role]="--role 只能是 master 或 node"
    [bad_lang]="--lang 只能是 zh 或 en"
)

declare -A M_EN=(
    [need_root]="Root privileges required, please run with sudo"
    [only_linux]="This script supports Linux only (detected %s)"
    [arch_unsupported]="Unsupported CPU architecture %s. Supported: amd64, 386, arm64, armv7"
    [no_systemd]="systemd not detected; binary mode does not support this init system. Use Docker mode instead"
    [banner]="DNSCat Installer"
    [banner_info]="arch linux/%s   mode %s   role %s"

    [lang_prompt]="Select language / 请选择语言"
    [lang_zh]="Chinese (中文)"
    [lang_en]="English"
    [lang_choice]="Enter choice"
    [lang_invalid]="Invalid input, please choose again"
    [lang_set]="Language set to English"

    [mode_prompt]="Select installation method"
    [mode_binary]="Binary install (managed by systemd, no Docker needed, recommended)"
    [mode_docker]="Docker install (requires Docker and Compose)"
    [role_prompt]="Select the role of this machine"
    [role_master]="Master (web console + API + authoritative DNS)"
    [role_node]="Edge node (authoritative DNS only, syncs zones from master)"

    [node_need_master]="An edge node requires --master-url, e.g. http://203.0.113.10:8080"
    [node_need_id]="An edge node requires --node-id, unique within the cluster"
    [node_need_token]="An edge node requires --cluster-token matching the master's cluster.secret_token"
    [node_need_pubip]="An edge node requires --public-ip, the public IP of this machine"
    [node_no_pubip_warn]="--public-ip not set: if this host is behind NAT the console will show the wrong node address"
    [ask_master_url]="Enter the master URL (e.g. http://203.0.113.10:8080)"
    [ask_node_id]="Enter the node ID (unique in cluster, e.g. edge-fra-01)"
    [ask_cluster_token]="Enter the cluster token (must match master's cluster.secret_token)"
    [ask_public_ip]="Enter the public IP of this machine"
    [input_empty]="Value cannot be empty, please try again"

    [port_free]="Port %s is free"
    [port_busy_list]="Current listeners on port %s:"
    [port_released]="Port %s released"
    [port_still_busy]="Port %s is still in use after the attempt; please resolve it manually"
    [port_http_busy]="Web console port %s is in use; use --http-port or free that port first"
    [stop_self]="An existing %s is running; stopping it before replacing"
    [stopped]="Stopped %s"
    [port_owner_self]="Port is held by %s from a previous install; stopping it before reinstall"

    [resolved_found]="Owner is systemd-resolved; its port 53 stub listener will be disabled (host name resolution stays intact)"
    [resolved_confirm]="  Write /etc/systemd/resolved.conf.d/dnscat.conf and restart systemd-resolved?"
    [resolved_cancel]="Cancelled. Use --dns-port for another port, or disable DNSStubListener manually and retry"
    [resolved_written]="Wrote /etc/systemd/resolved.conf.d/dnscat.conf"
    [resolved_resolvconf_static]="Pinned the previously active upstream DNS servers into /etc/resolv.conf, original backed up as %s"
    [resolved_no_upstream]="/etc/resolv.conf currently points at the local stub (127.0.0.53) but no usable upstream DNS server could be determined"
    [resolved_no_upstream_abort]="Aborting so the host is not left unable to resolve names. Configure a working upstream DNS in /etc/resolv.conf or systemd-resolved, then run the installer again"
    [resolved_restart_fail]="Failed to restart systemd-resolved, check systemctl status systemd-resolved"
    [resolved_restarted]="systemd-resolved restarted, stub listener disabled"
    [dns_ok]="Host name resolution works"
    [dns_check_fail]="Host name resolution test failed, check that /etc/resolv.conf points to a working upstream"

    [svc_found]="Owner is %s; it must be stopped for DNSCat to bind %s"
    [svc_warn]="After stopping, %s will no longer serve DNS on this host"
    [svc_confirm]="  Run systemctl disable --now %s?"
    [svc_cancel]="Cancelled. Handle %s manually and retry, or use --dns-port"
    [resolvconf_localhost]="/etc/resolv.conf still points to 127.0.0.1 but the local resolver was just stopped; set a working upstream DNS"

    [pihole_found]="Pi-hole (pihole-FTL) is holding port %s"
    [pihole_warn]="Pi-hole is a full DNS stack; stopping it automatically could break its UI and blocking, so this script will not touch it"
    [unknown_owner]="The following listeners cannot be handled safely: %s"
    [unknown_hint_title]="Pick one of these, then re-run this script:"
    [unknown_hint1]="  1) Stop that service and confirm the host can still resolve names"
    [unknown_hint2]="  2) Bind that service to an internal address only, leaving %s to DNSCat"
    [unknown_hint3]="  3) Use --dns-port. Note authoritative DNS must be on 53 externally; another port only works behind a forwarder or load balancer"
    [port_abort]="Port %s is still in use, installation aborted"

    [docker_port_title]="Port %s is in use. Docker mode will not modify host services."
    [docker_port_body]="Mapping port %s into a container would fail, please resolve the conflict and re-run."
    [docker_port_ways]="Common fixes:"
    [docker_port_w1]="  1) systemd-resolved (most common on Ubuntu / Debian)"
    [docker_port_w1n]="     This only disables the local stub listener; systemd-resolved keeps running and host resolution is unaffected."
    [docker_port_w2]="  2) dnsmasq / named / bind9 / unbound and friends"
    [docker_port_w2n]="     After stopping, make sure /etc/resolv.conf points at a working upstream DNS."
    [docker_port_w3]="  3) If the port must stay with the existing service"
    [docker_port_w3a]="     Use the binary install on another port: sudo ./deploy/install.sh --mode binary --dns-port 5353"
    [docker_port_w3b]="     Or edit the port mapping in deploy/%s"
    [docker_port_verify]="Verify the port is free: ss -lnptu | grep \":%s\""
    [docker_port_abort]="Resolve the conflict on port %s and run the installer again"
    [docker_missing]="docker not found, please install Docker first"
    [compose_missing]="Docker Compose not found, install the docker compose plugin or docker-compose"
    [compose_file_missing]="%s not found, run this script from inside the repository"
    [docker_building]="Building images and starting containers; the first build takes a few minutes"
    [docker_pulling]="Pulling published images (tag: %s) and starting containers"
    [docker_pull_fail]="Image pull failed, falling back to a local build (takes a few minutes)"
    [docker_up_fail]="Containers failed to start, check the build log above"
    [env_reuse]="Reusing existing %s"
    [env_written]="Wrote %s with mode 0600"

    [dl_start]="Downloading prebuilt binaries: %s (linux/%s)"
    [dl_fail]="Failed to download %s"
    [dl_done]="Download complete"
    [dl_need_tool]="curl or wget is required to download prebuilt binaries; or use --from-source"
    [dl_cli_fail]="Could not download the management CLI %s, skipping. Admin password reset will need another route"
    [sum_fetch]="Fetching checksum manifest: %s"
    [sum_no_tool]="Neither sha256sum nor shasum is available, skipping checksum verification"
    [sum_missing_official]="Could not fetch %s from the official release. Either the network failed or the release is still being published; aborting rather than installing unverified files"
    [sum_missing_mirror]="Mirror %s has no checksum manifest, skipping verification (custom DNSCAT_RELEASE_BASE)"
    [sum_no_entry]="No entry for %s in the checksum manifest, skipping verification for that file"
    [sum_ok]="Checksum verified: %s"
    [sum_mismatch]="Checksum mismatch for %s\n  expected %s\n  actual   %s\nThe download is corrupt or tampered with; aborting"
    [sum_abort]="Integrity check failed, aborting. Retry the install, or use --from-source to build from source"
    [use_local_bin]="Using existing binaries in %s (linux/%s)"
    [build_start]="Building from source (linux/%s)"
    [build_no_go]="go not found, cannot build from source. Install Go 1.25 or newer"
    [build_no_mod]="go.mod not found in %s, run this script from inside the repository"
    [build_web]="Building frontend assets"
    [build_web_reuse]="Reusing existing web/dist"
    [build_web_fail]="Frontend build failed"
    [build_web_no_npm]="The master needs frontend assets, but neither a built web/dist nor npm is available. Run (cd web && npm ci && npm run build) first"
    [build_go]="Building server and dnscat CLI"
    [build_go_node]="Building node"
    [build_fail]="Failed to build %s"
    [build_done]="Build complete"
    [no_release_cfg]="No release location configured, falling back to building from source"
    [repo_fetch]="Repository files are not present locally, fetching a source snapshot: %s"
    [repo_fetch_fail]="Failed to download the source snapshot: %s. Check connectivity, or git clone the repository and run this script from inside it"
    [repo_extract_fail]="Failed to extract the source snapshot, or it is incomplete"
    [repo_ready]="Source ready at %s"
    [bindir_missing]="No %s binary found in --binary-dir %s"

    [user_create]="Creating system user %s"
    [user_created]="Created %s"
    [user_no_tool]="Neither useradd nor adduser is available, cannot create the service account"
    [cfg_keep]="Keeping existing config %s; delete it first if you want it regenerated"
    [cfg_written]="Wrote %s with a random jwt_secret and cluster.secret_token"
    [cfg_unreadable]="Service account %s cannot read %s; relaxing permissions (otherwise the service silently falls back to built-in defaults)"
    [cfg_perm_relaxed]="Adjusted %s and its directory to be readable by the service account"
    [cfg_mysql_need_dsn]="--db mysql also requires --db-dsn with a connection string"
    [unit_written]="Wrote %s"
    [nodeenv_written]="Wrote %s with mode 0640"
    [bin_installed]="Installed %s"
    [cli_installed]="Installed %s (management CLI)"
    [cli_skipped]="dnscat CLI unavailable, skipped. Admin password reset will not work"
    [waiting_svc]="Waiting for the service to become ready"
    [svc_running]="%s is running"
    [svc_start_fail]="Service failed to start"
    [svc_recent_log]="%s is not running, recent log:"

    [cred_title]="A random administrator password was generated for this first install. Save it now, it is shown only once"
    [cred_user]="  Username : %s"
    [cred_pass]="  Password : %s"
    [cred_none]="Could not extract the initial password from the log. Possible reasons:"
    [cred_kept]="The database already has an administrator; the existing account and password are kept (only a bcrypt digest is stored)"
    [cred_none1]="  - This host was installed before and the database already has an administrator (only a bcrypt digest is stored)"
    [cred_none2]="  - The service has not finished starting yet"
    [cred_none_log]="Full log: journalctl -u dnscat-server -n 100 --no-pager"
    [cred_none_reset]="Reset a forgotten password: sudo dnscat reset-admin"

    [done_binary]="DNSCat installed (binary / systemd, %s)"
    [done_docker]="DNSCat installed (Docker, %s)"
    [sum_web]="  Web console : http://%s:%s"
    [sum_dns]="  Authoritative DNS : %s:%s (UDP/TCP)"
    [sum_cfg]="  Config file : %s"
    [sum_data]="  Data dir    : %s"
    [sum_token]="  Cluster token : %s"
    [sum_token_note]="               Pass this value via --cluster-token when installing edge nodes"
    [sum_envfile]="  Secrets file : %s (mode 0600, do not commit)"
    [sum_nodeid]="  Node ID     : %s"
    [sum_master]="  Master URL  : %s"
    [sum_pubip]="  Public IP   : %s"
    [sum_credfile]="  Credentials : %s"
    [edge_cmd_title]="Run this single command on an edge machine to join this cluster (token and master URL are prefilled):"
    [edge_cmd_note]="  Replace --node-id with a unique name for that machine and --public-ip with its public IP."
    [next_title]="Next steps:"
    [next1]="  1. Open the console and add ns1/ns2.<your-domain> with their IPs on the Nameservers page"
    [next2]="  2. At your registrar, point the domain NS at those hostnames and register matching glue records"
    [next3]="  3. Add your domains and DNS records"
    [tls_hint]="Note: port %s serves plain HTTP. For long-term public use put it behind a TLS reverse proxy and restrict source IPs"
    [node_online_hint]="This node should appear on the master console Cluster Nodes page; heartbeat runs every ~3 seconds"
    [cmds]="Useful commands:"
    [cmds_docker]="Useful commands (run inside %s):"
    [cmd_cli]="  dnscat                 interactive management menu"
    [cmd_reset]="  dnscat reset-admin     reset the administrator password"
    [cmd_upgrade]="  Upgrade:   curl -fsSL %s | sudo bash -s -- --yes --upgrade --role %s"
    [cmd_uninstall]="  Uninstall: curl -fsSL %s | sudo bash -s -- --yes --uninstall"

    [upgrade_only_systemd]="--upgrade only supports the systemd binary installation"
    [upgrade_not_installed]="%s not found; it does not look installed. Run a normal install first"
    [upgrade_start]="Upgrading %s, keeping config and data"
    [upgrade_restarted]="%s restarted and running"
    [upgrade_fail]="%s failed to start: journalctl -u %s -n 50 --no-pager"
    [upgrade_done]="Upgrade complete"

    [uninstall_start]="Uninstalling DNSCat"
    [uninstall_unit]="Removed %s"
    [uninstall_bin]="Removed binaries"
    [uninstall_resolved]="  Restore the systemd-resolved port 53 stub listener?"
    [uninstall_resolved_ok]="Restored the systemd-resolved stub listener"
    [purge_warn]="--purge will delete config and data, including the database, DNSSEC private keys and all DNS records"
    [purge_confirm]="  Really delete %s and %s? This cannot be undone"
    [purge_done]="Deleted config and data"
    [purge_kept]="Kept config %s and data %s; add --purge to remove them too"
    [uninstall_done]="Uninstall complete"

    [unknown_arg]="Unknown argument: %s (see --help)"
    [bad_mode]="--mode must be binary or docker"
    [bad_role]="--role must be master or node"
    [bad_lang]="--lang must be zh or en"
)

# t 取出消息并按 printf 语义填参。未知 key 原样返回，便于暴露漏翻的键。
t() {
    local key="$1"
    shift
    local fmt
    if [[ "${UI_LANG}" == "en" ]]; then
        fmt="${M_EN[$key]-}"
        [[ -z "${fmt}" ]] && fmt="${M_ZH[$key]-$key}"
    else
        fmt="${M_ZH[$key]-$key}"
    fi
    # shellcheck disable=SC2059
    printf "${fmt}" "$@"
}

log()  { printf '%s==>%s %s\n' "${C_CYA}" "${C_RST}" "$(t "$@")"; }
ok()   { printf '%s  ok%s %s\n' "${C_GRN}" "${C_RST}" "$(t "$@")"; }
warn() { printf '%s  !!%s %s\n' "${C_YEL}" "${C_RST}" "$(t "$@")" >&2; }
die()  { printf '%s[ERROR]%s %s\n' "${C_RED}" "${C_RST}" "$(t "$@")" >&2; exit 1; }
plain() { printf '%s\n' "$(t "$@")"; }
hr()   { printf -- '--------------------------------------------------------------------------------\n'; }

usage() {
    cat <<'USAGE_EOF'
DNSCat installer / 安装脚本
https://github.com/MengMengCode/DNSCat

Usage / 用法:
  sudo bash deploy/install.sh [options]

  Interactive (asks for language, mode and role) / 交互式（询问语言、方式与角色）:
      curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash

  Master / 主控:
      curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --role master

  Edge node / 被控节点:
      curl -fsSL https://raw.githubusercontent.com/MengMengCode/DNSCat/master/deploy/install.sh | sudo bash -s -- --yes --role node --master-url http://203.0.113.10:8080 --node-id edge-fra-01 --cluster-token <TOKEN> --public-ip 203.0.113.20

  From a local checkout / 已克隆仓库时:
      sudo bash deploy/install.sh

Options / 选项:
  --lang zh|en              界面语言 / UI language (default: ask, or zh)
  --mode binary|docker      安装方式 / install method (default: ask, or binary)
  --role master|node        角色 / role (default: ask, or master)
  --prefix DIR              二进制目录 / binary dir (default /usr/local/bin)
  --config-dir DIR          配置目录 / config dir (default /etc/dnscat)
  --data-dir DIR            数据目录 / data dir (default /var/lib/dnscat)
  --http-port PORT          控制台端口 / console port (default 8080)
  --dns-port PORT           DNS 端口 / DNS port (default 53)
  --db sqlite|mysql         主控数据库 / master database (default sqlite)
  --db-dsn DSN              自定义连接串 / custom DSN
  --master-url URL          主控地址 / master URL (node only)
  --node-id ID              节点标识 / node id (node only)
  --cluster-token TOKEN     集群令牌 / cluster token
  --public-ip IP            对外公网 IP / public IP (node only)
  --binary-dir DIR          使用已有二进制 / use existing binaries
  --from-source             从源码编译 / build from source
  --version VER             指定版本 / release version (default latest)
  -y, --yes                 非交互，自动确认 / non-interactive
  --upgrade                 升级 / upgrade in place
  --uninstall               卸载 / uninstall
  --purge                   配合 --uninstall 删除数据 / also delete data
  -h, --help                本帮助 / this help

Supported architectures / 支持架构:
  linux/amd64  linux/386  linux/arm64  linux/armv7

Port 53 / 53 端口:
  Binary mode frees port 53 automatically. For systemd-resolved it only disables
  the local stub listener, so host name resolution keeps working.
  二进制模式会自动让开 53；systemd-resolved 只关闭本地 stub 监听，宿主机解析不受影响。
  Docker mode never touches host services: it reports the conflict and exits.
  Docker 模式不改动宿主服务，检测到占用即中止并给出处置步骤。

Environment / 环境变量:
  DNSCAT_REPO           release repo, owner/repo (default MengMengCode/DNSCat)
  DNSCAT_RELEASE_BASE   full download prefix, overrides DNSCAT_REPO
  DNSCAT_VERSION        same as --version
USAGE_EOF
}

# ---------------------------- arg parsing ----------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --lang)          UI_LANG="${2:-}"; shift 2 ;;
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
        -y|--yes)        ASSUME_YES="yes"; NON_INTERACTIVE="yes"; shift ;;
        --upgrade)       DO_UPGRADE="yes"; shift ;;
        --uninstall)     DO_UNINSTALL="yes"; shift ;;
        --purge)         DO_PURGE="yes"; shift ;;
        -h|--help)       usage; exit 0 ;;
        *)               printf 'Unknown argument: %s (see --help)\n' "$1" >&2; exit 1 ;;
    esac
done

if [[ -n "${UI_LANG}" && "${UI_LANG}" != "zh" && "${UI_LANG}" != "en" ]]; then
    printf -- '--lang must be zh or en\n' >&2
    exit 1
fi

# ---------------------------- interaction helpers ----------------------------

# has_tty 判断能否与使用者交互。通过管道执行（curl | bash）时 stdin 不是终端，
# 但 /dev/tty 通常仍然可用，因此优先探测 /dev/tty。
has_tty() {
    [[ "${NON_INTERACTIVE}" == "yes" ]] && return 1
    [[ -r /dev/tty && -w /dev/tty ]] && return 0
    [[ -t 0 ]] && return 0
    return 1
}

read_tty() {
    local __var="$1" __prompt="$2" __reply=""
    if [[ -r /dev/tty ]]; then
        printf '%s' "${__prompt}" > /dev/tty
        IFS= read -r __reply < /dev/tty || __reply=""
    else
        printf '%s' "${__prompt}"
        IFS= read -r __reply || __reply=""
    fi
    printf -v "${__var}" '%s' "${__reply}" 2>/dev/null || eval "${__var}=\${__reply}"
}

confirm() {
    [[ "${ASSUME_YES}" == "yes" ]] && return 0
    if ! has_tty; then
        return 0
    fi
    local reply=""
    read_tty reply "$1 [y/N] "
    [[ "${reply}" == "y" || "${reply}" == "Y" ]]
}

ask_value() {
    # $1=变量名 $2=消息key
    local __var="$1" __key="$2" __val=""
    while true; do
        read_tty __val "$(t "${__key}"): "
        if [[ -n "${__val}" ]]; then
            eval "${__var}=\${__val}"
            return 0
        fi
        warn input_empty
    done
}

# ---------------------------- language selection ----------------------------
select_language() {
    if [[ -n "${UI_LANG}" ]]; then
        return 0
    fi
    if ! has_tty; then
        UI_LANG="zh"
        return 0
    fi

    local choice=""
    while true; do
        {
            printf '\n'
            printf -- '--------------------------------------------------------------------------------\n'
            printf '  %s\n' "请选择语言 / Select language"
            printf -- '--------------------------------------------------------------------------------\n'
            printf '    1) 中文 (Chinese)\n'
            printf '    2) English\n'
            printf -- '--------------------------------------------------------------------------------\n'
        } > /dev/tty 2>/dev/null || true

        read_tty choice "  请输入序号 / Enter choice [1]: "
        case "${choice}" in
            ''|1|zh|ZH|cn|CN) UI_LANG="zh"; break ;;
            2|en|EN)          UI_LANG="en"; break ;;
            *) printf '  Invalid input / 输入无效\n' > /dev/tty 2>/dev/null || true ;;
        esac
    done
    ok lang_set
}

select_mode_and_role() {
    if [[ -z "${MODE}" ]]; then
        if has_tty; then
            local c=""
            while true; do
                {
                    printf '\n  %s\n' "$(t mode_prompt)"
                    printf '    1) %s\n' "$(t mode_binary)"
                    printf '    2) %s\n' "$(t mode_docker)"
                } > /dev/tty 2>/dev/null || true
                read_tty c "  $(t lang_choice) [1]: "
                case "${c}" in
                    ''|1) MODE="binary"; break ;;
                    2)    MODE="docker"; break ;;
                    *)    warn lang_invalid ;;
                esac
            done
        else
            MODE="binary"
        fi
    fi
    [[ "${MODE}" == "binary" || "${MODE}" == "docker" ]] || die bad_mode

    if [[ -z "${ROLE}" ]]; then
        if has_tty; then
            local c=""
            while true; do
                {
                    printf '\n  %s\n' "$(t role_prompt)"
                    printf '    1) %s\n' "$(t role_master)"
                    printf '    2) %s\n' "$(t role_node)"
                } > /dev/tty 2>/dev/null || true
                read_tty c "  $(t lang_choice) [1]: "
                case "${c}" in
                    ''|1) ROLE="master"; break ;;
                    2)    ROLE="node"; break ;;
                    *)    warn lang_invalid ;;
                esac
            done
        else
            ROLE="master"
        fi
    fi
    [[ "${ROLE}" == "master" || "${ROLE}" == "node" ]] || die bad_role

    # 必须用 if，不能写成 [[ ... ]] && SERVICE_NAME=...：
    # 那样在 ROLE=master 时整个 && 列表返回 1，作为函数最后一条命令会让函数返回 1，
    # 配合 set -e 就会静默终止整个脚本（表现为「退出码 1 且没有任何输出」）。
    if [[ "${ROLE}" == "node" ]]; then
        SERVICE_NAME="dnscat-node"
    else
        SERVICE_NAME="dnscat-server"
    fi
}

# 被控节点缺参数时，能交互就问，不能就报错退出。
collect_node_params() {
    [[ "${ROLE}" == "node" ]] || return 0

    if [[ -z "${MASTER_URL}" ]]; then
        has_tty && ask_value MASTER_URL ask_master_url || die node_need_master
    fi
    if [[ -z "${NODE_ID}" ]]; then
        has_tty && ask_value NODE_ID ask_node_id || die node_need_id
    fi
    if [[ -z "${CLUSTER_TOKEN}" ]]; then
        has_tty && ask_value CLUSTER_TOKEN ask_cluster_token || die node_need_token
    fi
    if [[ -z "${PUBLIC_IP}" ]]; then
        if has_tty; then
            ask_value PUBLIC_IP ask_public_ip
        else
            warn node_no_pubip_warn
        fi
    fi
}

# ---------------------------- environment ----------------------------
require_root() { [[ "${EUID}" -eq 0 ]] || die need_root; }

detect_os() {
    local s
    s="$(uname -s)"
    [[ "${s}" == "Linux" ]] || die only_linux "${s}"
}

detect_arch() {
    local m
    m="$(uname -m)"
    case "${m}" in
        x86_64|amd64)          echo "amd64" ;;
        i386|i486|i586|i686)   echo "386" ;;
        aarch64|arm64)         echo "arm64" ;;
        armv7l|armv7|armhf|armv6l) echo "armv7" ;;
        *)                     die arch_unsupported "${m}" ;;
    esac
}

has_systemd() { [[ -d /run/systemd/system ]]; }

rand_hex() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex "$1"
    else
        head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
    fi
}

# ---------------------------- port 53 ----------------------------
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

port_is_busy() { [[ -n "$(port_listeners "$1")" ]]; }

describe_port() {
    local port="$1" line=""
    log port_busy_list "${port}"
    while IFS= read -r line; do
        [[ -z "${line}" ]] && continue
        printf '     %s\n' "${line}"
    done < <(port_listeners "${port}")
}

# collect_upstream_dns 输出可用的上游 DNS 地址（每行一个），排除环回地址。
#
# 关闭 stub 监听后，指向 127.0.0.53 的 /etc/resolv.conf 会立刻失效，
# 因此必须先确定真正可用的上游。来源按可靠性排序：
#   1) systemd-resolved 生成的非 stub 版 resolv.conf
#   2) resolvectl 报告的 DNS 服务器（上游可能由 resolvconf/NetworkManager 提供，
#      此时上一项可能是空的——这在真实主机上确实会发生）
#   3) 当前 resolv.conf 里已有的非环回地址
collect_upstream_dns() {
    local found=""

    if [[ -r /run/systemd/resolve/resolv.conf ]]; then
        found="$(grep -E '^[[:space:]]*nameserver[[:space:]]+' /run/systemd/resolve/resolv.conf 2>/dev/null \
                 | awk '{print $2}' | grep -vE '^127\.|^::1$' || true)"
    fi

    if [[ -z "${found}" ]] && command -v resolvectl >/dev/null 2>&1; then
        found="$(resolvectl status 2>/dev/null \
                 | sed -nE 's/^[[:space:]]*(Current DNS Server|DNS Servers):[[:space:]]*(.*)$/\2/p' \
                 | tr ' ' '\n' | grep -vE '^$|^127\.|^::1$' || true)"
    fi

    if [[ -z "${found}" ]] && [[ -r /etc/resolv.conf ]]; then
        found="$(grep -E '^[[:space:]]*nameserver[[:space:]]+' /etc/resolv.conf 2>/dev/null \
                 | awk '{print $2}' | grep -vE '^127\.|^::1$' || true)"
    fi

    printf '%s\n' "${found}" | sed '/^$/d' | awk '!seen[$0]++'
}

# fix_resolv_conf_for_stub_off 在关闭 stub 前确保 /etc/resolv.conf 仍指向可用上游。
# 找不到任何上游时直接中止，绝不把主机留在无法解析域名的状态。
fix_resolv_conf_for_stub_off() {
    local uses_stub="no"
    if [[ -e /etc/resolv.conf ]] \
       && grep -qE '^[[:space:]]*nameserver[[:space:]]+127\.0\.0\.53' /etc/resolv.conf 2>/dev/null; then
        uses_stub="yes"
    fi
    # 已是指向 stub-resolv.conf 的符号链接时同样需要处理
    if [[ -L /etc/resolv.conf ]] && [[ "$(readlink -f /etc/resolv.conf)" == *stub-resolv.conf ]]; then
        uses_stub="yes"
    fi
    [[ "${uses_stub}" == "yes" ]] || return 0

    local upstreams
    upstreams="$(collect_upstream_dns)"
    if [[ -z "${upstreams}" ]]; then
        warn resolved_no_upstream
        die resolved_no_upstream_abort
    fi

    local backup
    backup="/etc/resolv.conf.dnscat-backup.$(date +%Y%m%d%H%M%S)"
    cp -aL /etc/resolv.conf "${backup}" 2>/dev/null || true

    # 写静态文件而非符号链接：符号链接的目标在某些主机上并不含 nameserver，
    # 那正是导致「关掉 stub 后全网不通」的原因。
    rm -f /etc/resolv.conf
    {
        printf '# Written by DNSCat install.sh.\n'
        printf '# 由 DNSCat install.sh 写入：关闭 systemd-resolved 的 53 stub 监听后，\n'
        printf '# 这里固化了原先生效的上游 DNS，保证宿主机解析不中断。\n'
        printf '# 原文件已备份为 %s\n' "${backup}"
        while IFS= read -r ns; do
            [[ -z "${ns}" ]] && continue
            printf 'nameserver %s\n' "${ns}"
        done <<< "${upstreams}"
    } > /etc/resolv.conf
    chmod 0644 /etc/resolv.conf

    ok resolved_resolvconf_static "${backup}"
    while IFS= read -r ns; do
        [[ -z "${ns}" ]] && continue
        printf '       nameserver %s\n' "${ns}"
    done <<< "${upstreams}"
}

free_systemd_resolved() {
    log resolved_found
    if ! confirm "$(t resolved_confirm)"; then
        die resolved_cancel
    fi

    install -d -m 0755 /etc/systemd/resolved.conf.d
    cat > /etc/systemd/resolved.conf.d/dnscat.conf <<'RESOLVED_EOF'
# Written by DNSCat install.sh / 由 DNSCat install.sh 写入
#
# DNSCat needs port 53 to serve authoritative DNS, so the local stub listener
# of systemd-resolved is disabled. systemd-resolved itself keeps running, so
# host name resolution is unaffected as long as /etc/resolv.conf points at
# /run/systemd/resolve/resolv.conf.
#
# DNSCat 需要独占 53 提供权威解析，故关闭 systemd-resolved 的本地 stub 监听。
# 服务本身继续运行，只要 /etc/resolv.conf 指向 /run/systemd/resolve/resolv.conf，
# 宿主机解析就不受影响。删除本文件并重启 systemd-resolved 即可恢复原状。
[Resolve]
DNSStubListener=no
RESOLVED_EOF
    ok resolved_written

    fix_resolv_conf_for_stub_off

    systemctl restart systemd-resolved || die resolved_restart_fail
    ok resolved_restarted

    if command -v getent >/dev/null 2>&1; then
        if getent hosts github.com >/dev/null 2>&1 || getent hosts cloudflare.com >/dev/null 2>&1; then
            ok dns_ok
        else
            warn dns_check_fail
        fi
    fi
    return 0
}

stop_host_dns_service() {
    local svc="$1"
    if ! systemctl list-unit-files 2>/dev/null | grep -q "^${svc}\.service"; then
        return 1
    fi
    log svc_found "${svc}" "${DNS_PORT}"
    warn svc_warn "${svc}"
    if ! confirm "$(t svc_confirm "${svc}")"; then
        die svc_cancel "${svc}"
    fi
    systemctl disable --now "${svc}" || true
    ok stopped "${svc}"

    if [[ -e /etc/resolv.conf ]] && grep -qE '^[[:space:]]*nameserver[[:space:]]+127\.0\.0\.1' /etc/resolv.conf 2>/dev/null; then
        warn resolvconf_localhost
    fi
    return 0
}

free_port_53_binary() {
    if ! port_is_busy "${DNS_PORT}"; then
        ok port_free "${DNS_PORT}"
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
                free_systemd_resolved || unknown+=("${n}") ;;
            dnscat-server|dnscat-node)
                # 占用者是 DNSCat 自己：重装本来就要替换它，直接停掉。
                log port_owner_self "${n}"
                systemctl stop "${n}" 2>/dev/null || true
                pkill -x "${n}" 2>/dev/null || true
                ok stopped "${n}" ;;
            dnsmasq)
                stop_host_dns_service dnsmasq || unknown+=("${n}") ;;
            named|bind9)
                stop_host_dns_service named || stop_host_dns_service bind9 || unknown+=("${n}") ;;
            unbound)
                stop_host_dns_service unbound || unknown+=("${n}") ;;
            pdns_server|pdns)
                stop_host_dns_service pdns || unknown+=("${n}") ;;
            coredns)
                stop_host_dns_service coredns || unknown+=("${n}") ;;
            pihole-FTL)
                warn pihole_found "${DNS_PORT}"
                warn pihole_warn
                unknown+=("${n}") ;;
            *)
                unknown+=("${n}") ;;
        esac
    done <<< "${names}"

    if [[ ${#unknown[@]} -gt 0 ]]; then
        hr
        warn unknown_owner "${unknown[*]}"
        warn unknown_hint_title
        warn unknown_hint1
        warn unknown_hint2 "${DNS_PORT}"
        warn unknown_hint3
        hr
        die port_abort "${DNS_PORT}"
    fi

    local i
    for i in 1 2 3 4 5; do
        if ! port_is_busy "${DNS_PORT}"; then
            ok port_released "${DNS_PORT}"
            return 0
        fi
        sleep 1
    done

    describe_port "${DNS_PORT}"
    die port_still_busy "${DNS_PORT}"
}

check_port_53_docker() {
    if ! port_is_busy "${DNS_PORT}"; then
        ok port_free "${DNS_PORT}"
        return 0
    fi

    local compose_name="docker-compose.master.yml"
    [[ "${ROLE}" == "node" ]] && compose_name="docker-compose.node.yml"

    describe_port "${DNS_PORT}"
    hr
    {
        printf '%s%s%s%s\n' "${C_BLD}" "${C_YEL}" "$(t docker_port_title "${DNS_PORT}")" "${C_RST}"
        printf '%s\n\n' "$(t docker_port_body "${DNS_PORT}")"
        printf '%s\n' "$(t docker_port_ways)"
        printf '%s\n' "$(t docker_port_w1)"
        printf '       sudo mkdir -p /etc/systemd/resolved.conf.d\n'
        printf '       printf "[Resolve]\\nDNSStubListener=no\\n" | sudo tee /etc/systemd/resolved.conf.d/dnscat.conf\n'
        printf '       sudo ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf\n'
        printf '       sudo systemctl restart systemd-resolved\n'
        printf '%s\n\n' "$(t docker_port_w1n)"
        printf '%s\n' "$(t docker_port_w2)"
        printf '       sudo systemctl disable --now <service>\n'
        printf '%s\n\n' "$(t docker_port_w2n)"
        printf '%s\n' "$(t docker_port_w3)"
        printf '%s\n' "$(t docker_port_w3a)"
        printf '%s\n\n' "$(t docker_port_w3b "${compose_name}")"
        printf '%s\n' "$(t docker_port_verify "${DNS_PORT}")"
    } >&2
    hr
    die docker_port_abort "${DNS_PORT}"
}

# ---------------------------- binaries ----------------------------
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

# ensure_repo 保证手头有一份仓库文件。
#
# 独立运行（curl | bash）时没有 compose 与 Dockerfile，而 Docker 模式和源码编译
# 都需要它们，因此按需拉取一份源码快照到临时目录，并把 REPO_ROOT/SCRIPT_DIR 指过去。
# 纯二进制安装不会走到这里——那条路只需要下载一个可执行文件。
ensure_repo() {
    if [[ "${STANDALONE}" == "no" ]]; then
        return 0
    fi

    local ref="master"
    if [[ "${VERSION}" != "latest" ]]; then
        ref="${VERSION}"
    fi

    local url
    if [[ "${ref}" == "master" ]]; then
        url="https://github.com/${DNSCAT_REPO}/archive/refs/heads/master.tar.gz"
    else
        url="https://github.com/${DNSCAT_REPO}/archive/refs/tags/${ref}.tar.gz"
    fi

    log repo_fetch "${url}"
    local tgz="${WORK_DIR}/src.tar.gz"
    download_to "${url}" "${tgz}" || die repo_fetch_fail "${url}"

    local dir="${WORK_DIR}/src"
    install -d -m 0755 "${dir}"
    # --strip-components=1 去掉 GitHub 归档自带的 <repo>-<ref>/ 顶层目录
    tar -xzf "${tgz}" -C "${dir}" --strip-components=1 || die repo_extract_fail

    [[ -f "${dir}/go.mod" ]] || die repo_extract_fail
    REPO_ROOT="${dir}"
    SCRIPT_DIR="${dir}/deploy"
    STANDALONE="no"
    ok repo_ready "${REPO_ROOT}"
}

download_to() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 --connect-timeout 15 -o "${dest}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -q -T 15 -t 3 -O "${dest}" "${url}"
    else
        die dl_need_tool
    fi
}

# arch_matches 校验 ELF 架构，避免误用为其他架构编译的产物。
arch_matches() {
    local f="$1" desc=""
    command -v file >/dev/null 2>&1 || return 1
    desc="$(file -bL "${f}" 2>/dev/null)" || return 1
    case "${ARCH}" in
        amd64) [[ "${desc}" == *"x86-64"* ]] ;;
        386)   [[ "${desc}" == *"Intel 80386"* ]] ;;
        arm64) [[ "${desc}" == *"aarch64"* ]] ;;
        armv7) [[ "${desc}" == *"ARM"* && "${desc}" != *"aarch64"* ]] ;;
        *)     return 1 ;;
    esac
}

# pick_binary：带架构后缀的名字直接采信；无后缀的必须经 file 确认架构一致。
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
        BIN_SERVER="${s}"; BIN_CLI="${c}"
    else
        local nd
        nd="$(pick_binary "${dir}" "dnscat-node_linux_${ARCH}" "dnscat-node" "node_linux" "node")" || return 1
        BIN_NODE="${nd}"
    fi
    ok use_local_bin "${dir}" "${ARCH}"
    return 0
}

SUMS_FILE=""

sha256_of() {
    local f="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "${f}" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "${f}" | awk '{print $1}'
    else
        return 1
    fi
}

# fetch_checksums 取回 Release 的 SHA256SUMS.txt。
#
# 取不到时的处理刻意分两种：
#   - 官方 GitHub 地址：中止。清单本该存在，取不到说明网络异常或发布未完成，
#     此时继续装等于把一个来源不明的可执行文件以 root 跑起来。
#   - 自定义 DNSCAT_RELEASE_BASE（内网镜像）：仅告警。镜像可能只同步了二进制。
fetch_checksums() {
    local base="$1"
    SUMS_FILE=""

    if ! sha256_of /dev/null >/dev/null 2>&1; then
        warn sum_no_tool
        return 0
    fi

    local dest="${WORK_DIR}/SHA256SUMS.txt"
    log sum_fetch "${base}/SHA256SUMS.txt"
    if download_to "${base}/SHA256SUMS.txt" "${dest}" 2>/dev/null && [[ -s "${dest}" ]]; then
        SUMS_FILE="${dest}"
        return 0
    fi

    if [[ -n "${DNSCAT_RELEASE_BASE}" ]]; then
        warn sum_missing_mirror "${base}"
        return 0
    fi
    die sum_missing_official "SHA256SUMS.txt"
}

# verify_checksum 核对单个文件。清单为 sha256sum 输出格式：`<hash>  <filename>`。
# 返回 1 表示确凿的不匹配；清单缺失或无该条目返回 0（由 fetch_checksums 决定严格程度）。
verify_checksum() {
    local path="$1" name="$2"
    [[ -n "${SUMS_FILE}" ]] || return 0

    local want
    want="$(awk -v n="${name}" '$2 == n || $2 == "*" n { print $1; exit }' "${SUMS_FILE}")"
    if [[ -z "${want}" ]]; then
        warn sum_no_entry "${name}"
        return 0
    fi

    local got
    got="$(sha256_of "${path}")" || return 0
    if [[ "${want}" != "${got}" ]]; then
        warn sum_mismatch "${name}" "${want}" "${got}"
        return 1
    fi
    ok sum_ok "${name}"
    return 0
}

download_release() {
    local base
    base="$(release_base)" || return 1

    log dl_start "${base}" "${ARCH}"
    local out="${WORK_DIR}/bin"
    install -d -m 0755 "${out}"

    local main_name
    if [[ "${ROLE}" == "master" ]]; then
        main_name="dnscat-server_linux_${ARCH}"
    else
        main_name="dnscat-node_linux_${ARCH}"
    fi

    if ! download_to "${base}/${main_name}" "${out}/${main_name}"; then
        warn dl_fail "${main_name}"
        return 1
    fi

    # 先取清单再逐个核对。主程序校验不通过必须中止：
    # 这个文件接下来会以 root 装到 /usr/local/bin 并由 systemd 常驻运行。
    fetch_checksums "${base}"
    verify_checksum "${out}/${main_name}" "${main_name}" || die sum_abort
    chmod +x "${out}/${main_name}"

    if [[ "${ROLE}" == "master" ]]; then
        BIN_SERVER="${out}/${main_name}"
        local cli_name="dnscat_linux_${ARCH}"
        if download_to "${base}/${cli_name}" "${out}/${cli_name}" 2>/dev/null \
            && verify_checksum "${out}/${cli_name}" "${cli_name}"; then
            chmod +x "${out}/${cli_name}"
            BIN_CLI="${out}/${cli_name}"
        else
            warn dl_cli_fail "${cli_name}"
            BIN_CLI=""
        fi
    else
        BIN_NODE="${out}/${main_name}"
    fi
    ok dl_done
    return 0
}

build_from_source() {
    log build_start "${ARCH}"
    command -v go >/dev/null 2>&1 || die build_no_go
    ensure_repo
    [[ -f "${REPO_ROOT}/go.mod" ]] || die build_no_mod "${REPO_ROOT:-<未知目录>}"

    local out="${WORK_DIR}/bin"
    install -d -m 0755 "${out}"

    local goarch="${ARCH}" goarm=""
    if [[ "${ARCH}" == "armv7" ]]; then
        goarch="arm"; goarm="7"
    fi

    if [[ "${ROLE}" == "master" ]]; then
        if [[ ! -f "${REPO_ROOT}/web/dist/index.html" ]]; then
            command -v npm >/dev/null 2>&1 || die build_web_no_npm
            log build_web
            ( cd "${REPO_ROOT}/web" && npm ci --no-audit --no-fund && npm run build ) || die build_web_fail
        else
            ok build_web_reuse
        fi

        # 把前端产物拷进 embed 包目录，使 server 二进制自带控制台，
        # 不再依赖运行时的工作目录。
        rm -rf "${REPO_ROOT}/internal/webui/dist"
        cp -r "${REPO_ROOT}/web/dist" "${REPO_ROOT}/internal/webui/dist"

        log build_go
        ( cd "${REPO_ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${goarch}" GOARM="${goarm}" \
            go build -tags embedui -trimpath -ldflags "-s -w" -o "${out}/dnscat-server" ./cmd/server ) \
            || die build_fail server
        ( cd "${REPO_ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${goarch}" GOARM="${goarm}" \
            go build -trimpath -ldflags "-s -w" -o "${out}/dnscat" ./cmd/cli ) \
            || die build_fail cli
        BIN_SERVER="${out}/dnscat-server"
        BIN_CLI="${out}/dnscat"
    else
        log build_go_node
        ( cd "${REPO_ROOT}" && CGO_ENABLED=0 GOOS=linux GOARCH="${goarch}" GOARM="${goarm}" \
            go build -trimpath -ldflags "-s -w" -o "${out}/dnscat-node" ./cmd/node ) \
            || die build_fail node
        BIN_NODE="${out}/dnscat-node"
    fi
    ok build_done
}

acquire_binaries() {
    if [[ -n "${BINARY_DIR}" ]]; then
        use_local_binaries "${BINARY_DIR}" || die bindir_missing "${ROLE}" "${BINARY_DIR}"
        return 0
    fi
    if [[ "${FROM_SOURCE}" == "yes" ]]; then
        build_from_source
        return 0
    fi
    # 仓库内自带 bin/ 时优先复用（本地开发与离线安装场景）
    if [[ -n "${REPO_ROOT}" ]] && use_local_binaries "${REPO_ROOT}/bin"; then
        return 0
    fi
    if download_release; then
        return 0
    fi
    warn no_release_cfg
    build_from_source
}

# ---------------------------- config & systemd ----------------------------
ensure_service_user() {
    id -u "${SERVICE_USER}" >/dev/null 2>&1 && return 0
    log user_create "${SERVICE_USER}"
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --shell /usr/sbin/nologin "${SERVICE_USER}" \
            || useradd --system --no-create-home "${SERVICE_USER}"
    elif command -v adduser >/dev/null 2>&1; then
        adduser --system --no-create-home --disabled-login "${SERVICE_USER}" 2>/dev/null \
            || adduser -S -H -D "${SERVICE_USER}"
    else
        die user_no_tool
    fi
    ok user_created "${SERVICE_USER}"
}

write_master_config() {
    local cfg="${CONFIG_DIR}/config.yaml"
    if [[ -f "${cfg}" ]]; then
        ok cfg_keep "${cfg}"
        local existing
        existing="$(sed -nE 's/^[[:space:]]*secret_token:[[:space:]]*"?([^"#]*)"?.*/\1/p' "${cfg}" | head -1 | tr -d '[:space:]')"
        [[ -n "${existing}" ]] && CLUSTER_TOKEN="${existing}"
        return 0
    fi

    [[ -n "${CLUSTER_TOKEN}" ]] || CLUSTER_TOKEN="$(rand_hex 32)"
    local jwt
    jwt="$(rand_hex 32)"

    local driver dsn
    if [[ -n "${DB_DSN}" ]]; then
        dsn="${DB_DSN}"
        if [[ "${dsn}" == *"@tcp("* ]]; then driver="mysql"; else driver="sqlite"; fi
    elif [[ "${DB_DRIVER}" == "mysql" ]]; then
        die cfg_mysql_need_dsn
    else
        driver="sqlite"
        dsn="${DATA_DIR}/dnscat.db"
    fi

    # 目录必须让服务账号能进入：0750 且属主 root:root 时，
    # 以 dnscat 身份运行的服务连目录都进不去，自然读不到里面的配置文件。
    install -d -m 0750 "${CONFIG_DIR}"
    chown "root:${SERVICE_USER}" "${CONFIG_DIR}" 2>/dev/null || true

    cat > "${cfg}" <<CONFIG_EOF
# Generated by DNSCat install.sh at $(date '+%Y-%m-%d %H:%M:%S %z')
# 由 DNSCat install.sh 生成。以下密钥为本机随机生成，请勿外传。
# 被控节点安装时需要用到 cluster.secret_token。

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
  # Empty on purpose: add your own nameservers on the console Nameservers page.
  # 留空：请在控制台「权威 NS 服务器」页添加本部署实际的 NS 主机名。
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

    # 实测服务账号确实能读到配置。这一步很关键：读不到时服务端会回退到
    # 内置默认值（含相对路径的数据库），报出的错误与权限毫无关联，极难排查。
    if command -v runuser >/dev/null 2>&1; then
        if ! runuser -u "${SERVICE_USER}" -- test -r "${cfg}" 2>/dev/null; then
            warn cfg_unreadable "${SERVICE_USER}" "${cfg}"
            chmod 0755 "${CONFIG_DIR}"
            chmod 0644 "${cfg}"
            ok cfg_perm_relaxed "${cfg}"
        fi
    elif command -v sudo >/dev/null 2>&1; then
        if ! sudo -n -u "${SERVICE_USER}" test -r "${cfg}" 2>/dev/null; then
            warn cfg_unreadable "${SERVICE_USER}" "${cfg}"
            chmod 0755 "${CONFIG_DIR}"
            chmod 0644 "${cfg}"
            ok cfg_perm_relaxed "${cfg}"
        fi
    fi

    ok cfg_written "${cfg}"
}

write_master_unit() {
    cat > /etc/systemd/system/dnscat-server.service <<UNIT_EOF
[Unit]
Description=DNSCat Authoritative DNS Server (master)
Documentation=https://github.com/MengMengCode/DNSCat
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

# Runs unprivileged but still needs to bind port 53 and send ICMP probes.
# 以非 root 运行，但仍需绑定 53 特权端口并发送 ICMP 探测。
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
    ok unit_written /etc/systemd/system/dnscat-server.service
}

write_node_unit() {
    # 目录属主给到服务账号，与主控保持一致。
    # node 当前靠 systemd 以 root 读 EnvironmentFile，不受目录权限影响，
    # 但保持一致可以避免以后改成进程自读时踩到同一个坑。
    install -d -m 0750 "${CONFIG_DIR}"
    chown "root:${SERVICE_USER}" "${CONFIG_DIR}" 2>/dev/null || true

    cat > "${CONFIG_DIR}/node.env" <<NODEENV_EOF
# Generated by DNSCat install.sh. The token must match the master's
# cluster.secret_token exactly.
# 由 DNSCat install.sh 生成。集群令牌须与主控 cluster.secret_token 完全一致。
DNSCAT_MASTER_URL=${MASTER_URL}
DNSCAT_NODE_ID=${NODE_ID}
DNSCAT_CLUSTER_TOKEN=${CLUSTER_TOKEN}
DNSCAT_PUBLIC_IP=${PUBLIC_IP}
DNSCAT_DNS_PORT=${DNS_PORT}
NODEENV_EOF
    chmod 0640 "${CONFIG_DIR}/node.env"
    chown "root:${SERVICE_USER}" "${CONFIG_DIR}/node.env" 2>/dev/null || true
    ok nodeenv_written "${CONFIG_DIR}/node.env"

    local pubarg=""
    [[ -n "${PUBLIC_IP}" ]] && pubarg=' --public-ip ${DNSCAT_PUBLIC_IP}'

    cat > /etc/systemd/system/dnscat-node.service <<UNIT_EOF
[Unit]
Description=DNSCat Edge Node
Documentation=https://github.com/MengMengCode/DNSCat
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${DATA_DIR}
EnvironmentFile=${CONFIG_DIR}/node.env
# The token is deliberately not passed on the command line: systemd expands
# EnvironmentFile values into ExecStart, which would expose it in ps output.
# node reads DNSCAT_CLUSTER_TOKEN from the environment instead.
# 令牌刻意不放命令行：systemd 会把 EnvironmentFile 的值展开进 ExecStart，
# 那样 ps 输出里同机任何用户都能看到。node 直接读环境变量。
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
    ok unit_written /etc/systemd/system/dnscat-node.service
}

install_binaries() {
    install -d -m 0755 "${PREFIX}"
    if [[ "${ROLE}" == "master" ]]; then
        install -m 0755 "${BIN_SERVER}" "${PREFIX}/dnscat-server"
        ok bin_installed "${PREFIX}/dnscat-server"
        if [[ -n "${BIN_CLI}" && -f "${BIN_CLI}" ]]; then
            install -m 0755 "${BIN_CLI}" "${PREFIX}/dnscat"
            ok cli_installed "${PREFIX}/dnscat"
        else
            warn cli_skipped
        fi
    else
        install -m 0755 "${BIN_NODE}" "${PREFIX}/dnscat-node"
        ok bin_installed "${PREFIX}/dnscat-node"
    fi
}

# current_unit_logs 只返回「本次服务启动」产生的日志。
#
# 早前的写法是 journalctl -n 200 后 sed | tail -1。journald 会保留上一次安装的
# 日志，重装时第一轮轮询就能匹配到上一次的口令，循环随即 break，
# 于是把一个早已失效的口令当作本次的初始口令打印出来——对「口令只显示这一次」
# 的设计来说这是致命的：使用者拿到的口令根本登不进去。
# _SYSTEMD_INVOCATION_ID 精确对应 unit 的当前这一次启动，天然排除历史记录。
current_unit_logs() {
    local inv="" since=""

    inv="$(systemctl show -p InvocationID --value dnscat-server 2>/dev/null)" || inv=""
    if [[ -n "${inv}" ]]; then
        journalctl "_SYSTEMD_INVOCATION_ID=${inv}" --no-pager 2>/dev/null || true
        return 0
    fi

    # systemd < 232 没有 InvocationID，退回按本次进入 active 的时刻过滤
    since="$(systemctl show -p ActiveEnterTimestamp --value dnscat-server 2>/dev/null)" || since=""
    if [[ -n "${since}" ]]; then
        journalctl -u dnscat-server --since "${since}" --no-pager 2>/dev/null || true
        return 0
    fi

    journalctl -u dnscat-server --no-pager -n 200 2>/dev/null || true
}

# 首次安装才会有随机口令输出；库里已有管理员时不会再打印。
print_initial_credentials() {
    local pw="" user="" i logs="" existed="no"

    if command -v journalctl >/dev/null 2>&1; then
        for i in $(seq 1 30); do
            logs="$(current_unit_logs)"
            pw="$(printf '%s\n' "${logs}" \
                | sed -nE 's/.*初始随机口令[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
            if [[ -n "${pw}" ]]; then
                # 账号必须与口令取自同一次启动的同一段输出。
                # 排除「已存在」那行：它同样含「管理员账号」，
                # 早前会被匹配成账号名，打印出「已存在（口令仅以」这种碎片。
                user="$(printf '%s\n' "${logs}" \
                    | grep '管理员账号' | grep -v '已存在' \
                    | sed -nE 's/.*管理员账号[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
                break
            fi
            # 库里已有管理员时服务明确打印「已存在」，不必再等满 30 轮
            if printf '%s\n' "${logs}" | grep -q '管理员账号.*已存在'; then
                existed="yes"
                break
            fi
            sleep 1
        done
    fi

    hr
    if [[ -n "${pw}" ]]; then
        printf '%s%s%s\n' "${C_BLD}" "$(t cred_title)" "${C_RST}"
        plain cred_user "${user:-admin}"
        printf '%s%s%s\n' "${C_BLD}" "$(t cred_pass "${pw}")" "${C_RST}"
    elif [[ "${existed}" == "yes" ]]; then
        # 服务明确报告库里已有管理员，无需再列举其他可能原因
        plain cred_kept
        plain cred_none_reset
    else
        plain cred_none
        plain cred_none1
        plain cred_none2
        plain cred_none_log
        plain cred_none_reset
    fi
    hr
}

# print_edge_join_command 输出边缘节点的一键加入命令。
# 令牌与主控地址直接填好，使用者只需替换节点标识与该机公网 IP。
print_edge_join_command() {
    local master_ip="$1"
    local raw="https://raw.githubusercontent.com/${DNSCAT_REPO}/master/deploy/install.sh"

    printf '\n'
    printf '%s%s%s\n' "${C_BLD}" "$(t edge_cmd_title)" "${C_RST}"
    printf '\n'
    # 刻意输出成单行：多行加反斜杠从终端里复制极易漏掉续行符，
    # 粘到目标机上就成了半条命令。
    printf '  curl -fsSL %s | sudo bash -s -- --yes --lang %s --mode binary --role node --master-url http://%s:%s --node-id edge-01 --cluster-token %s --public-ip <EDGE_PUBLIC_IP>\n' \
        "${raw}" "${UI_LANG}" "${master_ip}" "${HTTP_PORT}" "${CLUSTER_TOKEN}"
    printf '\n'
    plain edge_cmd_note
}

host_ip() {
    local ip=""
    ip="$(hostname -I 2>/dev/null | awk '{print $1}')" || true
    [[ -z "${ip}" ]] && ip="<host-ip>"
    printf '%s' "${ip}"
}

summary_binary() {
    local ip
    ip="$(host_ip)"
    printf '\n'; hr
    printf '%s%s%s\n' "${C_BLD}" "$(t done_binary "${ROLE}")" "${C_RST}"
    hr
    if [[ "${ROLE}" == "master" ]]; then
        print_initial_credentials
        plain sum_web "${ip}" "${HTTP_PORT}"
        plain sum_dns "${ip}" "${DNS_PORT}"
        plain sum_cfg "${CONFIG_DIR}/config.yaml"
        plain sum_data "${DATA_DIR}"
        plain sum_token "${CLUSTER_TOKEN}"
        plain sum_token_note
        print_edge_join_command "${ip}"
        printf '\n'
        plain next_title
        plain next1
        plain next2
        plain next3
        printf '\n'
        plain tls_hint "${HTTP_PORT}"
    else
        plain sum_nodeid "${NODE_ID}"
        plain sum_master "${MASTER_URL}"
        plain sum_pubip "${PUBLIC_IP:-<unset>}"
        plain sum_dns "${ip}" "${DNS_PORT}"
        plain sum_credfile "${CONFIG_DIR}/node.env"
        printf '\n'
        plain node_online_hint
    fi
    printf '\n'
    plain cmds
    printf '  systemctl status %s\n' "${SERVICE_NAME}"
    printf '  journalctl -u %s -f\n' "${SERVICE_NAME}"
    if [[ "${ROLE}" == "master" ]]; then
        plain cmd_cli
        plain cmd_reset
    fi
    local raw="https://raw.githubusercontent.com/${DNSCAT_REPO}/master/deploy/install.sh"
    plain cmd_upgrade "${raw}" "${ROLE}"
    plain cmd_uninstall "${raw}"
    hr
}

summary_docker() {
    local compose_bin="$1" compose_file="$2"
    local ip
    ip="$(host_ip)"
    printf '\n'; hr
    printf '%s%s%s\n' "${C_BLD}" "$(t done_docker "${ROLE}")" "${C_RST}"
    hr
    if [[ "${ROLE}" == "master" ]]; then
        plain sum_web "${ip}" "${HTTP_PORT}"
        plain sum_dns "${ip}" "${DNS_PORT}"
        plain sum_envfile "${SCRIPT_DIR}/.env"
        plain sum_token "${CLUSTER_TOKEN}"
        plain sum_token_note
        print_edge_join_command "${ip}"
    else
        plain sum_nodeid "${NODE_ID}"
        plain sum_master "${MASTER_URL}"
    fi
    printf '\n'
    plain cmds_docker "${SCRIPT_DIR}"
    printf '  %s -f %s ps\n' "${compose_bin}" "${compose_file}"
    printf '  %s -f %s logs -f\n' "${compose_bin}" "${compose_file}"
    printf '  %s -f %s restart\n' "${compose_bin}" "${compose_file}"
    printf '  %s -f %s down\n' "${compose_bin}" "${compose_file}"
    hr
}

# ---------------------------- flows ----------------------------
install_binary_mode() {
    require_root
    has_systemd || die no_systemd

    collect_node_params

    # 重装场景：先停掉本机已有的同名服务，否则它自己占着端口。
    if [[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]] \
       && systemctl is-active --quiet "${SERVICE_NAME}"; then
        log stop_self "${SERVICE_NAME}"
        systemctl stop "${SERVICE_NAME}" || true
        ok stopped "${SERVICE_NAME}"
    fi

    free_port_53_binary

    if [[ "${ROLE}" == "master" ]] && port_is_busy "${HTTP_PORT}"; then
        describe_port "${HTTP_PORT}"
        die port_http_busy "${HTTP_PORT}"
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

    log waiting_svc
    local i
    for i in $(seq 1 20); do
        systemctl is-active --quiet "${SERVICE_NAME}" && break
        sleep 1
    done
    if ! systemctl is-active --quiet "${SERVICE_NAME}"; then
        warn svc_recent_log "${SERVICE_NAME}"
        journalctl -u "${SERVICE_NAME}" -n 40 --no-pager 2>/dev/null || true
        die svc_start_fail
    fi
    ok svc_running "${SERVICE_NAME}"

    summary_binary
}

install_docker_mode() {
    require_root
    command -v docker >/dev/null 2>&1 || die docker_missing

    local compose_bin=""
    if docker compose version >/dev/null 2>&1; then
        compose_bin="docker compose"
    elif command -v docker-compose >/dev/null 2>&1; then
        compose_bin="docker-compose"
    else
        die compose_missing
    fi

    # Docker 模式需要 compose 与 Dockerfile（镜像从源码构建），
    # 独立运行时先取一份源码快照。
    ensure_repo

    local compose_file="docker-compose.master.yml"
    [[ "${ROLE}" == "node" ]] && compose_file="docker-compose.node.yml"
    [[ -f "${SCRIPT_DIR}/${compose_file}" ]] || die compose_file_missing "${SCRIPT_DIR}/${compose_file}"

    check_port_53_docker

    local env_file="${SCRIPT_DIR}/.env"
    if [[ -f "${env_file}" ]]; then
        ok env_reuse "${env_file}"
        CLUSTER_TOKEN="$(sed -nE 's/^CLUSTER_TOKEN=(.*)$/\1/p' "${env_file}" | head -1)"
        if [[ "${ROLE}" == "node" ]]; then
            [[ -n "${MASTER_URL}" ]] || MASTER_URL="$(sed -nE 's/^MASTER_URL=(.*)$/\1/p' "${env_file}" | head -1)"
            [[ -n "${NODE_ID}" ]]    || NODE_ID="$(sed -nE 's/^NODE_ID=(.*)$/\1/p' "${env_file}" | head -1)"
            [[ -n "${PUBLIC_IP}" ]]  || PUBLIC_IP="$(sed -nE 's/^NODE_PUBLIC_IP=(.*)$/\1/p' "${env_file}" | head -1)"
        fi
    else
        if [[ "${ROLE}" == "master" ]]; then
            [[ -n "${CLUSTER_TOKEN}" ]] || CLUSTER_TOKEN="$(rand_hex 32)"
            cat > "${env_file}" <<ENV_EOF
# Generated by DNSCat install.sh. Do not commit.
# 由 DNSCat install.sh 生成，请勿提交版本库。
MYSQL_ROOT_PASSWORD=$(rand_hex 24)
JWT_SECRET=$(rand_hex 32)
CLUSTER_TOKEN=${CLUSTER_TOKEN}
ENV_EOF
        else
            collect_node_params
            [[ -n "${PUBLIC_IP}" ]] || die node_need_pubip
            cat > "${env_file}" <<ENV_EOF
# Generated by DNSCat install.sh. Do not commit.
# 由 DNSCat install.sh 生成，请勿提交版本库。
MASTER_URL=${MASTER_URL}
NODE_ID=${NODE_ID}
CLUSTER_TOKEN=${CLUSTER_TOKEN}
NODE_PUBLIC_IP=${PUBLIC_IP}
ENV_EOF
        fi
        chmod 0600 "${env_file}"
        ok env_written "${env_file}"
    fi

    # 记下启动时刻，供后面按 --since 过滤容器日志，避免读到上一次安装的输出
    local DOCKER_UP_SINCE
    DOCKER_UP_SINCE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    # 默认拉取 CI 发布的多架构镜像：目标机上不必装 Go/Node，也不用等几分钟编译。
    # compose 里 image 与 build 段并存，--from-source 时才就地构建。
    #
    # DNSCAT_VERSION 会映射成镜像 tag：latest 用 :latest，v1.2.3 用 :1.2.3
    # （容器镜像 tag 不带 v 前缀，见 docker.yml 的 type=semver,pattern={{version}}）。
    if [[ "${FROM_SOURCE}" == "yes" ]]; then
        log docker_building
        ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" up -d --build ) || die docker_up_fail
    else
        local img_tag="latest"
        if [[ "${VERSION}" != "latest" ]]; then
            img_tag="${VERSION#v}"
        fi
        local owner="${DNSCAT_REPO%%/*}"
        # GHCR 路径必须全小写，而 GitHub 用户名允许大写
        owner="$(printf '%s' "${owner}" | tr '[:upper:]' '[:lower:]')"
        export DNSCAT_SERVER_IMAGE="ghcr.io/${owner}/dnscat-server:${img_tag}"
        export DNSCAT_NODE_IMAGE="ghcr.io/${owner}/dnscat-node:${img_tag}"

        log docker_pulling "${img_tag}"
        # 拉取失败就退回本地构建：镜像可能尚未发布，或所在网络访问不了 ghcr.io。
        # 直接中止会让「一键安装」在完全可自救的情况下失败。
        if ! ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" pull ); then
            warn docker_pull_fail
            log docker_building
            ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" up -d --build ) || die docker_up_fail
        else
            ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" up -d ) || die docker_up_fail
        fi
    fi
    ( cd "${SCRIPT_DIR}" && ${compose_bin} -f "${compose_file}" ps ) || true

    if [[ "${ROLE}" == "master" ]]; then
        local i pw="" user="" logs=""
        # --since 把日志限定在本次启动之后。容器被复用（未重建）时旧日志还在，
        # 不加限制会读到上一次安装的口令，那个口令已经与库里的不一致了。
        for i in $(seq 1 60); do
            logs="$( cd "${SCRIPT_DIR}" \
                && ${compose_bin} -f "${compose_file}" logs --no-color --since "${DOCKER_UP_SINCE}" server 2>/dev/null \
                || ${compose_bin} -f "${compose_file}" logs --no-color server 2>/dev/null \
                || true )"
            pw="$(printf '%s\n' "${logs}" | sed -nE 's/.*初始随机口令[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
            if [[ -n "${pw}" ]]; then
                # 排除「已存在」那行，它也含「管理员账号」
                user="$(printf '%s\n' "${logs}" \
                    | grep '管理员账号' | grep -v '已存在' \
                    | sed -nE 's/.*管理员账号[^:]*:[[:space:]]*([^[:space:]]+).*/\1/p' | tail -1)"
                break
            fi
            printf '%s\n' "${logs}" | grep -q '管理员账号.*已存在' && break
            sleep 2
        done
        hr
        if [[ -n "${pw}" ]]; then
            printf '%s%s%s\n' "${C_BLD}" "$(t cred_title)" "${C_RST}"
            plain cred_user "${user:-admin}"
            printf '%s%s%s\n' "${C_BLD}" "$(t cred_pass "${pw}")" "${C_RST}"
        else
            plain cred_none
            plain cred_none1
            plain cred_none2
        fi
        hr
    fi

    summary_docker "${compose_bin}" "${compose_file}"
}

do_upgrade() {
    require_root
    has_systemd || die upgrade_only_systemd
    [[ -f "/etc/systemd/system/${SERVICE_NAME}.service" ]] || die upgrade_not_installed "${SERVICE_NAME}.service"

    log upgrade_start "${SERVICE_NAME}"
    acquire_binaries
    systemctl stop "${SERVICE_NAME}" || true
    install_binaries
    systemctl daemon-reload
    systemctl start "${SERVICE_NAME}"
    sleep 2
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        ok upgrade_restarted "${SERVICE_NAME}"
    else
        die upgrade_fail "${SERVICE_NAME}" "${SERVICE_NAME}"
    fi
    log upgrade_done
}

do_uninstall() {
    require_root
    log uninstall_start

    local u
    for u in dnscat-server dnscat-node; do
        if [[ -f "/etc/systemd/system/${u}.service" ]]; then
            systemctl disable --now "${u}" 2>/dev/null || true
            rm -f "/etc/systemd/system/${u}.service"
            ok uninstall_unit "${u}.service"
        fi
    done
    systemctl daemon-reload 2>/dev/null || true

    rm -f "${PREFIX}/dnscat-server" "${PREFIX}/dnscat-node" "${PREFIX}/dnscat"
    ok uninstall_bin

    if [[ -f /etc/systemd/resolved.conf.d/dnscat.conf ]]; then
        if confirm "$(t uninstall_resolved)"; then
            rm -f /etc/systemd/resolved.conf.d/dnscat.conf
            systemctl restart systemd-resolved 2>/dev/null || true
            ok uninstall_resolved_ok
        fi
    fi

    if [[ "${DO_PURGE}" == "yes" ]]; then
        warn purge_warn
        if confirm "$(t purge_confirm "${CONFIG_DIR}" "${DATA_DIR}")"; then
            rm -rf "${CONFIG_DIR}" "${DATA_DIR}"
            ok purge_done
        fi
    else
        ok purge_kept "${CONFIG_DIR}" "${DATA_DIR}"
    fi
    log uninstall_done
}

main() {
    detect_os

    # 语言必须最先确定：后续所有提示（包括架构不支持的报错）都要用它。
    select_language

    ARCH="$(detect_arch)"

    WORK_DIR="$(mktemp -d)"
    trap 'rm -rf "${WORK_DIR}"' EXIT

    if [[ "${DO_UNINSTALL}" == "yes" ]]; then
        SERVICE_NAME="dnscat-server"
        printf '\n'; hr
        printf '%s%s%s\n' "${C_BLD}" "$(t banner)" "${C_RST}"
        hr
        do_uninstall
        return
    fi

    select_mode_and_role

    printf '\n'; hr
    printf '%s%s%s  %s\n' "${C_BLD}" "$(t banner)" "${C_RST}" \
        "$(t banner_info "${ARCH}" "${MODE}" "${ROLE}")"
    hr

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
