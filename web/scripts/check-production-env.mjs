// Same fail-closed production policy as nexadmin release check.
import { readFileSync } from 'node:fs';
import { loadEnv, build } from 'vite';
import { execFileSync } from 'node:child_process';
let explicit = false;
for (const file of ['.env', '.env.local', '.env.production', '.env.production.local']) {
 let text;
 try { text = readFileSync(file, 'utf8'); } catch (error) { if (error.code === 'ENOENT') continue; throw error; }
 let seen = false;
 for (const line of text.split(/\r?\n/)) {
  if (/^\s*(?:export\s+)?VITE_MOCK\s*:/.test(line)) throw new Error(`${file}: unsupported VITE_MOCK assignment`);
  const assignment = line.match(/^\s*[^#=]+=(.*)$/);
  if (assignment) {
   const value = assignment[1].trim();
   if (value.length && [String.fromCharCode(39), String.fromCharCode(34), String.fromCharCode(96)].includes(value[0]) && !value.slice(1).includes(value[0])) throw new Error(`${file}: multiline dotenv value cannot be determined`);
  }
  if (!/^\s*(?:export\s+)?VITE_MOCK(?:\s*=|\s*$)/.test(line)) continue;
  if (seen || !/^\s*(?:export\s+)?VITE_MOCK\s*=\s*(?:true|false|"true"|"false"|'true'|'false')\s*(?:#.*)?$/.test(line)) throw new Error(`${file}: ambiguous VITE_MOCK`);
  seen = true;
  if (file === '.env.production') explicit = true;
 }
}
const env = loadEnv('production', process.cwd(), 'VITE_');
if (!explicit || env.VITE_MOCK !== 'false') throw new Error('Production requires effective VITE_MOCK=false');
process.env.VITE_MOCK = 'false';
if (process.argv.includes('--build')) {
  execFileSync(process.execPath, ['node_modules/typescript/bin/tsc', '--noEmit'], { stdio: 'inherit', env: process.env });
  await build({ mode: 'production' });
}
