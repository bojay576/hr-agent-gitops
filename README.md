# MCP HR Assistant

智能 HR 助手项目，包含：

- `ai-gateway`：对外 HTTP 入口，连接 LLM 和 MCP 工具
- `mcp-hr-server`：MCP Server，将 HR MySQL 数据库暴露为工具
- `mysql`：示例 HR 数据库
- `ollama`：可选本地 LLM 后端，也可以改用 OpenAI 兼容外部 API

默认部署命名空间是 `mcp-services`。

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

## MCP 工具

- `read_schema`：获取数据库表结构
- `execute_query`：执行 SQL 查询

AI Gateway 会把这两个工具提供给支持 tool/function calling 的 LLM。

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

## 常用命令

```bash
kubectl get pods -n mcp-services
kubectl logs -n mcp-services deploy/ai-gateway
kubectl logs -n mcp-services deploy/mcp-hr-server
kubectl logs -n mcp-services deploy/mysql
```
