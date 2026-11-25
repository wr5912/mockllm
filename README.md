# Mock LLM Server

A simple mock LLM server for end-to-end testing. Provides request/response mocking for OpenAI and Anthropic APIs using their official SDK types.

## Features

- OpenAI Chat Completions API (streaming and non-streaming)
- OpenAI Responses API (streaming and non-streaming)
- OpenAI Embeddings API
- Anthropic Messages API (streaming and non-streaming)
- Exact and contains matching
- Optional header matching (e.g., tenant ID, API key, custom headers)
- Tool/function calls support
- JSON configuration files

## Architecture

- **Server**: HTTP server with Gorilla mux router
- **Providers**: Separate handlers for OpenAI and Anthropic
- **Matching**: Linear search through mocks with exact/contains matching
- **SDK Integration**: Uses official OpenAI and Anthropic SDK types directly

## Key Types

Current implementation uses these core types:

### Configuration

- `Config`: Root configuration containing arrays of OpenAI and Anthropic mocks
- `OpenAIMock`, `OpenAIResponseMock`, and `OpenAIEmbeddingMock` Maps OpenAI requests to responses using official SDK types
- `AnthropicMock`: Maps Anthropic requests to responses using official SDK types

### Matching

- `MatchType`: Enum for matching strategies (`exact`, `contains`)
- `HeaderMatch`: Defines a header matching rule (name, value, match type)
- `OpenAIRequestMatch` and `OpenAIResponseRequestMatch`: Defines how to match OpenAI requests (match type + message + optional headers)
- `AnthropicRequestMatch`: Defines how to match Anthropic requests (match type + message + optional headers, system substrings, and advertised tool names)

## API Coverage

### OpenAI Chat Completions

- **Endpoint**: `POST /v1/chat/completions`
- **Auth**: Not enforced (`Authorization: Bearer <token>` may be sent for client compatibility)
- **Request**: `openai.ChatCompletionNewParams`
- **Response**: `openai.ChatCompletion` (streaming: `openai.ChatCompletionChunk`)
- **Matching**: Exact or contains on last message

### OpenAI Responses API

- **Endpoint**: `POST /v1/responses`
- **Auth**: Not enforced (`Authorization: Bearer <token>` may be sent for client compatibility)
- **Request**: `responses.ResponseNewParams`
- **Response**: `responses.Response`
- **Matching**: Exact or contains on input field
- **Features**: Supports text output and function call outputs

### OpenAI Embeddings API

- **Endpoint**: `POST /v1/embeddings`
- **Auth**: Not enforced (`Authorization: Bearer <token>` may be sent for client compatibility)
- **Request**: `openai.EmbeddingNewParams`
- **Response**: `openai.CreateEmbeddingResponse`
- **Matching**: Exact or contains on input field

### Anthropic Messages API

- **Endpoint**: `POST /v1/messages`
- **Auth**: `x-api-key` (presence check only)
- **Headers**: `anthropic-version` required
- **Request**: `anthropic.MessageNewParams`
- **Response**: `anthropic.Message`
- **Matching**: Exact on the last message, or contains on the latest message with the configured role; optional request-level system-text and tool-name constraints

## Running as an Executable

You can run MockLLM directly from this repo:

```bash
go run ./cmd/mockllm --config ./testdata/example_config.json
```

Or build a standalone binary:

```bash
go build -o mockllm ./cmd/mockllm
./mockllm --config ./path/to/mocks.json --listen-addr 127.0.0.1:8080
```

The process prints its base URL to stdout once the server is healthy, then keeps serving until it receives `SIGINT` or `SIGTERM`.

Validate a configuration without starting the HTTP server:

```bash
./mockllm --config ./path/to/mocks.json --check-config
```

## Docker 部署（局域网 50502）

Docker 构建脚本位于 `images/`，部署文件和可提交的 mock 配置位于 `docker/`：

```text
images/
├── build.sh
└── mockllm.tar          # 生成物，不提交
docker/
├── Dockerfile
├── compose.yaml
├── config/
│   └── config.json
├── lib/
│   └── compose.sh
├── deploy.sh
├── start.sh
└── stop.sh
```

`docker/config/` 会以只读 bind mount 挂载到容器的 `/etc/mockllm`，因此重启或重建容器不会丢失配置。服务没有需要写入的持久化数据，所以不使用 `docker/volume/` 或 named volume。

### Compose 兼容策略

用户无需手动选择 Compose 命令。三个脚本会共用同一套检测逻辑：

