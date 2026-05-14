import { mkdir, readdir, readFile, writeFile } from 'node:fs/promises';
import { basename, join } from 'node:path';

const fixtureRoot = join(import.meta.dirname, '..', '..', '..', 'fixtures');
const publicDir = join(import.meta.dirname, '..', 'public');
const outFile = join(publicDir, 'examples.json');

const cases = [
  ['chat', 'openai-chat-completions'],
  ['responses', 'openai-responses'],
  ['anthropic', 'anthropic-messages'],
  ['genai', 'google-genai'],
  ['ail', 'ail'],
];

const typeDirs = [
  ['request', 'request'],
  ['response', 'response'],
  ['stream', 'stream_chunk'],
];

const examples = {};

for (const [dir, style] of cases) {
  examples[style] = {};

  for (const [fixtureType, appType] of typeDirs) {
    const fixtures = await readFixtures(join(fixtureRoot, dir, fixtureType), style);
    if (fixtures.length > 0) examples[style][appType] = fixtures;
  }
}

await mkdir(publicDir, { recursive: true });
await writeFile(outFile, `${JSON.stringify(examples, null, 2)}\n`);

async function readFixtures(dir, style) {
  let entries;
  try {
    entries = await readdir(dir, { withFileTypes: true });
  } catch (err) {
    if (err.code === 'ENOENT') return [];
    throw err;
  }

  const files = entries
    .filter((entry) => entry.isFile() && isFixture(entry.name))
    .map((entry) => entry.name)
    .sort();

  return Promise.all(files.map(async (file) => ({
    name: labelFor(file),
    payload: await parseFixture(join(dir, file), style),
  })));
}

function isFixture(file) {
  return file.endsWith('.json') || file.endsWith('.ail');
}

async function parseFixture(path, style) {
  const body = await readFile(path, 'utf8');
  return style === 'ail' ? body.trim() : JSON.parse(body);
}

function labelFor(file) {
  return basename(file, '.json')
    .split(/[_-]+/)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}
