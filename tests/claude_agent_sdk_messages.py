#!/usr/bin/env -S uv run --locked --script
# /// script
# requires-python = ">=3.10"
# dependencies = [
#   "anyio==4.14.2",
#   "claude-agent-sdk==0.2.149",
# ]
# ///

"""通过 Claude Agent SDK 验证 MockLLM Anthropic Messages API。"""

import json
import math
import os
import sys
import tempfile
from pathlib import Path
from urllib.error import URLError
from urllib.parse import urlsplit
from urllib.request import ProxyHandler, Request, build_opener

import anyio
from claude_agent_sdk import (
    AssistantMessage,
    ClaudeAgentOptions,
    ResultMessage,
    TextBlock,
    query,
)


PROMPT = "Write me a haiku about Kagent"
EXPECTED_RESPONSE = (
    "Agents wake and run,\n"
    "Kubernetes hums below,\n"
    "Kagent finds its mind."
)
DEFAULT_BASE_URL = "http://127.0.0.1:50502"
DEFAULT_MODEL = "claude-sonnet-4-5"
REPO_ROOT = Path(__file__).resolve().parents[1]


def normalized_base_url() -> str:
    """返回不含 API 路径的 MockLLM 基础地址。"""

    base_url = os.environ.get("MOCKLLM_BASE_URL", DEFAULT_BASE_URL).rstrip("/")
    parsed = urlsplit(base_url)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.netloc
        or parsed.username is not None
        or parsed.password is not None
        or parsed.path not in {"", "/"}
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError(
            "MOCKLLM_BASE_URL 必须是不含 /v1 或 /v1/messages 路径的 HTTP(S) 基础地址"
        )
    return base_url


def positive_timeout_seconds() -> float:
    """读取并校验整个 SDK 调用的超时时间。"""

    raw_value = os.environ.get("MOCKLLM_TEST_TIMEOUT_SECONDS", "60")
    try:
        timeout = float(raw_value)
    except ValueError as exc:
        raise ValueError("MOCKLLM_TEST_TIMEOUT_SECONDS 必须是正数") from exc
    if not math.isfinite(timeout) or timeout <= 0:
        raise ValueError("MOCKLLM_TEST_TIMEOUT_SECONDS 必须是正数")
    return timeout


def check_mockllm_health(base_url: str) -> None:
    """在启动 SDK 子进程前确认服务和 Anthropic mock 已加载。"""

    request = Request(f"{base_url}/health", headers={"Accept": "application/json"})
    try:
        # 本地和局域网测试不应被宿主机 HTTP(S)_PROXY 干扰。
        with build_opener(ProxyHandler({})).open(request, timeout=5) as response:
            health = json.load(response)
    except (OSError, URLError, json.JSONDecodeError) as exc:
        raise RuntimeError(
            f"无法访问 MockLLM 健康接口 {base_url}/health：{exc}"
        ) from exc

    if health.get("status") != "healthy":
        raise RuntimeError(f"MockLLM 当前不健康：{health!r}")
    if not isinstance(health.get("anthropic"), int) or health["anthropic"] < 1:
        raise RuntimeError(
            "MockLLM 尚未加载 Anthropic mock，请先执行 ./docker/start.sh"
        )


