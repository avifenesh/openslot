// Proves that `svelte-check --tsgo` still reports type errors.
//
// Under --tsgo, if svelte2tsx emits invalid TypeScript for any component
// (for example a `*/` inside an `@component` doc comment), TypeScript 7 stops
// at the syntax error and skips every type check, and svelte-check drops that
// syntax error because it cannot map it back to a source line. `pnpm check`
// then reports 0 errors while real errors exist
// (sveltejs/language-tools#2995). This script plants one known type error and
// fails unless the check reports it.
import { spawnSync } from 'node:child_process';
import { rmSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url));
const canary = 'src/__tsgo_canary__.ts';
const canaryPath = fileURLToPath(new URL(`../${canary}`, import.meta.url));

writeFileSync(canaryPath, 'export const tsgoCanary: number = "not a number";\n');
let result;
try {
	result = spawnSync(
		'svelte-check',
		['--tsgo', '--tsconfig', './tsconfig.json', '--output', 'machine'],
		{ cwd: root, encoding: 'utf8', shell: process.platform === 'win32' }
	);
} finally {
	rmSync(canaryPath, { force: true });
}

if (result.error) {
	console.error(`check-canary: could not run svelte-check: ${result.error.message}`);
	process.exit(1);
}
const caught = result.stdout
	.split('\n')
	.some((line) => line.includes(' ERROR ') && line.includes(`"${canary}"`));
if (!caught) {
	console.error(
		`check-canary: svelte-check --tsgo did not report the planted type error in ${canary}.\n` +
			'Type checking stopped early, most likely on invalid TypeScript generated from a\n' +
			'.svelte file (for example `*/` inside an @component comment). Run\n' +
			'`pnpm exec svelte-check --tsconfig ./tsconfig.json` (TypeScript 6) to find it.'
	);
	process.stderr.write(result.stdout.split('\n').slice(-5).join('\n') + '\n');
	process.exit(1);
}
console.log('check-canary: the type check reports errors.');
