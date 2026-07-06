#!/usr/bin/env python3
"""
Agentic AI Step Executor

Runs inside a Kubernetes Job pod. Implements the ReAct tool-use loop:
1. Calls the model API with system prompt + step input
2. If model requests a tool call, dispatches to MCP server
3. Feeds tool result back to model
4. Repeats until final text response or max iterations
5. Writes OUTPUT:{json} to stdout

Environment variables:
  MODEL_PROVIDER      - openai or anthropic (default: anthropic)
  MODEL_NAME          - model identifier (default: claude-sonnet-4-6)
  MODEL_API_KEY       - API key for the model provider
  MODEL_TEMPERATURE   - sampling temperature (default: 0.2)
  AGENT_SYSTEM_PROMPT - system prompt for the agent
  STEP_INPUT          - JSON input for this step
  STEP_NAME           - name of this workflow step
  WORKFLOW_NAME       - name of the parent workflow
  AGENT_TOOLS         - JSON-encoded tool specs from the agent CR
  MCP_SERVERS         - JSON array of MCP server configs (dynamic, from registry)
  MCP_SERVER_URL      - Legacy: single MCP server URL (fallback)
  GITHUB_MCP_URL      - Legacy: GitHub MCP server URL (fallback)
  GITHUB_MCP_TOKEN    - Legacy: GitHub MCP auth token (fallback)
  MAX_TOOL_CALLS      - max tool invocations (default: 20)
"""

import os
import sys
import json
import time
import logging
import requests

logging.basicConfig(level=logging.DEBUG, format='%(asctime)s %(levelname)s %(message)s')
logger = logging.getLogger('executor')

# ── Config ────────────────────────────────────────────────────────────

def env_int(name, default):
    """Integer env var with fallback on missing or invalid values."""
    try:
        return int(os.environ.get(name, ''))
    except (TypeError, ValueError):
        return default


MODEL_PROVIDER = os.environ.get('MODEL_PROVIDER', 'anthropic')
MODEL_NAME = os.environ.get('MODEL_NAME', 'claude-sonnet-4-6')
MODEL_API_KEY = os.environ.get('MODEL_API_KEY', '')
MODEL_TEMPERATURE = float(os.environ.get('MODEL_TEMPERATURE', '0.2'))
MODEL_MAX_TOKENS = env_int('MODEL_MAX_TOKENS', 4096)
MODEL_TIMEOUT = env_int('MODEL_TIMEOUT', 120)
VERTEX_PROJECT_ID = os.environ.get('ANTHROPIC_VERTEX_PROJECT_ID', '')
VERTEX_REGION = os.environ.get('CLOUD_ML_REGION', 'us-east5')
USE_VERTEX = bool(VERTEX_PROJECT_ID)
MODEL_ENDPOINT = os.environ.get('MODEL_ENDPOINT', '').rstrip('/')
MODEL_API_FORMAT = os.environ.get('MODEL_API_FORMAT', '')
if not MODEL_API_FORMAT:
    if MODEL_PROVIDER in ('anthropic', 'vertex-ai'):
        MODEL_API_FORMAT = 'anthropic'
    else:
        MODEL_API_FORMAT = 'openai'
SYSTEM_PROMPT = os.environ.get('AGENT_SYSTEM_PROMPT', 'You are a helpful assistant.')
STEP_INPUT = os.environ.get('STEP_INPUT', '{}')
STEP_NAME = os.environ.get('STEP_NAME', 'unknown')
WORKFLOW_NAME = os.environ.get('WORKFLOW_NAME', 'unknown')
AGENT_TOOLS_JSON = os.environ.get('AGENT_TOOLS', '[]')
MAX_TOOL_CALLS = int(os.environ.get('MAX_TOOL_CALLS', '20'))
AUTONOMY_LEVEL = os.environ.get('AUTONOMY_LEVEL', 'supervised')
COST_LIMIT_USD = float(os.environ.get('COST_LIMIT_USD', '0'))  # 0 = no limit
CONTENT_FILTERS_JSON = os.environ.get('CONTENT_FILTERS', '[]')
MEMORY_TYPE = os.environ.get('MEMORY_TYPE', 'buffer')  # buffer, summary, vector, none
MEMORY_CM_NAME = os.environ.get('MEMORY_CM_NAME', '')   # ConfigMap name for summary memory
MAX_CONTEXT_TOKENS = int(os.environ.get('MAX_CONTEXT_TOKENS', '8192'))  # max tokens to load from memory

# Dynamic MCP servers (from registry via job_builder)
MCP_SERVERS_JSON = os.environ.get('MCP_SERVERS', '[]')

# Legacy single-server env vars (backward compat)
MCP_SERVER_URL = os.environ.get('MCP_SERVER_URL', '')
GITHUB_MCP_URL = os.environ.get('GITHUB_MCP_URL', '')
GITHUB_MCP_TOKEN = os.environ.get('GITHUB_MCP_TOKEN', '')

# Tool-to-client routing map (populated during init)
tool_to_client = {}
all_mcp_clients = []


# ── MCP Client ────────────────────────────────────────────────────────

def mcp_endpoint(server_url):
    """Normalize an MCP server URL: base URLs get /mcp appended, full
    /mcp endpoints pass through (F21 — users paste either form)."""
    base = server_url.rstrip('/')
    return base if base.endswith('/mcp') else base + '/mcp'