async def call_claude_agent_sdk(
    base_url: str, api_key: str, model: str
) -> tuple[str, ResultMessage]:
    """执行一次无工具的 Agent SDK query 并返回最终文本。"""

    # setting_sources=[] 不会隔离 ~/.claude.json；使用临时配置目录
    # 防止宿主 Claude 登录、记忆和 provider 选择污染测试。
    with tempfile.TemporaryDirectory(prefix="mockllm-claude-agent-sdk-") as config_dir:
        options = ClaudeAgentOptions(
            model=model,
            max_turns=1,
            tools=[],
            skills=[],
            system_prompt="Answer the user's request directly and concisely.",
            setting_sources=[],
            mcp_servers={},
            strict_mcp_config=True,
            permission_mode="dontAsk",
            cwd=REPO_ROOT,
            extra_args={"no-session-persistence": None},
            env={
                "ANTHROPIC_BASE_URL": base_url,
                "ANTHROPIC_API_KEY": api_key,
                "ANTHROPIC_AUTH_TOKEN": "",
                "CLAUDE_CODE_OAUTH_TOKEN": "",
                "CLAUDE_CONFIG_DIR": config_dir,
                "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1",
                "ENABLE_CLAUDEAI_MCP_SERVERS": "false",
                "CLAUDE_CODE_USE_BEDROCK": "",
                "CLAUDE_CODE_USE_VERTEX": "",
                "CLAUDE_CODE_USE_FOUNDRY": "",
                "CLAUDE_CODE_USE_MANTLE": "",
                "CLAUDE_CODE_USE_ANTHROPIC_AWS": "",
                "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST": "",
                "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
                "CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
                "CLAUDE_CODE_MAX_RETRIES": "0",
                "API_TIMEOUT_MS": "30000",
                # 该脚本专用于本机/局域网 MockLLM，禁止宿主代理改写路由。
                "HTTP_PROXY": "",
                "HTTPS_PROXY": "",
                "ALL_PROXY": "",
                "http_proxy": "",
                "https_proxy": "",
                "all_proxy": "",
                "NO_PROXY": "*",
                "no_proxy": "*",
            },
        )

        output_parts: list[str] = []
        final_result: ResultMessage | None = None
        async for message in query(prompt=PROMPT, options=options):
            if isinstance(message, AssistantMessage):
                if message.error is not None:
                    raise RuntimeError(
                        f"Claude Agent SDK assistant error: {message.error}"
                    )
                output_parts.extend(
                    block.text
                    for block in message.content
                    if isinstance(block, TextBlock)
                )
            elif isinstance(message, ResultMessage):
                final_result = message

    if final_result is None:
        raise RuntimeError("Claude Agent SDK 未返回 ResultMessage")
    if final_result.is_error or final_result.subtype != "success":
        raise RuntimeError(
            "Claude Agent SDK 调用失败："
            f"subtype={final_result.subtype!r}, "
            f"terminal_reason={final_result.terminal_reason!r}, "
            f"errors={final_result.errors!r}"
        )

    actual_response = "".join(output_parts)
    if actual_response != EXPECTED_RESPONSE:
        raise AssertionError(
            "AssistantMessage 响应不符合预期："
            f"expected={EXPECTED_RESPONSE!r}, actual={actual_response!r}"
        )
    if final_result.result != EXPECTED_RESPONSE:
        raise AssertionError(
            "ResultMessage.result 不符合预期："
            f"expected={EXPECTED_RESPONSE!r}, actual={final_result.result!r}"
        )

    return actual_response, final_result


async def run_test() -> None:
    """执行预检、SDK 调用和结果断言。"""

    base_url = normalized_base_url()
    check_mockllm_health(base_url)
    timeout = positive_timeout_seconds()
    api_key = os.environ.get("MOCKLLM_API_KEY", "mockllm-test-key")
    model = os.environ.get("MOCKLLM_ANTHROPIC_MODEL", DEFAULT_MODEL)
    if not api_key:
        raise ValueError("MOCKLLM_API_KEY 不得为空")
    if not model:
        raise ValueError("MOCKLLM_ANTHROPIC_MODEL 不得为空")

    # AnyIO cancel scope 会让 SDK 内部的 shielded close 完成子进程回收；
    # 不要改回 asyncio.wait_for，否则原始 cancellation 可打断 close()。
    with anyio.fail_after(timeout):
        actual_response, result = await call_claude_agent_sdk(
            base_url, api_key, model
        )

    print("Claude Agent SDK 响应：")
    print(actual_response)
    print(
        "PASS: Claude Agent SDK 已通过 MockLLM Anthropic Messages API "
        f"返回预期结果（session_id={result.session_id}）"
    )


def main() -> int:
    """运行测试并将失败转换为非零退出码。"""

    try:
        anyio.run(run_test)
    except KeyboardInterrupt:
        print("ERROR: 测试已中断", file=sys.stderr)
        return 130
    except TimeoutError:
        timeout = os.environ.get("MOCKLLM_TEST_TIMEOUT_SECONDS", "60")
        print(f"ERROR: Claude Agent SDK 调用超过 {timeout} 秒", file=sys.stderr)
        return 1
    except Exception as exc:  # noqa: BLE001 - CLI 测试需统一转换退出码。
        print(f"ERROR: {type(exc).__name__}: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