1. 优先使用 `docker compose`。
2. 不可用时回退到独立的 `docker-compose`。
3. 仅支持 Compose v2 及更高版本；检测到真正的 Compose v1 时会明确报错并退出。

### 部署、启动与停止

脚本会根据自身路径定位 Compose 和配置文件，可从任意当前目录调用。以下示例在仓库根目录执行：

```bash
# 构建镜像并导出 images/mockllm.tar
./images/build.sh

# 校验并加载 images/mockllm.tar，然后创建或重建服务
./docker/deploy.sh

# 启动已停止的容器；已运行时执行重启，以重新加载 config.json
./docker/start.sh

# 优雅停止，保留容器、镜像和配置
./docker/stop.sh
```

- `images/build.sh` 使用 `docker/Dockerfile` 构建 `mockllm:local`，构建阶段执行 Go 测试；只有镜像完整导出后才会原子替换 `mockllm.tar`，构建或导出失败不会覆盖上一份完整归档。
- `deploy.sh` 检查 Docker、Compose 和镜像归档，核对归档的标签与内部 digest；执行 `docker load` 后先用加载的镜像解析 `config.json`，成功后才强制重建服务并等待健康状态；它不会现场构建镜像。
- `start.sh` 会先用现有镜像解析 `config.json`，再重启运行中的容器或启动已停止的容器；容器不存在时使用已有镜像创建，镜像也不存在时提示先运行 `deploy.sh`。
- `stop.sh` 使用 10 秒优雅退出窗口停止服务，不删除容器、镜像或配置。

默认构建使用 `https://proxy.golang.org`。网络无法访问该代理时，可在生成归档时显式覆盖：

```bash
GOPROXY='https://goproxy.cn,direct' ./images/build.sh
```

`GOPROXY` 只支持不含凭据的地址。脚本会拒绝 `https://user:password@proxy.example` 这类 URL，避免密钥通过 Docker build arg 进入日志或构建 provenance。

`config.json` 只在 MockLLM 进程启动时读取，不支持热加载。修改 [`docker/config/config.json`](docker/config/config.json) 后执行 `./docker/start.sh`；仅修改 Compose 定义时执行 `./docker/deploy.sh`；修改源码或 Dockerfile 后，先执行 `./images/build.sh`，再执行 `./docker/deploy.sh`。

镜像归档不包含 `docker/config/config.json`；部署时始终挂载仓库内的当前配置。归档是构建主机平台对应的 Docker 镜像，需要部署到兼容的 CPU/OS 平台。

Compose 将宿主机的 `0.0.0.0:50502` 映射到容器的 `50050`，同一局域网内的其他机器可通过 `http://<宿主机局域网 IP>:50502` 访问。

### curl 验证

先将示例 IP 替换为 Docker 宿主机的真实局域网 IP：

```bash
MOCKLLM_BASE_URL='http://192.168.1.20:50502'
```

健康检查：

```bash
curl --fail-with-body --silent --show-error \
  "$MOCKLLM_BASE_URL/health"
```

OpenAI Chat Completions：

```bash
curl --fail-with-body --silent --show-error \
  "$MOCKLLM_BASE_URL/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer test-key' \
  --data-binary '{
    "model": "gpt-4o-mini",
    "messages": [
      {"role": "user", "content": "Hello"}
    ]
  }'
```

OpenAI Responses API 流式 SSE：

```bash
curl --fail-with-body --no-buffer --include \
  "$MOCKLLM_BASE_URL/v1/responses" \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer test-key' \
  --data-binary '{
    "model": "gpt-4",
    "input": "Write me a haiku about Kagent",
    "stream": true
  }'
```

流式输出应包含 `event: response.created`、`event: response.output_text.delta`、`event: response.completed` 和最后的 `data: [DONE]`。

### Claude Agent SDK 验证 Anthropic Messages API

[`tests/claude_agent_sdk_messages.py`](tests/claude_agent_sdk_messages.py) 使用真实的 Python Claude Agent SDK `query()` 调用 MockLLM。脚本固定输入 `Write me a haiku about Kagent`，禁用工具、MCP、auto memory 和宿主代理，并为每次运行创建临时 Claude 配置目录，避免本机登录与 settings 污染请求。测试会同时断言 `AssistantMessage` 与 `ResultMessage` 中的完整 haiku。

