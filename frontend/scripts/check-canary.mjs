// Proves that `svelte-check --tsgo` still reports type errors.
//
// Under --tsgo, if svelte2tsx emits invalid TypeScript for any component
// (for example a `*/` inside an `@component` doc comment), TypeScript 7 stops
// at the syntax error and skips every type check, and svelte-check drops that
// syntax error because it cannot map it back to a source line. `pnpm check`
// then reports 0 errors while real errors exist
// (sveltejs/language-tools#2995). This script plants one known type error and
// fails unless the check reports it. `pnpm check` also deletes the planted
// file, in case an interrupted run left it behind.
import { spawnSync } from 'node:child_process';
import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const require = createRequire(join(root, 'package.json'));
const canary = 'src/__tsgo_canary__.ts';
const canaryPath = join(root, canary);

// The local package's bin entry, run with this node, so neither PATH nor a
// global install decides which version runs.
function bin(pkg, name) {
	const manifest = require.resolve(`${pkg}/package.json`);
	const { bin } = JSON.parse(readFileSync(manifest, 'utf8'));
	return join(dirname(manifest), typeof bin === 'string' ? bin : bin[name]);
}

function run(file, args) {
	return spawnSync(process.execPath, [file, ...args], { cwd: root, encoding: 'utf8' });
}

const sync = run(bin('@sveltejs/kit', 'svelte-kit'), ['sync']);
if (sync.status !== 0) {
	process.stderr.write(sync.stdout + sync.stderr);
	process.exit(1);
}

writeFileSync(canaryPath, 'export const tsgoCanary: number = "not a number";\n');
let result;
try {
	result = run(bin('svelte-check', 'svelte-check'), [
		'--tsgo',
		'--tsconfig',
		'./tsconfig.json',
		'--output',
		'machine'
	]);
} finally {
	rmSync(canaryPath, { force: true });
}

if (result.error) {
	console.error(`check-canary: could not run svelte-check: ${result.error.message}`);
	process.exit(1);
}
const caught = result.stdout
	.split('\n')
	.some((line) => line.includes(' ERROR ') && line.replaceAll('\\\\', '/').includes(`"${canary}"`));
if (caught) {
	console.log('check-canary: the type check reports errors.');
	process.exit(0);
}

console.error(
	`check-canary: svelte-check --tsgo did not report the planted type error in ${canary}.\n` +
		'Type checking stopped early, most likely on invalid TypeScript generated from a\n' +
		'.svelte file (for example `*/` inside an @component comment).'
);
// svelte-check drops the syntax errors, but TypeScript 7 run on the overlay it
// generated names the generated copy of the broken component.
const overlay = run(bin('@typescript/native', 'tsc'), [
	'-p',
	'.svelte-kit/.svelte-check/tsconfig.json',
	'--noEmit',
	'--pretty',
	'false'
]);
const syntax = (overlay.stdout + overlay.stderr)
	.split('\n')
	.filter((line) => /error TS1\d{3}:/.test(line))
	.slice(0, 10);
if (syntax.length > 0) {
	console.error('Syntax errors in the generated TypeScript:\n' + syntax.join('\n'));
} else {
	process.stderr.write(result.stdout.split('\n').slice(-5).join('\n') + '\n');
}
process.exit(1);