class MCPClient:
    """Lightweight MCP JSON-RPC 2.0 client for streamable-http transport."""

    def __init__(self, server_url, server_name='unknown', auth_token=None):
        self.server_url = server_url.rstrip('/')
        self.server_name = server_name
        self.endpoint = mcp_endpoint(self.server_url)
        if '127.0.0.1' in self.endpoint:
            self.endpoint = self.endpoint.replace('127.0.0.1', 'localhost')
        self.session_id = None
        self.request_id = 0
        self.session = requests.Session()
        headers = {
            'Content-Type': 'application/json',
            'Accept': 'application/json, text/event-stream',
        }
        if auth_token:
            headers['Authorization'] = f'Bearer {auth_token}'
        self.session.headers.update(headers)

    def _next_id(self):
        self.request_id += 1
        return self.request_id

    def _send(self, method, params=None):
        """Send JSON-RPC request and parse SSE response."""
        payload = {
            'jsonrpc': '2.0',
            'method': method,
            'params': params or {},
            'id': self._next_id(),
        }
        headers = {}
        if self.session_id:
            headers['mcp-session-id'] = self.session_id

        try:
            resp = self.session.post(
                self.endpoint, json=payload, headers=headers,
                stream=True, timeout=15, allow_redirects=True,
            )
            if 'mcp-session-id' in resp.headers:
                self.session_id = resp.headers['mcp-session-id']

            ct = resp.headers.get('content-type', '')
            logger.debug(f"MCP [{self.server_name}] status={resp.status_code} content-type={ct}")

            if 'application/json' in ct:
                data = resp.json()
                logger.debug(f"MCP [{self.server_name}] JSON: {str(data)[:200]}")
                return data

            # Parse SSE response
            for line in resp.iter_lines():
                if isinstance(line, bytes):
                    line = line.decode('utf-8')
                if line and line.startswith('data: '):
                    data = json.loads(line[6:])
                    return data
                if line and line.startswith('event:'):
                    continue
            return None
        except Exception as e:
            logger.error(f"MCP [{self.server_name}] request failed: {e}")
            return None

    def initialize(self):
        """Initialize MCP session."""
        result = self._send('initialize', {
            'protocolVersion': '2025-03-26',
            'capabilities': {},
            'clientInfo': {'name': 'purko-executor', 'version': '2.0'},
        })
        if result and 'result' in result:
            info = result['result'].get('serverInfo', {})
            logger.info(f"MCP [{self.server_name}] connected: {info.get('name', '?')} v{info.get('version', '?')}")
            return True
        return False

    def list_tools(self):
        """List available tools."""
        result = self._send('tools/list')
        if result and 'result' in result:
            return result['result'].get('tools', [])
        return []

    def call_tool(self, name, arguments):
        """Call an MCP tool and return the result."""
        result = self._send('tools/call', {
            'name': name,
            'arguments': arguments,
        })
        if result and 'result' in result:
            content = result['result'].get('content', [])
            texts = [c.get('text', '') for c in content if c.get('type') == 'text']
            return '\n'.join(texts)
        if result and 'error' in result:
            return f"ERROR: {result['error'].get('message', 'unknown error')}"
        return 'ERROR: no response from MCP server'


# ── MCP Server Connection ─────────────────────────────────────────────

def connect_mcp_servers():
    """Connect to all MCP servers and build the tool routing map."""
    global tool_to_client, all_mcp_clients

    mcp_tools = []
    clients = []

    # Try dynamic MCP_SERVERS first (from registry)
    try:
        servers = json.loads(MCP_SERVERS_JSON)
    except json.JSONDecodeError:
        servers = []

    if servers:
        logger.info(f"Connecting to {len(servers)} MCP servers from registry")
        for srv in servers:
            name = srv.get('name', 'unknown')
            url = srv.get('url', '')
            auth = srv.get('auth', 'none')
            token = srv.get('token', '') if auth == 'bearer' else None

            if not url:
                continue

            try:
                client = MCPClient(url, server_name=name, auth_token=token)
                if client.initialize():
                    tools = client.list_tools()
                    clients.append(client)
                    for t in tools:
                        tool_to_client[t['name']] = client
                    mcp_tools.extend(tools)
                    logger.info(f"MCP [{name}]: {len(tools)} tools")
                else:
                    logger.warning(f"MCP [{name}] initialization failed")
            except Exception as e:
                logger.warning(f"MCP [{name}] connection failed: {e}")
    else:
        # Legacy: fall back to individual env vars
        logger.info("No MCP_SERVERS env var, using legacy env vars")

        if MCP_SERVER_URL:
            try:
                client = MCPClient(MCP_SERVER_URL, server_name='lumino')
                if client.initialize():
                    tools = client.list_tools()
                    clients.append(client)
                    for t in tools:
                        tool_to_client[t['name']] = client
                    mcp_tools.extend(tools)
                    logger.info(f"Lumino MCP: {len(tools)} tools")
            except Exception as e:
                logger.warning(f"Lumino MCP failed: {e}")

        if GITHUB_MCP_URL and GITHUB_MCP_TOKEN:
            try:
                client = MCPClient(GITHUB_MCP_URL, server_name='github', auth_token=GITHUB_MCP_TOKEN)
                if client.initialize():
                    tools = client.list_tools()
                    clients.append(client)
                    for t in tools:
                        tool_to_client[t['name']] = client
                    mcp_tools.extend(tools)
                    logger.info(f"GitHub MCP: {len(tools)} tools")
            except Exception as e:
                logger.warning(f"GitHub MCP failed: {e}")

    all_mcp_clients = clients
    logger.info(f"Total MCP tools: {len(mcp_tools)}, tool routing entries: {len(tool_to_client)}")
    return mcp_tools


# ── Model Clients ─────────────────────────────────────────────────────

def call_anthropic(messages, system_prompt, tools=None):
    """Call Anthropic Claude API with tool support. Uses Vertex AI if configured."""
    if USE_VERTEX:
        return call_anthropic_vertex(messages, system_prompt, tools)

    headers = {
        'x-api-key': MODEL_API_KEY,
        'anthropic-version': '2023-06-01',
        'content-type': 'application/json',
    }
    body = {
        'model': MODEL_NAME,
        'max_tokens': MODEL_MAX_TOKENS,
        'temperature': MODEL_TEMPERATURE,
        'system': system_prompt,
        'messages': messages,
    }
    if tools:
        body['tools'] = tools

    url = MODEL_ENDPOINT + '/messages' if MODEL_ENDPOINT else 'https://api.anthropic.com/v1/messages'
    resp = requests.post(
        url,
        headers=headers, json=body, timeout=MODEL_TIMEOUT,
    )
    resp.raise_for_status()
    result = resp.json()
    # Track cost from usage info
    usage = result.get('usage', {})
    track_cost(usage.get('input_tokens', estimate_tokens(messages)),
               usage.get('output_tokens', estimate_tokens(result.get('content', []))))
    return result


def vertex_model_name(name):
    """Convert direct API model name to Vertex AI format."""
    mapping = {
        'claude-sonnet-4-20250514': 'claude-sonnet-4-6',
        'claude-opus-4-20250514': 'claude-opus-4-6',
        'claude-haiku-4-5-20251001': 'claude-haiku-4-5',
        'claude-sonnet-4-6': 'claude-sonnet-4-6',
        'claude-opus-4-6': 'claude-opus-4-6',
        'claude-haiku-4-5': 'claude-haiku-4-5',
    }
    return mapping.get(name, name)


_vertex_client = None

def get_vertex_client():
    global _vertex_client
    if _vertex_client is None:
        from anthropic import AnthropicVertex
        _vertex_client = AnthropicVertex(
            project_id=VERTEX_PROJECT_ID,
            region=VERTEX_REGION,
        )
    return _vertex_client


