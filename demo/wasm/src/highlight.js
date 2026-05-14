const opcodeClasses = [
  [/^(REQ_START|REQ_YIELD|REQ_END|SUB_CONTENT|SUB_REASON|RESP_START|RESP_END)$/, 'asm-opcode-request'],
  [/^(MSG_START|MSG_END|DEF_START|DEF_END|CALL_START|CALL_END|RESULT_START|RESULT_END|THINK_START|THINK_END)$/, 'asm-opcode-struct'],
  [/^(ROLE_SYS|ROLE_USR|ROLE_AST|ROLE_TOOL|ROLE_DEV)$/, 'asm-opcode-role'],
  [/^(TXT_CHUNK|IMG_REF|AUD_REF|TXT_REF|FILE_REF|VID_REF|PART_JSON)$/, 'asm-opcode-content'],
  [/^(THINK_CHUNK|THINK_REF)$/, 'asm-opcode-thinking'],
  [/^(DEF_NAME|DEF_DESC|DEF_SCHEMA|DEF_RAW|CALL_NAME|CALL_ARGS|RESULT_DATA)$/, 'asm-opcode-tool'],
  [/^(SET_MODEL|SET_TEMP|SET_TOPP|SET_MAX|SET_STREAM|SET_STOP|SET_META|EXT_DATA|SET_REASON_EFFORT|SET_FMT|SET_SAFETY|SET_TOOL|SET_REASON_MODE|SET_REASON_BUDGET)$/, 'asm-opcode-config'],
  [/^(STREAM_START|STREAM_DELTA|STREAM_TOOL_DELTA|STREAM_END|STREAM_THINK_DELTA)$/, 'asm-opcode-stream'],
  [/^(RESP_ID|RESP_MODEL|RESP_DONE|USAGE)$/, 'asm-opcode-resp'],
];

function escapeHtml(text) {
  const node = document.createElement('div');
  node.textContent = text;
  return node.innerHTML;
}

export function highlightJSON(json) {
  return escapeHtml(json)
    .replace(/"([^"\\]*(\\.[^"\\]*)*)"\s*:/g, '<span class="json-key">"$1"</span>:')
    .replace(/:\s*"([^"\\]*(\\.[^"\\]*)*)"/g, ': <span class="json-string">"$1"</span>')
    .replace(/:\s*(-?\d+\.?\d*([eE][+-]?\d+)?)\b/g, ': <span class="json-number">$1</span>')
    .replace(/:\s*(true|false)\b/g, ': <span class="json-bool">$1</span>')
    .replace(/:\s*(null)\b/g, ': <span class="json-null">$1</span>');
}

export function highlightDisasm(text) {
  return escapeHtml(text).split('\n').map(highlightLine).join('\n');
}

function highlightLine(line) {
  if (line.trimStart().startsWith(';')) return `<span class="asm-comment">${line}</span>`;

  const trimmed = line.trimStart();
  const match = trimmed.match(/^([A-Z_]+)(.*)/);
  if (!match) return line;

  const [, opcode, rest] = match;
  const pad = '&nbsp;'.repeat(line.length - trimmed.length);
  const cls = opcodeClasses.find(([pattern]) => pattern.test(opcode))?.[1] || 'asm-opcode';
  const html = rest
    .replace(/"([^"\\]*(\\.[^"\\]*)*)"/g, '<span class="asm-string">"$1"</span>')
    .replace(/\b(\d+\.?\d*)\b/g, '<span class="asm-number">$1</span>')
    .replace(/(\{[^}]*\})/g, '<span class="asm-json">$1</span>');

  return `${pad}<span class="${cls}">${opcode}</span>${html}`;
}
