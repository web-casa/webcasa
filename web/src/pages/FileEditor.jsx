import { useState, useEffect, useRef, useCallback } from 'react'
import { Box, Flex, Text, Button, Badge } from '@radix-ui/themes'
import { Save, ArrowLeft, FileCode } from 'lucide-react'
import { useSearchParams, useNavigate } from 'react-router'
import { fileManagerAPI } from '../api/index.js'
import { EditorView, basicSetup } from 'codemirror'
import { EditorState } from '@codemirror/state'
import { javascript } from '@codemirror/lang-javascript'
import { oneDark } from '@codemirror/theme-one-dark'
import { useTranslation } from 'react-i18next'

const langByExt = {
    js: 'javascript', jsx: 'javascript', ts: 'javascript', tsx: 'javascript',
    json: 'javascript', mjs: 'javascript', cjs: 'javascript',
}

export default function FileEditor() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [searchParams] = useSearchParams()
    const filePath = searchParams.get('path') || ''
    const [saving, setSaving] = useState(false)
    const [hasChanges, setHasChanges] = useState(false)
    const [message, setMessage] = useState(null)
    const [loading, setLoading] = useState(true)
    const editorRef = useRef(null)
    const viewRef = useRef(null)
    // Use refs for content to avoid stale closures
    const contentRef = useRef('')
    const originalContentRef = useRef('')

    useEffect(() => {
        if (!filePath) return
        loadFile()
    }, [filePath])

    const loadFile = async () => {
        setLoading(true)
        try {
            const res = await fileManagerAPI.read(filePath)
            const text = res.data.content || ''
            contentRef.current = text
            originalContentRef.current = text
            setHasChanges(false)
            setMessage(null)
            initEditor(text)
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        } finally {
            setLoading(false)
        }
    }

    const initEditor = useCallback((text) => {
        if (!editorRef.current) return
        if (viewRef.current) {
            viewRef.current.destroy()
            viewRef.current = null
        }

        const updateListener = EditorView.updateListener.of((update) => {
            if (update.docChanged) {
                const newContent = update.state.doc.toString()
                contentRef.current = newContent
                setHasChanges(newContent !== originalContentRef.current)
            }
        })

        const ext = filePath.split('.').pop()?.toLowerCase()
        const extensions = [basicSetup, updateListener, oneDark, EditorView.lineWrapping]
        if (langByExt[ext]) {
            extensions.push(javascript({ jsx: ext === 'jsx' || ext === 'tsx', typescript: ext === 'ts' || ext === 'tsx' }))
        }

        const state = EditorState.create({ doc: text, extensions })
        viewRef.current = new EditorView({ state, parent: editorRef.current })
    }, [filePath])

    // Cleanup CodeMirror on unmount
    useEffect(() => {
        return () => {
            if (viewRef.current) {
                viewRef.current.destroy()
                viewRef.current = null
            }
        }
    }, [])

    // Re-init editor when ref mounts
    useEffect(() => {
        if (editorRef.current && contentRef.current && !viewRef.current) {
            initEditor(contentRef.current)
        }
    }, [loading])

    const handleSave = useCallback(async () => {
        if (!filePath || saving) return
        setSaving(true)
        try {
            await fileManagerAPI.write(filePath, contentRef.current)
            originalContentRef.current = contentRef.current
            setHasChanges(false)
            setMessage({ type: 'success', text: t('common.save_success') })
            setTimeout(() => setMessage(null), 3000)
        } catch (e) {
            setMessage({ type: 'error', text: e.response?.data?.error || e.message })
        } finally {
            setSaving(false)
        }
    }, [filePath, saving, t])

    // Ctrl+S save
    useEffect(() => {
        const handler = (e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 's') {
                e.preventDefault()
                handleSave()
            }
        }
        window.addEventListener('keydown', handler)
        return () => window.removeEventListener('keydown', handler)
    }, [handleSave])

    const fileName = filePath.split('/').pop() || filePath

    return (
        <Box>
            <Flex justify="between" align="center" mb="3" wrap="wrap" gap="2">
                <Flex align="center" gap="2">
                    <Button size="2" variant="ghost" onClick={() => navigate('/files')}>
                        <ArrowLeft size={16} />
                    </Button>
                    <FileCode size={18} />
                    <Text size="4" weight="bold">{fileName}</Text>
                    <Text size="2" color="gray" style={{ fontFamily: 'monospace' }}>{filePath}</Text>
                    {hasChanges && <Badge color="orange" size="1">{t('editor.unsaved')}</Badge>}
                </Flex>
                <Flex gap="2">
                    <Button size="2" onClick={handleSave} disabled={saving || !hasChanges}>
                        <Save size={14} /> {saving ? t('common.saving') : t('common.save')}
                    </Button>
                </Flex>
            </Flex>

            {message && (
                <Box mb="2" p="2" style={{
                    background: message.type === 'error' ? 'var(--red-3)' : 'var(--green-3)',
                    borderRadius: 6,
                    color: message.type === 'error' ? 'var(--red-11)' : 'var(--green-11)',
                }}>
                    <Text size="2">{message.text}</Text>
                </Box>
            )}

            {loading ? (
                <Text size="2" color="gray">{t('common.loading')}</Text>
            ) : (
                <Box
                    ref={editorRef}
                    style={{
                        border: '1px solid var(--gray-6)',
                        borderRadius: 8,
                        overflow: 'hidden',
                        minHeight: 500,
                    }}
                />
            )}
        </Box>
    )
}