def call_anthropic_vertex(messages, system_prompt, tools=None):
    """Call Claude via Vertex AI using the Anthropic SDK."""
    client = get_vertex_client()
    model = vertex_model_name(MODEL_NAME)
    logger.info(f"Vertex AI model: {model}")

    kwargs = {
        'model': model,
        'max_tokens': MODEL_MAX_TOKENS,
        'temperature': MODEL_TEMPERATURE,
        'system': system_prompt,
        'messages': messages,
    }
    if tools:
        kwargs['tools'] = tools

    response = client.messages.create(**kwargs)
    # Track cost from Vertex AI usage
    track_cost(getattr(response.usage, 'input_tokens', 0),
               getattr(response.usage, 'output_tokens', 0))
    result = {'content': [], 'stop_reason': response.stop_reason}
    for b in response.content:
        if b.type == 'text':
            result['content'].append({'type': 'text', 'text': b.text})
        elif b.type == 'tool_use':
            result['content'].append({
                'type': 'tool_use', 'id': b.id,
                'name': b.name, 'input': dict(b.input) if b.input else {},
            })
    return result


def llm_mode():
    """LLM mode requires credentials OR a custom endpoint — keyless
    endpoints (Ollama, vLLM, gateways) are valid LLM backends."""
    return bool(MODEL_API_KEY or USE_VERTEX or MODEL_ENDPOINT)


def openai_headers():
    """Request headers for OpenAI-format APIs. The Authorization header is
    omitted without an API key — an empty 'Bearer ' makes strict gateways
    return 401, silently dropping the executor into demo mode."""
    headers = {'Content-Type': 'application/json'}
    if MODEL_API_KEY:
        headers['Authorization'] = f'Bearer {MODEL_API_KEY}'
    return headers


def call_openai(messages, system_prompt, tools=None):
    """Call OpenAI API with tool support."""
    headers = openai_headers()
    msgs = [{'role': 'system', 'content': system_prompt}] + messages
    body = {
        'model': MODEL_NAME,
        'max_tokens': MODEL_MAX_TOKENS,
        'temperature': MODEL_TEMPERATURE,
        'messages': msgs,
    }
    if tools:
        body['tools'] = [
            {'type': 'function', 'function': t} for t in tools
        ]

    url = MODEL_ENDPOINT + '/chat/completions' if MODEL_ENDPOINT else 'https://api.openai.com/v1/chat/completions'
    resp = requests.post(
        url,
        headers=headers, json=body, timeout=MODEL_TIMEOUT,
    )
    resp.raise_for_status()
    result = resp.json()
    track_cost(*openai_usage(result))
    return result


def openai_usage(result):
    """Token usage from an OpenAI-format response (prompt_tokens /
    completion_tokens), zeros when the endpoint reports none."""
    usage = result.get('usage') or {}
    return usage.get('prompt_tokens', 0), usage.get('completion_tokens', 0)


# ── Tool Routing ──────────────────────────────────────────────────────

def build_tool_definitions(agent_tools, mcp_tools):
    """Build tool definitions for the model API from agent specs + MCP tools."""
    definitions = []

    mcp_by_name = {t['name']: t for t in mcp_tools}
    for tool in agent_tools:
        tool_type = tool.get('type', 'mcp')

        if tool_type == 'mcp':
            config = tool.get('config', {})
            capability = config.get('capability', tool['name'])
            if capability in mcp_by_name:
                mcp_def = mcp_by_name[capability]
                definitions.append({
                    'name': capability,
                    'description': mcp_def.get('description', ''),
                    'input_schema': mcp_def.get('inputSchema', {'type': 'object', 'properties': {}}),
                })

        elif tool_type == 'function':
            # Function tools — from FUNCTION_TOOLS registry
            if tool['name'] in FUNCTION_TOOLS:
                ft = FUNCTION_TOOLS[tool['name']]
                definitions.append({
                    'name': ft['name'],
                    'description': ft['description'],
                    'input_schema': ft['input_schema'],
                })
            elif tool['name'] == 'code-sandbox' and CODE_EXECUTION_ENABLED:
                # code-sandbox is an alias for execute_code
                code_tool = get_code_execution_tool()
                if code_tool:
                    definitions.append(code_tool)

        elif tool_type == 'builtin':
            # Builtin tools — from BUILTIN_TOOLS registry
            if tool['name'] in BUILTIN_TOOLS:
                bt = BUILTIN_TOOLS[tool['name']]
                definitions.append({
                    'name': bt['name'],
                    'description': bt['description'],
                    'input_schema': bt['input_schema'],
                })

        elif tool_type == 'api':
            # API tools — direct HTTP endpoint calls
            endpoint = tool.get('endpoint', '')
            if isinstance(endpoint, dict):
                endpoint_desc = endpoint.get('url', '')
            else:
                endpoint_desc = str(endpoint)
            definitions.append({
                'name': tool['name'],
                'description': f"API endpoint: {endpoint_desc}",
                'input_schema': {'type': 'object', 'properties': {}, 'additionalProperties': True},
            })

    if not agent_tools:
        for t in mcp_tools:
            definitions.append({
                'name': t['name'],
                'description': t.get('description', ''),
                'input_schema': t.get('inputSchema', {'type': 'object', 'properties': {}}),
            })

    return definitions


def format_tools_for_anthropic(tool_defs):
    return [
        {'name': t['name'], 'description': t['description'], 'input_schema': t['input_schema']}
        for t in tool_defs
    ]


def format_tools_for_openai(tool_defs):
    return [
        {'name': t['name'], 'description': t['description'], 'parameters': t['input_schema']}
        for t in tool_defs
    ]


# ── Cost Tracking ─────────────────────────────────────────────────────

# Approximate pricing per token (USD)
TOKEN_PRICING = {
    'claude-sonnet-4-6': {'input': 3.0 / 1e6, 'output': 15.0 / 1e6},
    'claude-opus-4-6': {'input': 15.0 / 1e6, 'output': 75.0 / 1e6},
    'claude-haiku-4-5': {'input': 0.8 / 1e6, 'output': 4.0 / 1e6},
    'gpt-4o': {'input': 2.5 / 1e6, 'output': 10.0 / 1e6},
    'gpt-4o-mini': {'input': 0.15 / 1e6, 'output': 0.6 / 1e6},
}

total_cost_usd = 0.0
total_tokens_in = 0
total_tokens_out = 0


def model_pricing(model_name):
    """Resolve per-token pricing: MODEL_PRICE_IN/OUT env (USD per MToken,
    from the LLMProvider CR) wins; then the built-in table; unknown models
    cost $0 — a local/self-hosted model must never be billed at a made-up
    default rate."""
    env_in, env_out = os.environ.get('MODEL_PRICE_IN'), os.environ.get('MODEL_PRICE_OUT')
    if env_in and env_out:
        try:
            return {'input': float(env_in) / 1e6, 'output': float(env_out) / 1e6}
        except ValueError:
            logger.warning(f"Invalid MODEL_PRICE_IN/OUT ({env_in}/{env_out}), ignoring")
    return TOKEN_PRICING.get(model_name, {'input': 0.0, 'output': 0.0})


