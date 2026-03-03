/**
 * Copy text to clipboard, with fallback for insecure (HTTP) contexts.
 *
 * @param {string} text  - The text to copy.
 * @param {function} [onSuccess] - Called after a successful copy.
 */
export function copyToClipboard(text, onSuccess) {
    if (!text) return

    // Prefer the Async Clipboard API when running in a secure context.
    if (window.isSecureContext && navigator.clipboard?.writeText) {
        navigator.clipboard.writeText(text).then(() => {
            onSuccess?.()
        }).catch(() => {
            fallbackCopy(text, onSuccess)
        })
        return
    }

    fallbackCopy(text, onSuccess)
}

function fallbackCopy(text, onSuccess) {
    const textarea = document.createElement('textarea')
    textarea.value = text
    // Keep the element off-screen but technically visible so that
    // `document.execCommand('copy')` works in all browsers.
    textarea.setAttribute('readonly', '')
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    textarea.style.top = '-9999px'
    document.body.appendChild(textarea)
    textarea.focus()
    textarea.select()

    try {
        const ok = document.execCommand('copy')
        if (ok) {
            onSuccess?.()
        }
    } catch {
        // Silently fail — nothing more we can do.
    } finally {
        document.body.removeChild(textarea)
    }
}
