import { useState, useEffect, useRef, useCallback } from 'react'
import { Box, Flex, Text, Button, Badge, Callout } from '@radix-ui/themes'
import { Save, Check, X, FileCode, AlignLeft, RefreshCw } from 'lucide-react'
import { caddyAPI } from '../api/index.js'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState } from '@codemirror/state'
import { oneDark } from '@codemirror/theme-one-dark'

export default function CaddyfileEditor() {
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
            setMessage({ type: 'error', text: '加载 Caddyfile 失败' })
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
            setMessage({ type: 'success', text: '格式化完成' })
        } catch (e) {
            setMessage({ type: 'error', text: '格式化失败' })
        }
        setFormatting(false)
    }

    const handleValidate = async () => {
        setValidating(true)
        try {
            const res = await caddyAPI.validate(content)
            setValidationResult(res.data)
            if (res.data.valid) {
                setMessage({ type: 'success', text: '✅ 语法验证通过' })
            } else {
                setMessage({ type: 'error', text: res.data.error })
            }
        } catch (e) {
            setMessage({ type: 'error', text: '验证失败' })
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
                    ? (res.data.reload_error ? `已保存，但重载失败：${res.data.reload_error}` : '保存并重载成功')
                    : '保存成功',
            })
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || '保存失败' })
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
                            Caddyfile 编辑器
                        </Text>
                        <Text size="2" color="gray" as="p">
                            直接编辑 Caddy 配置文件
                        </Text>
                    </Box>
                </Flex>
                <Flex gap="2" align="center">
                    {hasChanges && (
                        <Badge color="yellow" variant="soft" size="1">
                            未保存
                        </Badge>
                    )}
                    <Button variant="soft" size="2" onClick={handleFormat} disabled={formatting}>
                        <AlignLeft size={14} />
                        {formatting ? '格式化中...' : '格式化'}
                    </Button>
                    <Button
                        variant="soft"
                        size="2"
                        color={validationResult?.valid ? 'green' : validationResult?.valid === false ? 'red' : 'gray'}
                        onClick={handleValidate}
                        disabled={validating}
                    >
                        {validationResult?.valid ? <Check size={14} /> : <X size={14} />}
                        {validating ? '验证中...' : '验证'}
                    </Button>
                    <Button variant="soft" size="2" onClick={handleReset} disabled={!hasChanges}>
                        <RefreshCw size={14} />
                        重置
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
                        {saving ? '保存中...' : '保存'}
                    </Button>
                    <Button
                        size="2"
                        color="blue"
                        onClick={() => handleSave(true)}
                        disabled={saving || !hasChanges}
                    >
                        <RefreshCw size={14} />
                        保存并重载
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
