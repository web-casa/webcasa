import { useState, useEffect, useCallback, useRef } from 'react'
import { Box, Flex, Text, Button, Table, Badge, Dialog, TextField, Select, DropdownMenu, Checkbox } from '@radix-ui/themes'
import {
    FolderOpen, File, ChevronRight, Home, Upload, FolderPlus,
    Download, Trash2, Pencil, Archive, Lock, ArrowUp, MoreHorizontal,
    FileText, FileCode, Image, Music, Video, Database, Package as PackageIcon
} from 'lucide-react'
import { useNavigate } from 'react-router'
import { fileManagerAPI } from '../api/index.js'
import { useTranslation } from 'react-i18next'

function getFileIcon(name, isDir) {
    if (isDir) return FolderOpen
    const ext = name.split('.').pop()?.toLowerCase()
    const lowerName = name.toLowerCase()
    const codeExts = ['js', 'jsx', 'ts', 'tsx', 'go', 'py', 'rs', 'java', 'c', 'cpp', 'h', 'css', 'scss', 'html', 'vue', 'svelte', 'json', 'yaml', 'yml', 'toml', 'xml', 'sh', 'bash', 'zsh', 'sql', 'rb', 'php', 'conf', 'cfg', 'ini', 'env']
    const codeNames = ['caddyfile', 'dockerfile', 'makefile', 'vagrantfile', 'gemfile', 'rakefile', '.gitignore', '.env']
    const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'ico', 'bmp']
    const audioExts = ['mp3', 'wav', 'flac', 'ogg', 'aac']
    const videoExts = ['mp4', 'mkv', 'avi', 'mov', 'webm']
    const archiveExts = ['zip', 'tar', 'gz', 'bz2', 'xz', '7z', 'rar']
    const dbExts = ['db', 'sqlite', 'sqlite3']

    if (codeExts.includes(ext) || codeNames.includes(lowerName)) return FileCode
    if (imageExts.includes(ext)) return Image
    if (audioExts.includes(ext)) return Music
    if (videoExts.includes(ext)) return Video
    if (archiveExts.includes(ext)) return PackageIcon
    if (dbExts.includes(ext)) return Database
    if (['md', 'txt', 'log', 'csv'].includes(ext)) return FileText
    return File
}

function formatSize(bytes) {
    if (bytes === 0) return '—'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i]
}

function formatTime(t) {
    if (!t) return '—'
    return new Date(t).toLocaleString()
}