def track_cost(input_tokens, output_tokens):
    """Track cumulative cost. Raises if cost limit exceeded."""
    global total_cost_usd, total_tokens_in, total_tokens_out
    pricing = model_pricing(MODEL_NAME)
    cost = input_tokens * pricing['input'] + output_tokens * pricing['output']
    total_cost_usd += cost
    total_tokens_in += input_tokens
    total_tokens_out += output_tokens
    logger.info(f"Cost: ${total_cost_usd:.4f} (+${cost:.4f}), tokens: {total_tokens_in}in/{total_tokens_out}out")
    if COST_LIMIT_USD > 0 and total_cost_usd > COST_LIMIT_USD:
        raise Exception(f"Cost limit exceeded: ${total_cost_usd:.4f} > ${COST_LIMIT_USD:.2f}")


def estimate_tokens(text):
    """Rough token estimate — ~4 chars per token for English."""
    return max(1, len(str(text)) // 4)


# ── Content Filtering ─────────────────────────────────────────────────

def apply_content_filters(text):
    """Apply content filters to LLM output. Returns filtered text."""
    try:
        filters = json.loads(CONTENT_FILTERS_JSON)
    except json.JSONDecodeError:
        return text

    import re
    for f in filters:
        if f == 'no-secrets-in-output':
            # Redact common secret patterns
            text = re.sub(r'(?i)(api[_-]?key|token|password|secret)["\s:=]+["\']?[\w\-\.]{8,}["\']?', r'\1=***REDACTED***', text)
        elif f == 'no-pii':
            # Redact SSN, email patterns
            text = re.sub(r'\b\d{3}-\d{2}-\d{4}\b', '***SSN***', text)
            text = re.sub(r'\b[\w.+-]+@[\w-]+\.[\w.-]+\b', '***EMAIL***', text)
        elif f.startswith('regex:'):
            # Custom regex filter: "regex:pattern:replacement"
            parts = f.split(':', 2)
            if len(parts) == 3:
                text = re.sub(parts[1], parts[2], text)
    return text


# ── Agent Memory ──────────────────────────────────────────────────────

def load_memory_context():
    """Load previous execution context based on memory type."""
    if MEMORY_TYPE == 'none' or MEMORY_TYPE == 'buffer':
        return ''

    if MEMORY_TYPE == 'summary':
        # Summary memory is passed via AGENT_MEMORY env var (from ConfigMap)
        memory = os.environ.get('AGENT_MEMORY', '')
        if memory:
            logger.info(f"Loaded summary memory: {len(memory)} chars")
            return f"\n\n[Previous execution context]\n{memory}\n\n[Current task follows]"
        return ''

    if MEMORY_TYPE == 'vector':
        # Vector memory — load from PVC at /var/run/agent-memory
        memory_path = '/var/run/agent-memory'
        if not os.path.exists(memory_path):
            logger.info("No vector memory directory found (PVC not mounted?)")
            return ''
        try:
            # Read memory files, newest first, up to MAX_CONTEXT_TOKENS (~4 chars/token)
            max_chars = MAX_CONTEXT_TOKENS * 4
            files = sorted(os.listdir(memory_path), reverse=True)
            context_parts = []
            total_chars = 0
            for fname in files:
                fpath = os.path.join(memory_path, fname)
                if os.path.isfile(fpath) and fname.endswith('.txt'):
                    with open(fpath) as f:
                        content = f.read()
                    if total_chars + len(content) > max_chars:
                        # Truncate to fit
                        remaining = max_chars - total_chars
                        if remaining > 100:
                            context_parts.append(content[:remaining])
                        break
                    context_parts.append(content)
                    total_chars += len(content)
            if context_parts:
                # Reverse back to chronological order
                context_parts.reverse()
                logger.info(f"Loaded vector memory: {len(context_parts)} entries, {total_chars} chars (max {max_chars})")
                return "\n\n[Previous execution context]\n" + "\n---\n".join(context_parts) + "\n\n[Current task follows]"
        except Exception as e:
            logger.warning(f"Failed to load vector memory: {e}")
        return ''

    return ''


def save_memory(conversation, output):
    """Save execution context for future use. Returns memory_update for OUTPUT."""
    if MEMORY_TYPE == 'none' or MEMORY_TYPE == 'buffer':
        return None

    if MEMORY_TYPE == 'summary':
        # Build a summary of this execution
        task = STEP_INPUT[:200] if STEP_INPUT else 'unknown'
        result_summary = ''
        if isinstance(output, dict):
            if 'response' in output:
                result_summary = str(output['response'])[:500]
            elif 'status' in output:
                result_summary = f"Status: {output['status']}"
        summary = f"[{WORKFLOW_NAME}/{STEP_NAME}] Task: {task} | Result: {result_summary}"
        return summary

    if MEMORY_TYPE == 'vector':
        # Write to PVC-mounted directory
        memory_path = '/var/run/agent-memory'
        try:
            os.makedirs(memory_path, exist_ok=True)
            import time as _time
            fname = f"{int(_time.time())}_{STEP_NAME}.txt"
            task = STEP_INPUT[:200] if STEP_INPUT else 'unknown'
            result_summary = json.dumps(output, default=str)[:2000] if output else ''
            with open(os.path.join(memory_path, fname), 'w') as f:
                f.write(f"Task: {task}\nResult: {result_summary}")
            logger.info(f"Saved vector memory: {fname}")
        except Exception as e:
            logger.warning(f"Failed to save vector memory: {e}")
        return None

    return None


# ── Autonomy Enforcement ──────────────────────────────────────────────

# Tools that mutate state — blocked under 'restricted' autonomy
WRITE_TOOLS = {
    # GitHub write operations
    'push_files', 'create_or_update_file', 'delete_file',
    'create_pull_request', 'merge_pull_request', 'update_pull_request',
    'create_branch', 'create_repository', 'fork_repository',
    'update_pull_request_branch', 'pull_request_review_write',
    'add_comment_to_pending_review', 'add_reply_to_pull_request_comment',
    'issue_write', 'sub_issue_write', 'add_issue_comment',
    'assign_copilot_to_issue', 'request_copilot_review',
    # Code execution (blocked under restricted/shu)
    'execute_code', 'code-sandbox',
}


def filter_tools_by_autonomy(tool_defs):
    """Filter tool definitions based on autonomy level before passing to LLM."""
    if AUTONOMY_LEVEL == 'manual':
        logger.info("Autonomy: manual — all tools disabled, analysis-only mode")
        return []
    if AUTONOMY_LEVEL == 'restricted':
        # Read-only tools only — block write tools AND code execution
        filtered = [t for t in tool_defs if t['name'] not in WRITE_TOOLS and t['name'] != 'execute_code']
        blocked = len(tool_defs) - len(filtered)
        if blocked > 0:
            logger.info(f"Autonomy: restricted — blocked {blocked} write/code tools")
        return filtered
    # supervised, full — all tools allowed
    return tool_defs


# ── CodeAct — Code Execution Tool ────────────────────────────────────

CODE_EXECUTION_ENABLED = os.environ.get('CODE_EXECUTION_ENABLED', 'false') == 'true'
CODE_LANGUAGES = os.environ.get('CODE_LANGUAGES', 'python,bash').split(',')
CODE_SANDBOX = json.loads(os.environ.get('CODE_SANDBOX', '{}'))


def get_code_execution_tool():
    """Returns the execute_code tool definition if enabled."""
    if not CODE_EXECUTION_ENABLED:
        return None
    return {
        'name': 'execute_code',
        'description': (
            'Execute Python or bash code and return the output. '
            'Use for data processing, calculations, formatting, analysis, '
            'and any task not covered by other tools. '
            'Print results to stdout.'
        ),
        'input_schema': {
            'type': 'object',
            'properties': {
                'code': {
                    'type': 'string',
                    'description': 'Code to execute'
                },
                'language': {
                    'type': 'string',
                    'enum': CODE_LANGUAGES,
                    'default': 'python',
                    'description': 'Programming language'
                }
            },
            'required': ['code']
        }
    }


def execute_code(code, language='python'):
    """Execute code in a sandboxed subprocess."""
    import subprocess
    import tempfile

    timeout = CODE_SANDBOX.get('maxExecutionSeconds', 30)
    max_output = CODE_SANDBOX.get('maxOutputBytes', 100000)

    logger.info(f"CODEACT: executing {language} ({len(code)} chars)")
    for i, line in enumerate(code.split('\n')[:20], 1):
        logger.info(f"  code[{i}]: {line}")
    if code.count('\n') > 20:
        logger.info(f"  ... ({code.count(chr(10)) - 20} more lines)")

    try:
        if language == 'python':
            with tempfile.NamedTemporaryFile(mode='w', suffix='.py', dir='/tmp', delete=False) as f:
                f.write(code)
                f.flush()
                result = subprocess.run(
                    ['python', f.name],
                    capture_output=True, text=True,
                    timeout=timeout, cwd='/tmp',
                    env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'},
                )
                os.unlink(f.name)
        elif language == 'bash':
            result = subprocess.run(
                ['bash', '-c', code],
                capture_output=True, text=True,
                timeout=timeout, cwd='/tmp',
            )
        else:
            return f"ERROR: Unsupported language: {language}"

        output = result.stdout[:max_output]
        if result.returncode != 0:
            output += f"\nEXIT CODE: {result.returncode}"
        if result.stderr:
            output += f"\nSTDERR:\n{result.stderr[:max_output // 2]}"

        logger.info(f"CODEACT: completed, {len(output)} chars output, exit={result.returncode}")
        return output if output else "(no output)"

    except subprocess.TimeoutExpired:
        logger.warning(f"CODEACT: timed out after {timeout}s")
        return f"ERROR: Execution timed out after {timeout}s"
    except Exception as e:
        logger.error(f"CODEACT: error: {e}")
        return f"ERROR: {e}"


# ── Function Tool Registry ───────────────────────────────────────────
# function tools are in-process tools with configRef support (distinct from builtin)

FUNCTION_TOOLS = {}


def register_function_tool(name, description, input_schema, handler):
    FUNCTION_TOOLS[name] = {
        'name': name, 'description': description,
        'input_schema': input_schema, 'handler': handler,
    }


# Register execute_code as a function tool
register_function_tool('execute_code',
    'Execute Python or bash code in a sandbox',
    {'type': 'object', 'properties': {'code': {'type': 'string'}, 'language': {'type': 'string', 'enum': ['python', 'bash']}}, 'required': ['code']},
    lambda args: execute_code(args.get('code', ''), args.get('language', 'python')))

# Register vector-search as a function tool
def _vector_search(args):
    """Search agent memory using keyword matching."""
    query = args.get('query', '')
    memory_path = '/var/run/agent-memory'
    max_results = args.get('maxResults', 5)
    if not os.path.exists(memory_path):
        return json.dumps({'results': [], 'message': 'No memory available'})
    entries = []
    for fname in sorted(os.listdir(memory_path)):
        fpath = os.path.join(memory_path, fname)
        if os.path.isfile(fpath) and fname.endswith('.txt'):
            with open(fpath) as f:
                entries.append({'file': fname, 'content': f.read()})
    query_terms = query.lower().split()
    scored = []
    for entry in entries:
        content_lower = entry['content'].lower()
        score = sum(1 for term in query_terms if term in content_lower)
        if score > 0:
            scored.append({'file': entry['file'], 'score': score, 'preview': entry['content'][:500]})
    scored.sort(key=lambda x: x['score'], reverse=True)
    return json.dumps({'results': scored[:max_results], 'totalEntries': len(entries), 'matched': len(scored)})

register_function_tool('vector-search',
    'Search agent memory for relevant previous findings using keyword matching',
    {'type': 'object', 'properties': {'query': {'type': 'string'}, 'maxResults': {'type': 'integer'}}, 'required': ['query']},
    _vector_search)


# ── Builtin Tool Registry ────────────────────────────────────────────
# builtin tools are static capabilities compiled into the executor (no configRef)

BUILTIN_TOOLS = {}


def register_builtin_tool(name, description, input_schema, handler):
    BUILTIN_TOOLS[name] = {
        'name': name, 'description': description,
        'input_schema': input_schema, 'handler': handler,
    }


def _static_analysis(args):
    """Analyze code for common issues."""
    import re as _re
    code = args.get('code', '')
    language = args.get('language', 'python')
    findings = []
    if language == 'python':
        if 'eval(' in code: findings.append({'severity': 'HIGH', 'issue': 'eval() usage — code injection risk'})
        if 'exec(' in code: findings.append({'severity': 'HIGH', 'issue': 'exec() usage — code injection risk'})
        if 'import os' in code and 'system(' in code: findings.append({'severity': 'MEDIUM', 'issue': 'os.system() — use subprocess instead'})
        if _re.search(r'except\s*:', code): findings.append({'severity': 'LOW', 'issue': 'bare except — catch specific exceptions'})
        if _re.search(r'(?i)(password|api.?key|secret)\s*=\s*["\'][^"\']+["\']', code): findings.append({'severity': 'HIGH', 'issue': 'possible hardcoded secret'})
        if 'import pickle' in code: findings.append({'severity': 'MEDIUM', 'issue': 'pickle usage — deserialization risk'})
        if '# TODO' in code or '# FIXME' in code: findings.append({'severity': 'LOW', 'issue': 'unresolved TODO/FIXME'})
    elif language in ('go', 'golang'):
        if 'fmt.Sprintf' in code and 'sql' in code.lower(): findings.append({'severity': 'HIGH', 'issue': 'possible SQL injection via string formatting'})
        if 'os.Exit' in code: findings.append({'severity': 'LOW', 'issue': 'os.Exit — consider returning errors instead'})
    return json.dumps({'findings': findings, 'total': len(findings), 'language': language})

register_builtin_tool('static-analysis',
    'Analyze code for security issues, anti-patterns, and quality problems',
    {'type': 'object', 'properties': {'code': {'type': 'string'}, 'language': {'type': 'string'}}, 'required': ['code']},
    _static_analysis)


def _trigger_workflow(args):
    """Trigger another Purko workflow via the dashboard API."""
    workflow_name = args.get('workflow', '')
    namespace = args.get('namespace', 'ai-agents')
    payload = args.get('payload', {})
    if isinstance(payload, str):
        try:
            payload = json.loads(payload)
        except json.JSONDecodeError:
            payload = {'task': payload}

    # Use localhost since executor runs with hostNetwork
    url = f'http://localhost:8082/api/trigger/{namespace}/{workflow_name}'
    logger.info(f"CHAIN_TRIGGER: {url} payload={json.dumps(payload)[:200]}")

    try:
        resp = requests.post(url, json=payload, timeout=30)
        result = resp.json()
        logger.info(f"CHAIN_RESULT: {resp.status_code} {json.dumps(result)[:200]}")
        return json.dumps(result)
    except Exception as e:
        logger.error(f"CHAIN_ERROR: {e}")
        return json.dumps({'error': str(e)})

register_builtin_tool('trigger-workflow',
    'Trigger another Purko workflow to chain SDLC phases. Passes the current context as payload to the next workflow.',
    {'type': 'object', 'properties': {
        'workflow': {'type': 'string', 'description': 'Name of the workflow to trigger'},
        'namespace': {'type': 'string', 'description': 'Namespace (default: ai-agents)'},
        'payload': {'type': 'object', 'description': 'JSON payload with parameters for the next workflow'}
    }, 'required': ['workflow']},
    _trigger_workflow)


def check_autonomy_for_tool(tool_name):
    """Check if a specific tool call is allowed under current autonomy level.
    Returns error string if blocked, None if allowed."""
    if AUTONOMY_LEVEL == 'manual':
        return f"BLOCKED: Manual autonomy — tool '{tool_name}' not allowed. Provide analysis without tools."
    if AUTONOMY_LEVEL == 'restricted' and tool_name in WRITE_TOOLS:
        return f"BLOCKED: Restricted autonomy — write tool '{tool_name}' not allowed."
    return None


# ── ReAct Loop ────────────────────────────────────────────────────────

def run_react_loop(agent_tools, tool_defs):
    """Execute the ReAct tool-use loop."""
    # Apply autonomy filtering to tool definitions
    tool_defs = filter_tools_by_autonomy(tool_defs)

    # Load memory context from previous executions
    memory_context = load_memory_context()

    messages = [
        {'role': 'user', 'content': f"Execute this task:\n\n{STEP_INPUT}{memory_context}"},
    ]

    tool_call_count = 0
    tool_call_log = []  # Track all tool calls for output
    final_output = None

    for iteration in range(MAX_TOOL_CALLS + 1):
        logger.info(f"Iteration {iteration + 1}, tool calls so far: {tool_call_count}, messages: {len(messages)}")

        try:
            if MODEL_API_FORMAT == 'anthropic':
                tools = format_tools_for_anthropic(tool_defs) if tool_defs else None
                response = call_anthropic(messages, SYSTEM_PROMPT, tools)
                final_output, tool_requests = parse_anthropic_response(response, messages)
            else:
                tools = format_tools_for_openai(tool_defs) if tool_defs else None
                response = call_openai(messages, SYSTEM_PROMPT, tools)
                final_output, tool_requests = parse_openai_response(response, messages)

        except requests.exceptions.ConnectionError as e:
            # A configured endpoint that is unreachable is an outage, not an
            # invitation to fabricate output (F24) — fail the step loudly.
            logger.error(f"Model API not reachable: {e}")
            final_output = api_failure_output(f"Model API not reachable: {e}", STEP_NAME)
            break
        except requests.exceptions.HTTPError as e:
            if e.response is not None and e.response.status_code == 401:
                logger.error("Model API rejected credentials (401)")
                final_output = api_failure_output("Model API rejected credentials (HTTP 401) — check the provider API key", STEP_NAME)
            else:
                final_output = {'error': str(e), 'step': STEP_NAME}
            break
        except Exception as e:
            logger.error(f"Model API error: {e}")
            final_output = {'error': str(e), 'step': STEP_NAME}
            break

        if not tool_requests:
            break

        pending_results = []
        limit_reached = False
        for tool_req in tool_requests:
            tool_call_count += 1
            if tool_call_count > MAX_TOOL_CALLS:
                if not limit_reached:
                    logger.warning(f"Max tool calls ({MAX_TOOL_CALLS}) reached")
                    limit_reached = True
                pending_results.append({
                    'type': 'tool_result',
                    'tool_use_id': tool_req.get('id', ''),
                    'content': 'Tool call limit reached. Please provide your final response.',
                    'is_error': True,
                })
                continue

            tool_name = tool_req['name']
            tool_input = tool_req['input']

            # Log tool call with server and input summary
            server_name = tool_to_client.get(tool_name, None)
            server_label = server_name.server_name if server_name else 'unknown'
            input_summary = json.dumps(tool_input, default=str)[:200]
            logger.info(f"TOOL_CALL #{tool_call_count}: {tool_name} (server: {server_label}) input: {input_summary}")

            # Autonomy check — block disallowed tools at runtime
            blocked = check_autonomy_for_tool(tool_name)
            if blocked:
                logger.warning(f"TOOL_BLOCKED: {tool_name} — {blocked}")
                tool_call_log.append({'tool': tool_name, 'server': server_label, 'status': 'blocked', 'reason': blocked})
                if MODEL_API_FORMAT == 'anthropic':
                    pending_results.append({
                        'type': 'tool_result',
                        'tool_use_id': tool_req.get('id', ''),
                        'content': blocked,
                        'is_error': True,
                    })
                continue

            t_start = time.time()
            result = execute_tool(agent_tools, tool_name, tool_input)
            t_elapsed = time.time() - t_start

            result_preview = str(result)[:200].replace('\n', ' ')
            logger.info(f"TOOL_RESULT #{tool_call_count}: {tool_name} ({t_elapsed:.1f}s) result: {result_preview}")
            tool_call_log.append({'tool': tool_name, 'server': server_label, 'elapsed_s': round(t_elapsed, 1), 'input_preview': input_summary[:100], 'result_bytes': len(str(result))})

            if MODEL_API_FORMAT == 'anthropic':
                pending_results.append({
                    'type': 'tool_result',
                    'tool_use_id': tool_req.get('id', ''),
                    'content': str(result)[:50000],
                })
            else:
                messages.append({
                    'role': 'tool',
                    'tool_call_id': tool_req.get('id', ''),
                    'content': result,
                })

        if MODEL_API_FORMAT == 'anthropic' and pending_results:
            messages.append({
                'role': 'user',
                'content': pending_results,
            })

    output = final_output or {'status': 'completed', 'step': STEP_NAME}

    # Attach tool call log to output
    if tool_call_log:
        output['tools_called'] = [t['tool'] for t in tool_call_log if t.get('status') != 'blocked']
        output['tool_call_log'] = tool_call_log

    # Extract structured fields from response text for condition evaluation
    output = extract_structured_fields(output)

    return output


def extract_structured_fields(output):
    """Scan the response text for known patterns and promote them to top-level JSON fields.
    This enables condition expressions like: steps.X.output.verdict == approve"""
    import re as _re

    response = output.get('response', '')
    if not response:
        return output

    # Patterns: **Key**: value, **Key**: `value`, Key: value at start of line
    # Patterns handle markdown bold, emojis, backticks, and various formatting:
    # **Verdict**: approve
    # **Verdict**: `approve`
    # **Verdict**: ⚠️ `request_changes`
    # Verdict: approve
    # The .{0,10} allows emojis and symbols between colon and value
    patterns = {
        'verdict': [
            r'\*\*[Vv]erdict\*\*:\s*.{0,10}?`?(approve|request_changes|comment|reject|merge|revise|discuss)`?',
            r'[Vv]erdict\*?\*?:\s*.{0,10}?`?(approve|request_changes|comment|reject|merge|revise|discuss)`?',
        ],
        'severity': [
            r'\*\*[Ss]everity\*\*:\s*.{0,10}?`?([Pp][1-4]|[Cc]ritical|[Hh]igh|[Mm]edium|[Ll]ow|[Cc]lean)`?',
            r'[Ss]everity\*?\*?:\s*.{0,10}?`?([Pp][1-4]|[Cc]ritical|[Hh]igh|[Mm]edium|[Ll]ow|[Cc]lean)`?',
        ],
        'riskLevel': [
            r'\*\*[Rr]isk\s*[Ll]evel\*\*:\s*.{0,10}?`?([Cc]ritical|[Hh]igh|[Mm]edium|[Ll]ow|[Cc]lean)`?',
            r'[Rr]isk\s*[Ll]evel\*?\*?:\s*.{0,10}?`?([Cc]ritical|[Hh]igh|[Mm]edium|[Ll]ow|[Cc]lean)`?',
        ],
        'status': [
            r'\*\*[Ss]tatus\*\*:\s*.{0,10}?`?(\w+)`?',
        ],
        'recommendation': [
            r'\*\*[Rr]ecommendation\*\*:\s*.{0,10}?`?(approve|reject|merge|revise|discuss|monitor|remediate|escalate)`?',
        ],
        'feasibility': [
            r'\*\*[Ff]easibility\*\*:\s*.{0,10}?`?(approved|rejected|needs-discussion|feasible|infeasible)`?',
        ],
        'anomalyDetected': [
            r'anomaly[Dd]etected\*?\*?:\s*.{0,10}?`?(true|false)`?',
        ],
        'findingsCount': [
            r'\*\*[Tt]otal\s*[Ff]indings?\*\*:\s*.{0,10}?`?(\d+)`?',
            r'\*\*[Ff]indings?\s*[Ff]ound\*\*:\s*.{0,10}?`?(\d+)`?',
            r'\*\*(\d+)\s+findings?\*\*',
            r'(\d+)\s+findings?\s',
        ],
        'coveragePercent': [
            r'\*\*[Cc]overage\*\*:\s*.{0,10}?`?(\d+)%?`?',
            r'[Cc]overage:\s*.{0,10}?`?(\d+)%?`?',
        ],
    }

    extracted = {}
    for field, field_patterns in patterns.items():
        for pattern in field_patterns:
            match = _re.search(pattern, response, _re.IGNORECASE)
            if match:
                value = match.group(1).strip()
                # Convert known types
                if value.lower() in ('true', 'false'):
                    value = value.lower() == 'true'
                elif value.isdigit():
                    value = int(value)
                else:
                    value = value.lower()
                extracted[field] = value
                break

    if extracted:
        logger.info(f"Extracted structured fields: {extracted}")
        for k, v in extracted.items():
            if k not in output:  # don't overwrite existing fields
                output[k] = v

    return output


def wrap_model_text(text):
    """Model output as a dict: JSON objects pass through; everything else
    (plain text, bare JSON strings/lists/numbers) is wrapped — downstream
    code requires a dict and crashes on other types."""
    try:
        parsed = json.loads(text)
        if isinstance(parsed, dict):
            return parsed
    except json.JSONDecodeError:
        pass
    return {'response': text, 'step': STEP_NAME}


def parse_anthropic_response(response, messages):
    content = response.get('content', [])
    text_parts = []
    tool_requests = []

    assistant_content = []
    for block in content:
        assistant_content.append(block)
        if block['type'] == 'text':
            text_parts.append(block['text'])
        elif block['type'] == 'tool_use':
            tool_requests.append({
                'id': block['id'],
                'name': block['name'],
                'input': block['input'],
            })

    messages.append({'role': 'assistant', 'content': assistant_content})

    if tool_requests:
        return None, tool_requests

    text = '\n'.join(text_parts)
    return wrap_model_text(text), []


def parse_openai_response(response, messages):
    choice = response['choices'][0]
    message = choice['message']
    messages.append(message)

    if message.get('tool_calls'):
        tool_requests = []
        for tc in message['tool_calls']:
            tool_requests.append({
                'id': tc['id'],
                'name': tc['function']['name'],
                'input': json.loads(tc['function']['arguments']),
            })
        return None, tool_requests

    text = message.get('content', '')
    return wrap_model_text(text), []


# ── Tool Execution ────────────────────────────────────────────────────

def execute_tool(agent_tools, tool_name, tool_input):
    """Execute a tool call — route by type: function → builtin → mcp → api → fallback."""

    # 1. Function tools (CodeAct, vector-search)
    if tool_name in FUNCTION_TOOLS:
        return FUNCTION_TOOLS[tool_name]['handler'](tool_input)

    # code-sandbox is an alias for execute_code
    if tool_name == 'execute_code' or tool_name == 'code-sandbox':
        if CODE_EXECUTION_ENABLED:
            return execute_code(tool_input.get('code', ''), tool_input.get('language', 'python'))
        return "ERROR: Code execution is not enabled for this agent"

    # 2. Builtin tools (static-analysis)
    if tool_name in BUILTIN_TOOLS:
        return BUILTIN_TOOLS[tool_name]['handler'](tool_input)

    # 3. MCP tools — route to the right server
    client = tool_to_client.get(tool_name)
    if client:
        result = client.call_tool(tool_name, tool_input)
        if result and not result.startswith('ERROR:'):
            return result

    # Try all MCP clients as fallback
    for c in all_mcp_clients:
        result = c.call_tool(tool_name, tool_input)
        if result and not result.startswith('ERROR:'):
            return result

    # 4. API tools — direct HTTP endpoint call
    for tool in agent_tools:
        if tool['name'] == tool_name and (tool.get('type') == 'api' or tool.get('endpoint')):
            return call_api_tool(tool, tool_input)

    return f"Tool '{tool_name}' returned no result"


def call_api_tool(tool_spec, tool_input):
    """Call an API tool directly via HTTP. Handles both string and struct endpoint formats."""
    endpoint = tool_spec.get('endpoint', '')

    # Handle string endpoint (spec format: "http://host:port/path")
    if isinstance(endpoint, str):
        url = endpoint
        method = 'POST'
        headers = {}
        timeout = 30
    else:
        # Struct endpoint (EndpointSpec)
        url = endpoint.get('url', '')
        method = endpoint.get('method', 'POST').upper()
        headers = endpoint.get('headers', {})
        timeout = endpoint.get('timeoutSeconds', 30)

    if not url:
        return f"API error: no endpoint URL for tool {tool_spec.get('name', '?')}"

    # Per-tool timeout from spec (overrides endpoint timeout)
    tool_timeout = tool_spec.get('timeout', '')
    if tool_timeout:
        parsed = parse_tool_timeout(tool_timeout)
        if parsed > 0:
            timeout = parsed

    logger.info(f"API_CALL: {method} {url} (timeout: {timeout}s)")

    try:
        if method == 'GET':
            resp = requests.get(url, params=tool_input, headers=headers, timeout=timeout)
        else:
            resp = requests.request(method, url, json=tool_input, headers=headers, timeout=timeout)
        resp.raise_for_status()
        logger.info(f"API_RESULT: {resp.status_code}, {len(resp.text)} chars")
        return resp.text[:100000]
    except Exception as e:
        logger.warning(f"API_ERROR: {e}")
        return f"API error: {e}"


def parse_tool_timeout(timeout_str):
    """Parse timeout string like '10s', '2m', '1h' to seconds."""
    if not timeout_str:
        return 0
    s = str(timeout_str).strip()
    if s.endswith('s'):
        return int(s[:-1])
    if s.endswith('m'):
        return int(s[:-1]) * 60
    if s.endswith('h'):
        return int(s[:-1]) * 3600
    try:
        return int(s)
    except ValueError:
        return 0


# ── Demo Mode ─────────────────────────────────────────────────────────

def run_demo_mode(agent_tools):
    """Run without model API — call MCP tools directly."""
    logger.info("Demo mode: calling MCP tools directly (no model API)")

    results = {}

    if all_mcp_clients:
        client = all_mcp_clients[0]  # use first available client
        demo_calls = [
            ('list_namespaces', {}),
            ('list_pods_in_namespace', {'namespace': 'ai-agents'}),
        ]

        for tool in agent_tools:
            name = tool.get('name', '')
            if 'metric' in name.lower() or 'prometheus' in name.lower():
                demo_calls.append(('prometheus_query', {'query': 'up', 'namespace': 'ai-agents'}))
            elif 'log' in name.lower():
                demo_calls.append(('list_pods_in_namespace', {'namespace': 'kube-system'}))
            elif 'event' in name.lower():
                demo_calls.append(('smart_get_namespace_events', {'namespace': 'ai-agents', 'time_period': '1h'}))

        for tool_name, args in demo_calls[:3]:
            logger.info(f"Demo tool call: {tool_name}")
            # Route through tool_to_client if available
            c = tool_to_client.get(tool_name, client)
            result = c.call_tool(tool_name, args)
            if result:
                results[tool_name] = result[:2000]

    return {
        'status': 'completed',
        'mode': 'demo',
        'step': STEP_NAME,
        'workflow': WORKFLOW_NAME,
        'servers': len(all_mcp_clients),
        'tools_called': list(results.keys()),
        'data': results,
    }


# ── Main ──────────────────────────────────────────────────────────────

def main():
    logger.info(f"Step executor starting: workflow={WORKFLOW_NAME} step={STEP_NAME}")
    logger.info(f"Model: {MODEL_PROVIDER}/{MODEL_NAME}, autonomy: {AUTONOMY_LEVEL}")

    # Parse agent tools
    try:
        agent_tools = json.loads(AGENT_TOOLS_JSON)
    except json.JSONDecodeError:
        agent_tools = []
    logger.info(f"Agent tools: {len(agent_tools)}")

    # Connect to all MCP servers dynamically
    mcp_tools = connect_mcp_servers()

    # Build tool definitions for the model
    tool_defs = build_tool_definitions(agent_tools, mcp_tools)
    logger.info(f"Tool definitions for model: {len(tool_defs)}")

    # Run the ReAct loop
    if llm_mode():
        endpoint_info = MODEL_ENDPOINT or ('Vertex AI' if USE_VERTEX else 'default')
        logger.info(f"LLM mode: {MODEL_API_FORMAT} format, endpoint: {endpoint_info}")
        output = run_react_loop(agent_tools, tool_defs)
    else:
        logger.info("No MODEL_API_KEY, VERTEX_PROJECT_ID, or MODEL_ENDPOINT — running in demo mode")
        output = run_demo_mode(agent_tools)

    # Save memory for future executions
    memory_update = save_memory([], output)

    # Add cost/token metadata to output
    if isinstance(output, dict):
        output['_metrics'] = {
            'tokens_in': total_tokens_in,
            'tokens_out': total_tokens_out,
            'cost_usd': round(total_cost_usd, 6),
            'autonomy': AUTONOMY_LEVEL,
        }
        if memory_update:
            output['_memory_update'] = memory_update

    # Apply content filters to output
    output_json = json.dumps(output, default=str)
    output_json = apply_content_filters(output_json)

    print(f"OUTPUT:{output_json}")
    logger.info(f"Step completed: {len(output_json)} bytes, cost: ${total_cost_usd:.4f}")
    sys.exit(output_exit_code(output))


def api_failure_output(detail, step):
    """Error payload for model-API failures — exits nonzero via
    output_exit_code, never demo mode."""
    return {'error': detail, 'step': step}


def output_exit_code(output):
    """Non-zero when the step produced only an error — the Job must fail so
    the controller records a Failed step and retries, instead of archiving
    the error as a successful run."""
    if isinstance(output, dict) and output.get('error'):
        return 1
    return 0


if __name__ == '__main__':
    main()
