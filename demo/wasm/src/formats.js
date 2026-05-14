export const formats = [
  ['chat', 'openai-chat-completions', 'OpenAI Chat Completions'],
  ['responses', 'openai-responses', 'OpenAI Responses'],
  ['anthropic', 'anthropic-messages', 'Anthropic Messages'],
  ['genai', 'google-genai', 'Google GenAI'],
  ['ail', 'ail', 'AIL Assembly'],
];

export const types = [
  ['request', 'Request'],
  ['response', 'Response'],
  ['stream_chunk', 'Stream Chunk'],
];

export const styleToSlug = Object.fromEntries(formats.map(([slug, style]) => [style, slug]));
export const slugToStyle = Object.fromEntries(formats.map(([slug, style]) => [slug, style]));
export const slugNames = Object.fromEntries(formats.map(([slug, , name]) => [slug, name]));