export default function FileManager() {
    const { t } = useTranslation()
    const navigate = useNavigate()
    const [currentPath, setCurrentPath] = useState('/')
    const [files, setFiles] = useState([])
    const [loading, setLoading] = useState(false)
    const [selected, setSelected] = useState(new Set())
    const [error, setError] = useState(null)
    const uploadRef = useRef(null)

    // Dialogs
    const [mkdirOpen, setMkdirOpen] = useState(false)
    const [mkdirName, setMkdirName] = useState('')
    const [renameOpen, setRenameOpen] = useState(false)
    const [renameTarget, setRenameTarget] = useState(null)
    const [renameNewName, setRenameNewName] = useState('')
    const [chmodOpen, setChmodOpen] = useState(false)
    const [chmodTarget, setChmodTarget] = useState(null)
    const [chmodMode, setChmodMode] = useState('')
    const [compressOpen, setCompressOpen] = useState(false)
    const [compressName, setCompressName] = useState('')
    const [compressFormat, setCompressFormat] = useState('tar.gz')
    const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)

    const loadFiles = useCallback(async (path) => {
        setLoading(true)
        setError(null)
        try {
            const res = await fileManagerAPI.list(path)
            const items = res.data.files || []
            items.sort((a, b) => {
                if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
                return a.name.localeCompare(b.name)
            })
            setFiles(items)
            setCurrentPath(path)
            setSelected(new Set())
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        } finally {
            setLoading(false)
        }
    }, [])

    useEffect(() => {
        loadFiles('/')
    }, [loadFiles])

    const navigateTo = (path) => {
        loadFiles(path)
    }

    const goUp = () => {
        if (currentPath === '/') return
        const parts = currentPath.split('/').filter(Boolean)
        parts.pop()
        navigateTo('/' + parts.join('/') || '/')
    }

    const handleDoubleClick = (file) => {
        if (file.is_dir) {
            navigateTo(file.path)
        } else {
            navigate(`/files/edit?path=${encodeURIComponent(file.path)}`)
        }
    }

    const toggleSelect = (path) => {
        setSelected(prev => {
            const next = new Set(prev)
            if (next.has(path)) next.delete(path)
            else next.add(path)
            return next
        })
    }

    const toggleSelectAll = () => {
        if (selected.size === files.length) {
            setSelected(new Set())
        } else {
            setSelected(new Set(files.map(f => f.path)))
        }
    }

    // Upload
    const handleUpload = async (e) => {
        const fileList = e.target.files
        if (!fileList?.length) return
        for (const file of fileList) {
            const formData = new FormData()
            formData.append('file', file)
            formData.append('path', currentPath.endsWith('/') ? currentPath : currentPath + '/')
            try {
                await fileManagerAPI.upload(formData)
            } catch (err) {
                setError(err.response?.data?.error || err.message)
            }
        }
        e.target.value = ''
        loadFiles(currentPath)
    }

    // Mkdir
    const handleMkdir = async () => {
        if (!mkdirName.trim()) return
        const path = (currentPath === '/' ? '/' : currentPath + '/') + mkdirName.trim()
        try {
            await fileManagerAPI.mkdir(path)
            setMkdirOpen(false)
            setMkdirName('')
            loadFiles(currentPath)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
    }

    // Delete
    const handleDelete = async () => {
        const paths = [...selected]
        if (!paths.length) return
        try {
            await fileManagerAPI.delete(paths)
            setDeleteConfirmOpen(false)
            loadFiles(currentPath)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
    }

    // Rename
    const handleRename = async () => {
        if (!renameTarget || !renameNewName.trim()) return
        const parentPath = renameTarget.path.substring(0, renameTarget.path.lastIndexOf('/')) || '/'
        const newPath = (parentPath === '/' ? '/' : parentPath + '/') + renameNewName.trim()
        try {
            await fileManagerAPI.rename(renameTarget.path, newPath)
            setRenameOpen(false)
            loadFiles(currentPath)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
    }

    // Chmod
    const handleChmod = async () => {
        if (!chmodTarget || !chmodMode.trim()) return
        try {
            await fileManagerAPI.chmod(chmodTarget.path, chmodMode.trim())
            setChmodOpen(false)
            loadFiles(currentPath)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
    }

    // Compress
    const handleCompress = async () => {
        const paths = [...selected]
        if (!paths.length || !compressName.trim()) return
        const dest = (currentPath === '/' ? '/' : currentPath + '/') + compressName.trim()
        try {
            await fileManagerAPI.compress(paths, dest, compressFormat)
            setCompressOpen(false)
            setCompressName('')
            loadFiles(currentPath)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
    }

    // Extract
    const handleExtract = async (file) => {
        try {
            await fileManagerAPI.extract(file.path, currentPath)
            loadFiles(currentPath)
        } catch (e) {
            setError(e.response?.data?.error || e.message)
        }
    }

    // Download
    const handleDownload = (file) => {
        const token = localStorage.getItem('token')
        const url = fileManagerAPI.download(file.path)
        const a = document.createElement('a')
        a.href = url + `&token=${token}`
        a.download = file.name
        a.click()
    }

    // Breadcrumbs
    const breadcrumbs = currentPath === '/' ? ['/'] : ['/', ...currentPath.split('/').filter(Boolean)]

    return (
        <Box>
            <Flex justify="between" align="center" mb="4" wrap="wrap" gap="2">
                <Box>
                    <Text size="5" weight="bold">{t('fm.title')}</Text>
                    <Text size="2" color="gray" ml="2">{currentPath}</Text>
                </Box>
                <Flex gap="2" wrap="wrap">
                    <input ref={uploadRef} type="file" multiple style={{ display: 'none' }} onChange={handleUpload} />
                    <Button size="2" variant="soft" onClick={() => uploadRef.current?.click()}>
                        <Upload size={14} /> {t('fm.upload')}
                    </Button>
                    <Button size="2" variant="soft" onClick={() => { setMkdirName(''); setMkdirOpen(true) }}>
                        <FolderPlus size={14} /> {t('fm.new_folder')}
                    </Button>
                    {selected.size > 0 && (
                        <>
                            <Button size="2" variant="soft" color="red" onClick={() => setDeleteConfirmOpen(true)}>
                                <Trash2 size={14} /> {t('common.delete')} ({selected.size})
                            </Button>
                            <Button size="2" variant="soft" onClick={() => { setCompressName('archive.tar.gz'); setCompressFormat('tar.gz'); setCompressOpen(true) }}>
                                <Archive size={14} /> {t('fm.compress')}
                            </Button>
                        </>
                    )}
                </Flex>
            </Flex>

            {error && (
                <Box mb="3" p="2" style={{ background: 'var(--red-3)', borderRadius: 6, color: 'var(--red-11)' }}>
                    <Text size="2">{error}</Text>
                </Box>
            )}

            {/* Breadcrumb */}
            <Flex align="center" gap="1" mb="3" wrap="wrap">
                <Button size="1" variant="ghost" onClick={() => navigateTo('/')}>
                    <Home size={14} />
                </Button>
                {breadcrumbs.slice(1).map((part, i) => {
                    const path = '/' + breadcrumbs.slice(1, i + 2).join('/')
                    return (
                        <Flex key={path} align="center" gap="1">
                            <ChevronRight size={12} style={{ color: 'var(--gray-8)' }} />
                            <Button size="1" variant="ghost" onClick={() => navigateTo(path)}>
                                {part}
                            </Button>
                        </Flex>
                    )
                })}
                {currentPath !== '/' && (
                    <Button size="1" variant="ghost" onClick={goUp} ml="2">
                        <ArrowUp size={14} /> ..
                    </Button>
                )}
            </Flex>

            {/* File table */}
            <Table.Root variant="surface">
                <Table.Header>
                    <Table.Row>
                        <Table.ColumnHeaderCell width="40px">
                            <Checkbox checked={files.length > 0 && selected.size === files.length} onCheckedChange={toggleSelectAll} />
                        </Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell>{t('fm.name')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell width="100px">{t('fm.size')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell width="100px">{t('fm.permissions')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell width="80px">{t('fm.owner')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell width="170px">{t('fm.modified')}</Table.ColumnHeaderCell>
                        <Table.ColumnHeaderCell width="60px">{t('common.actions')}</Table.ColumnHeaderCell>
                    </Table.Row>
                </Table.Header>
                <Table.Body>
                    {loading && (
                        <Table.Row>
                            <Table.Cell colSpan={7}>
                                <Text size="2" color="gray">{t('common.loading')}</Text>
                            </Table.Cell>
                        </Table.Row>
                    )}
                    {!loading && files.length === 0 && (
                        <Table.Row>
                            <Table.Cell colSpan={7}>
                                <Text size="2" color="gray">{t('fm.empty_dir')}</Text>
                            </Table.Cell>
                        </Table.Row>
                    )}
                    {!loading && files.map((file) => {
                        const Icon = getFileIcon(file.name, file.is_dir)
                        const isArchive = /\.(tar\.gz|tgz|zip|tar\.bz2|tar\.xz)$/i.test(file.name)
                        return (
                            <Table.Row
                                key={file.path}
                                style={{ cursor: 'pointer' }}
                                onDoubleClick={() => handleDoubleClick(file)}
                            >
                                <Table.Cell>
                                    <Checkbox
                                        checked={selected.has(file.path)}
                                        onCheckedChange={() => toggleSelect(file.path)}
                                        onClick={(e) => e.stopPropagation()}
                                    />
                                </Table.Cell>
                                <Table.Cell>
                                    <Flex align="center" gap="2">
                                        <Icon size={16} style={{ color: file.is_dir ? 'var(--accent-9)' : 'var(--gray-9)', flexShrink: 0 }} />
                                        <Text size="2" weight={file.is_dir ? 'medium' : 'regular'}>
                                            {file.name}
                                        </Text>
                                        {file.is_symlink && <Badge size="1" variant="outline">link</Badge>}
                                    </Flex>
                                </Table.Cell>
                                <Table.Cell>
                                    <Text size="2" color="gray">{file.is_dir ? '—' : formatSize(file.size)}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    <Text size="1" style={{ fontFamily: 'monospace' }}>{file.mode_octal}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    <Text size="2" color="gray">{file.user || '—'}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    <Text size="2" color="gray">{formatTime(file.mod_time)}</Text>
                                </Table.Cell>
                                <Table.Cell>
                                    <DropdownMenu.Root>
                                        <DropdownMenu.Trigger>
                                            <Button size="1" variant="ghost" onClick={(e) => e.stopPropagation()}>
                                                <MoreHorizontal size={14} />
                                            </Button>
                                        </DropdownMenu.Trigger>
                                        <DropdownMenu.Content>
                                            {!file.is_dir && (
                                                <DropdownMenu.Item onClick={() => navigate(`/files/edit?path=${encodeURIComponent(file.path)}`)}>
                                                    <FileCode size={14} /> {t('fm.edit')}
                                                </DropdownMenu.Item>
                                            )}
                                            {!file.is_dir && (
                                                <DropdownMenu.Item onClick={() => handleDownload(file)}>
                                                    <Download size={14} /> {t('common.download')}
                                                </DropdownMenu.Item>
                                            )}
                                            <DropdownMenu.Item onClick={() => { setRenameTarget(file); setRenameNewName(file.name); setRenameOpen(true) }}>
                                                <Pencil size={14} /> {t('fm.rename')}
                                            </DropdownMenu.Item>
                                            <DropdownMenu.Item onClick={() => { setChmodTarget(file); setChmodMode(file.mode_octal); setChmodOpen(true) }}>
                                                <Lock size={14} /> {t('fm.chmod')}
                                            </DropdownMenu.Item>
                                            {isArchive && (
                                                <DropdownMenu.Item onClick={() => handleExtract(file)}>
                                                    <Archive size={14} /> {t('fm.extract')}
                                                </DropdownMenu.Item>
                                            )}
                                            <DropdownMenu.Separator />
                                            <DropdownMenu.Item color="red" onClick={() => { setSelected(new Set([file.path])); setDeleteConfirmOpen(true) }}>
                                                <Trash2 size={14} /> {t('common.delete')}
                                            </DropdownMenu.Item>
                                        </DropdownMenu.Content>
                                    </DropdownMenu.Root>
                                </Table.Cell>
                            </Table.Row>
                        )
                    })}
                </Table.Body>
            </Table.Root>

            {/* Mkdir Dialog */}
            <Dialog.Root open={mkdirOpen} onOpenChange={setMkdirOpen}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('fm.new_folder')}</Dialog.Title>
                    <TextField.Root
                        value={mkdirName}
                        onChange={e => setMkdirName(e.target.value)}
                        placeholder={t('fm.folder_name_placeholder')}
                        onKeyDown={e => e.key === 'Enter' && handleMkdir()}
                        autoFocus
                    />
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleMkdir}>{t('common.create')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Rename Dialog */}
            <Dialog.Root open={renameOpen} onOpenChange={setRenameOpen}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('fm.rename')}</Dialog.Title>
                    <TextField.Root
                        value={renameNewName}
                        onChange={e => setRenameNewName(e.target.value)}
                        onKeyDown={e => e.key === 'Enter' && handleRename()}
                        autoFocus
                    />
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleRename}>{t('common.save')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Chmod Dialog */}
            <Dialog.Root open={chmodOpen} onOpenChange={setChmodOpen}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('fm.chmod')}</Dialog.Title>
                    <Text size="2" color="gray" mb="2">{chmodTarget?.path}</Text>
                    <TextField.Root
                        value={chmodMode}
                        onChange={e => setChmodMode(e.target.value)}
                        placeholder="0755"
                        onKeyDown={e => e.key === 'Enter' && handleChmod()}
                        autoFocus
                    />
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleChmod}>{t('common.save')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Compress Dialog */}
            <Dialog.Root open={compressOpen} onOpenChange={setCompressOpen}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('fm.compress')}</Dialog.Title>
                    <Flex direction="column" gap="3">
                        <TextField.Root
                            value={compressName}
                            onChange={e => setCompressName(e.target.value)}
                            placeholder="archive.tar.gz"
                        />
                        <Select.Root value={compressFormat} onValueChange={setCompressFormat}>
                            <Select.Trigger />
                            <Select.Content>
                                <Select.Item value="tar.gz">tar.gz</Select.Item>
                                <Select.Item value="zip">zip</Select.Item>
                            </Select.Content>
                        </Select.Root>
                    </Flex>
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button onClick={handleCompress}>{t('fm.compress')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>

            {/* Delete Confirm */}
            <Dialog.Root open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
                <Dialog.Content maxWidth="400px">
                    <Dialog.Title>{t('common.delete')}</Dialog.Title>
                    <Text size="2">{t('fm.confirm_delete', { count: selected.size })}</Text>
                    <Flex justify="end" gap="2" mt="4">
                        <Dialog.Close><Button variant="soft" color="gray">{t('common.cancel')}</Button></Dialog.Close>
                        <Button color="red" onClick={handleDelete}>{t('common.delete')}</Button>
                    </Flex>
                </Dialog.Content>
            </Dialog.Root>
        </Box>
    )
}
