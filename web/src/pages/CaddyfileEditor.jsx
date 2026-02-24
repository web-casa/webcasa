import { useState, useEffect, useRef, useCallback } from 'react'
import { Box, Flex, Text, Button, Badge, Callout } from '@radix-ui/themes'
import { Save, Check, X, FileCode, AlignLeft, RefreshCw } from 'lucide-react'
import { caddyAPI } from '../api/index.js'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'
import { useTranslation } from 'react-i18next'

export default function CaddyfileEditor() {
    const { t } = useTranslation()
    const [content, setContent] = useState('')
    const [originalContent, setOriginalContent] = useState('')
    const [saving, setSaving] = useState(false)
    const [formatting, setFormatting] = useState(false)
    const [validating, setValidating] = useState(false)
    const [validationResult, setValidationResult] = useState(null) // { valid, error }
    const [message, setMessage] = useState(null) // { type: 'success'|'error', text }
    const [hasChanges, setHasChanges] = useState(false)
    const editorRef = useRef(null)
    const viewRef = useRef(null)

    useEffect(() => {
        loadCaddyfile()
    }, [])

    const loadCaddyfile = async () => {
        try {
            const res = await caddyAPI.caddyfile()
            const text = res.data.content || ''
            setContent(text)
            setOriginalContent(text)
            setHasChanges(false)
            setValidationResult(null)
            setMessage(null)

            // Initialize editor
            if (viewRef.current) {
                viewRef.current.destroy()
            }
            initEditor(text)
        } catch (e) {
            setMessage({ type: 'error', text: t('editor.load_failed') })
        }
    }

    const initEditor = useCallback((text) => {
        if (!editorRef.current) return

        const updateListener = EditorView.updateListener.of((update) => {
            if (update.docChanged) {
                const newContent = update.state.doc.toString()
                setContent(newContent)
                setHasChanges(newContent !== originalContent)
                setValidationResult(null)
            }
        })

        const state = EditorState.create({
            doc: text,
            extensions: [
                basicSetup,
                oneDark,
                updateListener,
                EditorView.theme({
                    '&': { height: '100%', fontSize: '13px' },
                    '.cm-scroller': { overflow: 'auto', fontFamily: "'JetBrains Mono', 'Fira Code', monospace" },
                    '.cm-content': { minHeight: '500px' },
                    '.cm-gutters': { backgroundColor: '#111113', borderRight: '1px solid #27272a' },
                }),
            ],
        })

        viewRef.current = new EditorView({
            state,
            parent: editorRef.current,
        })
    }, [originalContent])

    const handleFormat = async () => {
        setFormatting(true)
        try {
            const res = await caddyAPI.format(content)
            const formatted = res.data.content
            if (viewRef.current) {
                viewRef.current.dispatch({
                    changes: { from: 0, to: viewRef.current.state.doc.length, insert: formatted },
                })
            }
            setMessage({ type: 'success', text: t('editor.format_success') })
        } catch (e) {
            setMessage({ type: 'error', text: t('editor.format_failed') })
        }
        setFormatting(false)
    }

    const handleValidate = async () => {
        setValidating(true)
        try {
            const res = await caddyAPI.validate(content)
            setValidationResult(res.data)
            if (res.data.valid) {
                setMessage({ type: 'success', text: `✅ ${t('editor.valid')}` })
            } else {
                setMessage({ type: 'error', text: res.data.error })
            }
        } catch (e) {
            setMessage({ type: 'error', text: t('editor.invalid') })
        }
        setValidating(false)
    }

    const handleSave = async (reload = false) => {
        setSaving(true)
        try {
            const res = await caddyAPI.saveCaddyfile(content, reload)
            setOriginalContent(content)
            setHasChanges(false)
            setMessage({
                type: 'success',
                text: reload
                    ? (res.data.reload_error ? t('editor.save_reload_failed', { error: res.data.reload_error }) : t('editor.saved_reloaded'))
                    : t('editor.saved'),
            })
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || t('common.save_failed') })
        }
        setSaving(false)
    }

    const handleReset = () => {
        if (viewRef.current) {
            viewRef.current.dispatch({
                changes: { from: 0, to: viewRef.current.state.doc.length, insert: originalContent },
            })
        }
        setHasChanges(false)
        setValidationResult(null)
        setMessage(null)
    }

    return (
        <Box>
            <Flex justify="between" align="center" mb="4">
                <Flex align="center" gap="3">
                    <FileCode size={20} style={{ color: '#10b981' }} />
                    <Box>
                        <Text size="5" weight="bold" style={{ color: 'var(--cp-text)' }}>
                            {t('editor.title')}
                        </Text>
                        <Text size="2" color="gray" as="p">
                            {t('editor.subtitle')}
                        </Text>
                    </Box>
                </Flex>
                <Flex gap="2" align="center">
                    {hasChanges && (
                        <Badge color="yellow" variant="soft" size="1">
                            {t('editor.unsaved')}
                        </Badge>
                    )}
                    <Button variant="soft" size="2" onClick={handleFormat} disabled={formatting}>
                        <AlignLeft size={14} />
                        {formatting ? t('editor.formatting') : t('editor.format')}
                    </Button>
                    <Button
                        variant="soft"
                        size="2"
                        color={validationResult?.valid ? 'green' : validationResult?.valid === false ? 'red' : 'gray'}
                        onClick={handleValidate}
                        disabled={validating}
                    >
                        {validationResult?.valid ? <Check size={14} /> : <X size={14} />}
                        {validating ? t('editor.validating') : t('editor.validate')}
                    </Button>
                    <Button variant="soft" size="2" onClick={handleReset} disabled={!hasChanges}>
                        <RefreshCw size={14} />
                        {t('editor.reset')}
                    </Button>
                    <Button
                        size="2"
                        onClick={() => handleSave(false)}
                        disabled={saving || !hasChanges}
                        style={{
                            background: 'linear-gradient(135deg, #10b981, #059669)',
                            cursor: saving || !hasChanges ? 'not-allowed' : 'pointer',
                        }}
                    >
                        <Save size={14} />
                        {saving ? t('common.saving') : t('editor.save')}
                    </Button>
                    <Button
                        size="2"
                        color="blue"
                        onClick={() => handleSave(true)}
                        disabled={saving || !hasChanges}
                    >
                        <RefreshCw size={14} />
                        {t('editor.save_reload')}
                    </Button>
                </Flex>
            </Flex>

            {message && (
                <Callout.Root
                    color={message.type === 'success' ? 'green' : 'red'}
                    size="1"
                    mb="3"
                >
                    <Callout.Text>{message.text}</Callout.Text>
                </Callout.Root>
            )}

            <Box
                ref={editorRef}
                style={{
                    border: '1px solid var(--cp-border-subtle)',
                    borderRadius: 8,
                    overflow: 'hidden',
                    minHeight: 500,
                    background: 'var(--cp-card)',
                }}
            />
        </Box>
    )
}
