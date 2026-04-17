// Wrappers around the `composerize` npm package.
//
// Provided as a thin module so the core conversion + post-processing logic
// can be exercised independently of React state.

import composerize from 'composerize'

/**
 * Convert a `docker run ...` command string into Docker Compose YAML.
 *
 * Behavior:
 *   - Trims surrounding whitespace.
 *   - Strips composerize's leading `name: <your project name>` placeholder
 *     line so the generated YAML starts at the `services:` block. The stack
 *     name is captured separately by the surrounding form.
 *   - Throws on empty input or empty conversion output.
 *
 * @param {string} cmd - the docker run command, e.g. `docker run -p 80:80 nginx`
 * @returns {string} Compose YAML
 */
export function dockerRunToCompose(cmd) {
    const trimmed = (cmd || '').trim()
    if (!trimmed) {
        throw new Error('empty docker run command')
    }
    const yaml = composerize(trimmed)
    if (typeof yaml !== 'string' || !yaml.trim()) {
        throw new Error('empty conversion result')
    }
    // Strip composerize's placeholder project-name line wherever it lands
    // (it's preceded by an explanatory `# ...` comment when the input
    // references an external network/volume that needs manual creation).
    return yaml.replace(/^name: <your project name>\r?\n/m, '')
}