本机需要安装 `uv`。首次执行会在 uv 隔离环境中下载脚本锁定的 `claude-agent-sdk==0.2.149` 及其内置 Claude Code CLI，不会写入系统 Python 环境。旁边的 `tests/claude_agent_sdk_messages.py.lock` 固定了传递依赖，脚本使用 `--locked` 防止测试时静默更新锁文件：

```bash
./tests/claude_agent_sdk_messages.py
```

修改脚本顶部的 PEP 723 依赖声明后，使用以下命令显式更新锁文件：

```bash
uv lock --script tests/claude_agent_sdk_messages.py
```

默认访问 `http://127.0.0.1:50502`。要从局域网其他主机执行，可覆盖基础地址（不要附加 `/v1` 或 `/v1/messages`）：

```bash
MOCKLLM_BASE_URL='http://192.168.1.20:50502' \
  ./tests/claude_agent_sdk_messages.py
```

如果健康接口中 `anthropic` 为 `0`，说明容器尚未重新加载 [`docker/config/config.json`](docker/config/config.json)，请先执行 `./docker/start.sh`。

Claude Code 的主机 managed policy 始终会加载，SDK 无法从进程内禁用。如果企业策略强制了其他 provider 或网络设置，请在不受该策略影响的测试主机或容器中执行。

### 日志与故障恢复

当前环境使用 `docker compose` 查看实时日志：

```bash
docker compose \
  --project-name mockllm \
  --project-directory docker \
  --file docker/compose.yaml \
  logs --tail 100 --follow mockllm
```

如果主机只安装了独立的 Compose v2，将命令开头的 `docker compose` 替换为 `docker-compose`。部署、启动和停止操作仍应使用上述三个脚本。

- `deploy.sh` 在归档缺失、manifest/digest 校验失败、加载失败或配置解析失败时会返回非零状态，不重建正式服务；健康状态超时则输出服务日志。
- `start.sh` 如果配置解析失败或等待健康状态超时，会返回非零状态并输出诊断信息。
- 如果无效的 `config.json` 导致容器重启或健康检查失败，执行 `./docker/stop.sh`，修复配置后再执行 `./docker/start.sh`。
- 如果 `50502` 端口已被占用，先停止占用者，再执行 `./docker/deploy.sh`。

> [!WARNING]
> MockLLM 不提供真实身份认证：OpenAI 端点不校验 `Authorization`，Anthropic 端点只检查 `x-api-key` 和 `anthropic-version` 是否存在。仅应将 `50502` 暴露给可信局域网，不要直接暴露到公网。`docker/config/config.json` 会纳入版本控制，请勿在其中保存真实 API key、token 或其他敏感数据。

## Configuration

### Config Go Structs

```go
config := mockllm.Config{
    OpenAI: []mockllm.OpenAIMock{
        {
            Name: "simple-response",
            Match: mockllm.OpenAIRequestMatch{
                MatchType: mockllm.MatchTypeExact,
                Message: /* openai.ChatCompletionMessageParamUnion */,
            },
            Response: /* openai.ChatCompletion */,
        },
    },
    OpenAIResponse: []mockllm.OpenAIResponseMock{
        {
            Name: "haiku-response",
            Match: mockllm.OpenAIResponseRequestMatch{
                MatchType: mockllm.MatchTypeContains,
                Input: /* responses.ResponseNewParamsInputUnion */,
            },
            Response: /* responses.Response */,
        },
    },
    OpenAIEmbeddings: []mockllm.OpenAIEmbeddingMock{
        {
            Name: "embedding-response",
            Match: mockllm.OpenAIEmbeddingRequestMatch{
                MatchType: mockllm.MatchTypeExact,
                Input: /* openai.EmbeddingNewParamsInputUnion */,
            },
            Response: /* openai.CreateEmbeddingResponse */,
        },
    },
    Anthropic: []mockllm.AnthropicMock{/* ... */},
}
```

### Config JSON Files

```json
{
  "openai": [
    {
      "name": "initial_request",
      "match": {
        "match_type": "exact",
        "message" : {
          "content": "List all nodes in the cluster",
          "role": "user"
        }
      },
      "response": {
        "id": "chatcmpl-1",
        "object": "chat.completion",
        "created": 1677652288,
        "model": "gpt-4.1-mini",
        "choices": [
          {
            "index": 0,
            "role": "assistant",
            "message": {
              "content": "",
              "tool_calls": [
                ...
              ]
            },
            "finish_reason": "tool_calls"
          }
        ]
      }
    },
    {
      "name": "k8s_get_resources_response",
      "match": {
        "match_type": "contains",
        "message" : {
          "content": "kagent-control-plane",
          "role": "tool",
          "tool_call_id": "call_1"
        }
      },
      "response": {
        "id": "call_1",
        "object": "chat.completion.tool_message",
        "created": 1677652288,
        "model": "gpt-4.1-mini",
        "choices": [
          ...
        ]
      }
    }
  ],
  "openai_response": [
    /* ... */
  ],
  "openai_embeddings": [
    /* ... */
  ],
  "anthropic": [
    /* ... */
  ]
}
```

