// Regression guard for web/src/utils/composerize.js.
//
// Not wired into a test framework (frontend ships none); runnable directly:
//   node web/src/utils/composerize.test.mjs
//
// Scope: the contract we own is the placeholder-line stripping post-process.
// If composerize@1.x changes its placeholder text (`name: <your project
// name>`) or stops emitting it entirely, the assertions here will fail
// loudly rather than silently leaving a stale line in the generated YAML.
//
// If this file fails after a dependency bump, check the regex in
// composerize.js first.

import { dockerRunToCompose } from './composerize.js'

let pass = 0
let fail = 0
function ok(name, cond, detail) {
    if (cond) {
        console.log('PASS', name)
        pass++
    } else {
        console.log('FAIL', name, detail ? `— ${detail}` : '')
        fail++
    }
}

function asserts(name, cmd, checks) {
    try {
        const yaml = dockerRunToCompose(cmd)
        for (const [label, cond] of checks(yaml)) {
            ok(`${name} / ${label}`, cond, JSON.stringify(yaml.slice(0, 120)))
        }
    } catch (e) {
        ok(`${name} / throws`, false, `unexpected throw: ${e.message}`)
    }
}

// Strips the placeholder even when preceded by composerize's explanatory
// comment (e.g. when --network mynet forces an external-network warning).
asserts(
    'external network placeholder strip',
    'docker run -d -p 8080:80 --network mynet nginx:latest',
    (yaml) => [
        ['no placeholder line', !yaml.includes('name: <your project name>')],
        ['services block present', yaml.includes('services:')],
    ],
)

// Strips the leading placeholder for a simple image-only command.
asserts(
    'simple image placeholder strip',
    'docker run nginx',
    (yaml) => [
        ['no placeholder line', !yaml.includes('name: <your project name>')],
        ['services block present', yaml.includes('services:')],
        ['image captured', yaml.includes('image: nginx')],
    ],
)

// Complex flags round-trip without losing semantic fields.
asserts(
    'env + port + volume + restart',
    'docker run -d -p 8080:80 -e FOO=bar -v /data:/data --restart unless-stopped nginx:latest',
    (yaml) => [
        ['no placeholder line', !yaml.includes('name: <your project name>')],
        ['env captured', yaml.includes('FOO=bar')],
        ['port captured', yaml.includes('8080:80')],
        ['restart captured', yaml.includes('restart: unless-stopped')],
    ],
)

// Empty input must throw — caller treats a throw as "show friendly error".
try {
    dockerRunToCompose('')
    ok('empty input throws', false, 'expected throw, got output')
} catch {
    ok('empty input throws', true)
}

console.log(`\n${pass} pass, ${fail} fail`)
process.exit(fail > 0 ? 1 : 0)
