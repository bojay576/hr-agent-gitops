#!/usr/bin/env bash
set -euo pipefail

# ============================================================
#  MCP HR Assistant 一键卸载脚本
#  删除部署脚本创建的所有 Kubernetes 资源
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NAMESPACE="${NAMESPACE:-mcp-services}"
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
BOLD='\033[1m'

info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
err()   { echo -e "${RED}[ERR]${NC}   $*"; }
step()  { echo -e "\n${CYAN}${BOLD}==> $*${NC}\n"; }
title() { echo -e "${BOLD}$*${NC}"; }

# ---- 前置检查 ----
check_prerequisites() {
    step "检查前置条件"

    if ! command -v kubectl &>/dev/null; then
        err "kubectl 未安装"
        exit 1
    fi
    info "kubectl: $(kubectl version --client --short 2>/dev/null || kubectl version --client)"

    if ! kubectl cluster-info &>/dev/null; then
        err "无法连接 Kubernetes 集群，请检查 kubeconfig"
        exit 1
    fi
    info "集群连接正常: $(kubectl config current-context)"

    # 检查命名空间是否存在
    if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
        info "命名空间 '${NAMESPACE}' 不存在，无需卸载"
        exit 0
    fi
}

# ---- 确认卸载 ----
confirm() {
    step "确认卸载"

    echo "  即将删除命名空间: ${BOLD}${NAMESPACE}${NC}"
    echo ""
    echo "  该命名空间包含以下资源："
    kubectl get all,pvc,configmap,secret -n "$NAMESPACE" -o name 2>/dev/null | sed 's/^/    /' || echo "    （无资源）"
    echo ""

    # 检查是否有 PVC（持久数据）
    local has_pvc
    has_pvc=$(kubectl get pvc -n "$NAMESPACE" -o name 2>/dev/null || true)
    if [ -n "$has_pvc" ]; then
        warn "检测到持久卷声明 (PVC)，删除后将丢失所有数据！"
        echo ""
        echo "  数据备份选项："
        echo "    [b] 先备份 MySQL 数据，再卸载"
        echo "    [c] 直接继续卸载（不备份）"
        echo "    [q] 取消卸载"
        read -r -p "  请选择 [b/c/q] (默认 q): " backup_choice
        backup_choice="${backup_choice:-q}"

        case "$backup_choice" in
            b|B)
                backup_mysql
                ;;
            c|C)
                warn "确认不备份，数据将永久丢失"
                ;;
            *)
                info "取消卸载"
                exit 0
                ;;
        esac
    else
        echo "  未检测到持久数据。"
        read -r -p "  确认卸载所有资源？(y/N): " confirm_choice
        if [ "${confirm_choice}" != "y" ] && [ "${confirm_choice}" != "Y" ]; then
            info "取消卸载"
            exit 0
        fi
    fi
}

# ---- 备份 MySQL 数据 ----
backup_mysql() {
    step "备份 MySQL 数据"

    local backup_dir="${SCRIPT_DIR}/backup"
    mkdir -p "$backup_dir"
    local backup_file="${backup_dir}/hr_db_$(date +%Y%m%d_%H%M%S).sql"

    # 获取 MySQL root 密码
    local mysql_pod
    mysql_pod=$(kubectl get pod -n "$NAMESPACE" -l app=mysql -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

    if [ -z "$mysql_pod" ]; then
        warn "MySQL Pod 未运行，跳过数据备份"
        read -r -p "  确认继续卸载？(y/N): " confirm_choice
        if [ "${confirm_choice}" != "y" ] && [ "${confirm_choice}" != "Y" ]; then
            info "取消卸载"
            exit 0
        fi
        return
    fi

    local root_password
    root_password=$(kubectl get secret -n "$NAMESPACE" mysql-secret -o jsonpath='{.data.root-password}' 2>/dev/null | base64 -d || echo "")

    if [ -n "$root_password" ]; then
        info "正在导出 MySQL 数据到 ${backup_file}..."
        if kubectl exec -n "$NAMESPACE" "$mysql_pod" -- mysqldump -u root -p"$root_password" hr_db > "$backup_file" 2>/dev/null; then
            info "备份完成: ${backup_file}"
            echo "  恢复方法: kubectl exec -i -n ${NAMESPACE} deploy/mysql -- mysql -u root -p\"${root_password}\" hr_db < ${backup_file}"
        else
            warn "备份失败，文件可能不完整"
        fi
    else
        warn "无法获取 MySQL 密码，跳过数据备份"
    fi

    echo ""
    read -r -p "  确认继续卸载？(y/N): " confirm_choice
    if [ "${confirm_choice}" != "y" ] && [ "${confirm_choice}" != "Y" ]; then
        info "取消卸载"
        exit 0
    fi
}

# ---- 执行卸载 ----
do_uninstall() {
    step "执行卸载"

    info "删除命名空间: ${NAMESPACE}"
    kubectl delete namespace "$NAMESPACE" --wait=false 2>/dev/null || true

    # 等待命名空间完全删除
    info "等待资源清理完成..."
    if kubectl wait --for=delete namespace/"$NAMESPACE" --timeout=120s 2>/dev/null; then
        info "命名空间 '${NAMESPACE}' 已删除"
    else
        warn "命名空间删除超时，请手动检查: kubectl get namespace ${NAMESPACE}"
        warn "如果有资源卡在 Terminating 状态，可尝试: kubectl delete namespace ${NAMESPACE} --force --grace-period=0"
    fi
}

# ---- 输出状态 ----
print_result() {
    step "卸载完成"

    echo ""
    title "═══════════════════════════════════════════════════"
    title "  MCP HR Assistant — 已卸载"
    title "═══════════════════════════════════════════════════"
    echo ""

    # 确认命名空间已删除
    if kubectl get namespace "$NAMESPACE" &>/dev/null; then
        warn "命名空间 '${NAMESPACE}' 仍在删除中，请稍后检查"
    else
        info "命名空间 '${NAMESPACE}' 已不存在，所有资源已清理"
    fi

    echo ""
    info "如需重新部署，执行: ./deploy.sh"
    echo ""
}

# ---- 主流程 ----
main() {
    echo ""
    title "╔══════════════════════════════════════════════╗"
    title "║   MCP HR Assistant — 一键卸载工具            ║"
    title "╚══════════════════════════════════════════════╝"
    echo ""

    check_prerequisites
    confirm
    do_uninstall
    print_result
}

main "$@"