### Generating Embeddings

When configuring mocks, you need to specify the embedding vectors for the input strings. You can do so using the OpenAI embeddings API (with your API key) or use a local option like `ollama` (e.g. `ollama run embeddinggemma "Hello world"`). MockLLM works with embeddings of any dimension, so you can use any embedding model you like.

### Header Matching

Mocks can optionally require specific HTTP headers to match. When `headers` is specified, all header rules must match (AND semantics) in addition to the body match. Header matching is optional — mocks without `headers` continue to work identically.

#### Go Header Structs

```go
mock := mockllm.OpenAIMock{
    Name: "tenant-a-response",
    Match: mockllm.OpenAIRequestMatch{
        MatchType: mockllm.MatchTypeContains,
        Message:   /* ... */,
        Headers: []mockllm.HeaderMatch{
            {Name: "X-Tenant-ID", Value: "tenant-a", MatchType: mockllm.MatchTypeExact},
        },
    },
    Response: /* ... */,
}
```

#### JSON Header Objects

```json
{
  "name": "tenant-a-response",
  "match": {
    "match_type": "contains",
    "message": { "role": "user", "content": "Hello" },
    "headers": [
      { "name": "X-Tenant-ID", "value": "tenant-a", "match_type": "exact" },
      { "name": "Authorization", "value": "Bearer", "match_type": "contains" }
    ]
  },
  "response": {}
}
```

- `name`: Header name (case-insensitive, per HTTP spec)
- `value`: Value to match against
- `match_type`: `"exact"` (default if omitted) or `"contains"`

### Anthropic Request Fields

Anthropic mocks can additionally require text in the top-level system prompt and
exact names in the advertised tool list. Every configured value must be present,
and these constraints are combined with the message and header match:

```json
{
  "match_type": "contains",
  "message": {
    "role": "user",
    "content": [{"type": "text", "text": "add 3 and 5"}]
  },
  "system_contains": ["Use the arithmetic skill"],
  "tool_names": ["mcp__calculator__add_numbers"]
}
```

### Matching Algorithm

Simple linear search through mocks:

1. Parse incoming request into appropriate SDK type
2. Iterate through provider-specific mocks in order
3. For each mock, check if the match criteria are met:
   - **Body**: Exact JSON comparison, or provider-specific contains matching on message/input
   - **Headers** (optional): All specified header rules must match
4. Return the response from the first matching mock
5. Return 404 if no match found

## Response Types

- **Non-streaming**: JSON responses using SDK types
- **Streaming**: Server-Sent Events (SSE) for Chat Completions, Responses, and Anthropic Messages APIs
- Uses official SDK response types directly

## Usage

```go
config := mockllm.Config{/* mocks */}
server := mockllm.NewServer(config)
baseURL, err := server.Start(context.Background())
defer server.Stop(context.Background())

// Use baseURL for API calls in tests
client := openai.NewClient(
    option.WithBaseURL(baseURL+"/v1/"),
    option.WithAPIKey("test-key"),
)
```

## Project Structure

- `server.go` — HTTP server, routing, lifecycle
- `types.go` — Configuration types
- `headers.go` — Shared header matching logic
- `openai.go` — OpenAI handler (Chat Completions)
- `openai_response.go` — OpenAI handler (Responses API)
- `openai_embeddings.go` — OpenAI handler (Embeddings API)
- `anthropic.go` — Anthropic handler
- `server_test.go` — Integration tests
- `testdata/` — Test fixtures

## Dependencies

- `github.com/openai/openai-go/v3`
- `github.com/anthropics/anthropic-sdk-go`
- `github.com/gorilla/mux`

## Limitations

- Simple matching only (exact/contains on message/input and optional headers)
- **Does not mock hosted tools (e.g. OpenAI file search, code execution) calls, reasoning, and MCP calls**
- No stateful conversation tracking
- No latency simulation
- No error injection
