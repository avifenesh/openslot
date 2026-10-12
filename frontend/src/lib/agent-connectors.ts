export type AgentConnector = {
 id: string; name: string; category: 'app' | 'provider'; description: string;
 format: 'json' | 'toml' | 'python' | 'text'; filename: string; destination: string;
 public_url: boolean; oauth?: boolean; docs: string;
};

export function normalizeMCPEndpoint(value: string): string {
 const url = new URL(value);
 if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
  throw new Error('Use an HTTP or HTTPS MCP URL without credentials, query parameters, or a fragment.');
 }
 if (!url.pathname.replace(/\/$/, '').endsWith('/mcp')) throw new Error('The connector URL must end in /mcp.');
 return url.href.replace(/\/$/, '');
}

export function needsPublicURL(endpoint: string): boolean {
 try {
  const u = new URL(endpoint); const h = u.hostname.toLowerCase().replace(/^\[|\]$/g, '');
  return u.protocol !== 'https:' || (!h.includes('.') && !h.includes(':')) || h === 'localhost' || h.endsWith('.localhost') || h.endsWith('.local') || h.endsWith('.internal') || h === '::1' || h.startsWith('fc') && h.includes(':') || h.startsWith('fd') && h.includes(':') || h.startsWith('fe80:') || /^(127\.|10\.|0\.|169\.254\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(h);
 } catch { return true; }
}

const json = (value: unknown) => JSON.stringify(value, null, 2);
const shellQuote = (value: string) => `'${value.replaceAll("'", "'\\''")}'`;

export function connectorSetup(connector: AgentConnector, endpoint: string, key = ''): string {
 endpoint = normalizeMCPEndpoint(endpoint);
 const token = key || 'YOUR_OPENSLOT_API_KEY';
 const headers = { Authorization: `Bearer ${token}` };
 const entry = { url: endpoint, headers };
 switch (connector.id) {
  case 'codex': return `[mcp_servers.openslot]\nurl = ${JSON.stringify(endpoint)}\nbearer_token_env_var = "OPENSLOT_API_KEY"\n`;
  case 'claude-code': return json({mcpServers:{openslot:{type:'http', ...entry}}});
  case 'claude': return `Name: OpenSlot\nRemote MCP server URL: ${endpoint}\n\nAdd a custom connector in Claude settings, then sign in to OpenSlot and approve access.\nNo OpenSlot API key is needed for this OAuth flow.\n`;
  case 'antigravity': return json({mcpServers:{openslot:{serverUrl:endpoint, headers}}});
  case 'gemini-cli': return json({mcpServers:{openslot:{httpUrl:endpoint, headers}}});
  case 'vscode': return json({servers:{openslot:{type:'http', ...entry}}});
  case 'cursor': case 'generic': return json({mcpServers:{openslot:entry}});
  case 'bedrock': return json([{type:'remote_mcp',name:'openslot',config:{remoteMcp:{url:endpoint,headers}}}]);
  case 'anthropic': return `import os\nimport anthropic\n\nclient = anthropic.Anthropic()\nresponse = client.beta.messages.create(\n    model=os.environ["ANTHROPIC_MODEL"],\n    max_tokens=1024,  # Response budget for this short connection example.\n    messages=[{"role": "user", "content": "List my OpenSlot meeting types."}],\n    mcp_servers=[{\n        "type": "url", "name": "openslot",\n        "url": ${JSON.stringify(endpoint)},\n        "authorization_token": os.environ["OPENSLOT_API_KEY"],\n    }],\n    tools=[{"type": "mcp_toolset", "mcp_server_name": "openslot"}],\n    betas=["mcp-client-2025-11-20"],\n)\nprint(response)\n`;
  case 'openai': case 'grok': {
   const xai = connector.id === 'grok'; const env = xai ? 'XAI' : 'OPENAI';
   return `import os\nfrom openai import OpenAI\n\nclient = OpenAI(api_key=os.environ["${env}_API_KEY"]${xai ? ', base_url="https://api.x.ai/v1"' : ''})\nresponse = client.responses.create(\n    model=os.environ["${env}_MODEL"],\n    input="List my OpenSlot meeting types.",\n    tools=[{\n        "type": "mcp", "server_label": "openslot",\n        "server_url": ${JSON.stringify(endpoint)},\n        "authorization": os.environ["OPENSLOT_API_KEY"],\n${xai ? '' : '        "require_approval": "always",\n'}    }],\n)\nprint(response)\n${xai ? '# xAI does not support require_approval; apply confirmations in your agent.\n' : '# Continue any mcp_approval_request in your existing agent approval flow.\n'}`;
  }
  case 'glm': return `import os\nfrom openai import OpenAI\n\nclient = OpenAI(api_key=os.environ["ZAI_API_KEY"], base_url="https://api.z.ai/api/paas/v4")\nresponse = client.chat.completions.create(\n    model=os.environ["ZAI_MODEL"],\n    messages=[{"role": "user", "content": "List my OpenSlot meeting types."}],\n    extra_body={"tools": [{\n        "type": "mcp",\n        "mcp": {\n            "server_label": "openslot",\n            "server_url": ${JSON.stringify(endpoint)},\n            "transport_type": "streamable-http",\n            "headers": {"Authorization": "Bearer " + os.environ["OPENSLOT_API_KEY"]},\n        },\n    }]},\n)\nprint(response)\n`;
  default: throw new Error('No setup adapter is available for this connector.');
 }
}

export function connectorCommand(connector: AgentConnector, endpoint: string): string | null {
 endpoint = normalizeMCPEndpoint(endpoint);
 switch (connector.id) {
  case 'codex': return `codex mcp add openslot --url ${shellQuote(endpoint)} --bearer-token-env-var OPENSLOT_API_KEY`;
  case 'claude-code': return `claude mcp add --transport http openslot ${shellQuote(endpoint)} --header "Authorization: Bearer $OPENSLOT_API_KEY"`;
  default: return null;
 }
}

export async function checkMCPConnection(endpoint: string, key: string): Promise<string[]> {
 normalizeMCPEndpoint(endpoint);
 if (!key.trim()) throw new Error('Create or paste an OpenSlot API key first.');
 const response = await fetch(endpoint, {
  method:'POST', credentials:'omit', redirect:'error', signal:AbortSignal.timeout(15000),
  headers:{'Content-Type':'application/json',Accept:'application/json, text/event-stream',Authorization:`Bearer ${key.trim()}`,'MCP-Protocol-Version':'2026-07-28','Mcp-Method':'tools/list'},
  body:JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list',params:{_meta:{'io.modelcontextprotocol/protocolVersion':'2026-07-28','io.modelcontextprotocol/clientCapabilities':{},'io.modelcontextprotocol/clientInfo':{name:'openslot-connection-check',version:'1'}}}}),
 });
 if (response.status === 401) throw new Error('This key is invalid or has been revoked.');
 if (!response.ok) throw new Error(`OpenSlot returned HTTP ${response.status}.`);
 const text = await response.text();
 const message = response.headers.get('content-type')?.includes('text/event-stream')
  ? text.split(/\r?\n\r?\n/).map(event => event.split(/\r?\n/).filter(line=>line.startsWith('data:')).map(line=>line.slice(5).trimStart()).join('\n')).filter(Boolean).map(data=>JSON.parse(data)).find(result=>result.id===1)
  : JSON.parse(text);
 if (message?.error || !Array.isArray(message?.result?.tools)) throw new Error('The endpoint did not return an MCP tool list.');
 const names = message.result.tools.map((tool: {name:string})=>tool.name);
 if (!names.includes('get_available_slots') || !names.includes('create_booking')) throw new Error('This MCP server does not expose the OpenSlot scheduling tools.');
 return names;
}
