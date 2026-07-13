# MCP HR Assistant

智能 HR 助手项目，包含：

- `ai-gateway`：对外 HTTP 入口，连接 LLM 和 MCP 工具（Go 语言 API 后端，无前端 UI）
- `mcp-hr-server`：MCP Server，将 HR MySQL 数据库暴露为工具
- `mysql`：示例 HR 数据库
- `ollama`：可选本地 LLM 后端，也可以改用 OpenAI 兼容外部 API

默认部署命名空间是 `mcp-services`。

## 目录

```text
apps/
  namespace.yaml
  mysql/
  mcp-agent/
  ollama/
src/
  ai-gateway/
  mcp-hr-server/
deploy.sh
```

## 部署

```bash
./deploy.sh
```

脚本会引导你选择：

- 本地 Ollama 模式
- 外部 OpenAI 兼容 API 模式

常用环境变量：

```bash
NAMESPACE=mcp-services
IMAGE_REGISTRY=ghcr.io/bojay576
IMAGE_TAG=latest
USE_LOCAL_IMAGES=false
MCP_IMAGE=ghcr.io/bojay576/mcp-hr-server:latest
GATEWAY_IMAGE=ghcr.io/bojay576/ai-gateway:latest
```

## 卸载 / 回退

部署脚本不包含卸载功能。如需移除所有资源，请手动执行：

```bash
# 删除整个命名空间（会删除所有相关资源）
NAMESPACE="${NAMESPACE:-mcp-services}"
kubectl delete namespace "$NAMESPACE"

# 如需保留命名空间仅删除单个资源：
# kubectl delete -n "$NAMESPACE" deploy/ai-gateway deploy/mcp-hr-server deploy/mysql deploy/ollama
# kubectl delete -n "$NAMESPACE" svc/ai-gateway-service svc/mcp-server-service svc/mysql-service svc/ollama-service
# kubectl delete -n "$NAMESPACE" pvc/mysql-pvc pvc/ollama-pvc
# kubectl delete -n "$NAMESPACE" secret/mysql-secret secret/mcp-server-secret secret/gateway-llm-secret
# kubectl delete -n "$NAMESPACE" configmap/mysql-init-scripts
```

如果需要保留数据（PVC 中的数据），删除命名空间前先备份：

```bash
NAMESPACE="${NAMESPACE:-mcp-services}"

# 1. 备份 MySQL 数据（可选）
kubectl exec -n "$NAMESPACE" deploy/mysql -- mysqldump -u root -p"$MYSQL_ROOT_PASSWORD" hr_db > hr_db_backup.sql

# 2. 删除命名空间（PVC 会随之删除）
kubectl delete namespace "$NAMESPACE"

# 3. 如需恢复：重新部署后导入备份
# kubectl exec -i -n "$NAMESPACE" deploy/mysql -- mysql -u root -p"$MYSQL_ROOT_PASSWORD" hr_db < hr_db_backup.sql
```

## 访问

Service 使用 NodePort `30080` 暴露 AI Gateway。

在 macOS minikube Docker driver 下，推荐使用端口转发：

```bash
kubectl port-forward -n mcp-services svc/ai-gateway-service 30080:3000
```

然后访问：

```bash
curl http://127.0.0.1:30080/healthz
```

聊天接口：

```bash
curl -s http://127.0.0.1:30080/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"查询所有员工和他们所在部门"}'
```

OpenAI 兼容接口：

```bash
curl -s http://127.0.0.1:30080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"Engineering 部门有哪些员工？"}]}'
```

> **关于前端界面**：本项目目前不包含独立的前端 UI 界面。`ai-gateway` 是 Go 语言编写的 API 后端网关，提供 RESTful 和 OpenAI 兼容的 HTTP 接口。
>
> 可以通过以下方式与此系统交互：
> - 直接调用 `/chat` 或 `/v1/chat/completions` API（如上示例）
> - 对接任意 OpenAI 兼容客户端（如 ChatBox、NextChat、Open WebUI 等）
> - 自行开发前端页面，调用上述 API 即可

## MCP 工具

- `read_schema`：获取数据库表结构
- `execute_query`：执行 SQL 查询

AI Gateway 会把这两个工具提供给支持 tool/function calling 的 LLM。

## 常用命令

```bash
kubectl get pods -n mcp-services
kubectl logs -n mcp-services deploy/ai-gateway
kubectl logs -n mcp-services deploy/mcp-hr-server
kubectl logs -n mcp-services deploy/mysql
```

## 常见问题

### MySQL 镜像拉取失败

如果遇到 `ImagePullBackOff`，可能是 Docker Hub 限制所致。可在 `apps/mysql/mysql-deployment.yaml` 中将镜像地址替换为国内可用的镜像源，例如：

```yaml
image: swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/mysql:8.0
```

### 健康检查

选择外部 API 模式时，部署脚本会自动验证 API 连通性。如果连接失败，脚本会提示是否继续部署。你也可以手动验证：

```bash
curl -s -H "Authorization: Bearer YOUR_API_KEY" https://your-api-endpoint/v1/models
```
