import { BrowserRouter, Routes, Route, Navigate } from 'react-router'
import { useEffect } from 'react'
import { useAuthStore } from './stores/auth.js'
import Login from './pages/Login.jsx'
import Layout from './pages/Layout.jsx'
import Dashboard from './pages/Dashboard.jsx'
import HostList from './pages/HostList.jsx'
import Logs from './pages/Logs.jsx'
import Settings from './pages/Settings.jsx'
import Users from './pages/Users.jsx'
import AuditLogs from './pages/AuditLogs.jsx'
import CaddyfileEditor from './pages/CaddyfileEditor.jsx'
import DnsProviders from './pages/DnsProviders.jsx'
import Certificates from './pages/Certificates.jsx'
import Templates from './pages/Templates.jsx'
import Plugins from './pages/Plugins.jsx'
import DockerOverview from './pages/DockerOverview.jsx'
import DockerContainers from './pages/DockerContainers.jsx'
import DockerImages from './pages/DockerImages.jsx'
import DockerNetworks from './pages/DockerNetworks.jsx'
import DockerVolumes from './pages/DockerVolumes.jsx'
import ProjectList from './pages/ProjectList.jsx'
import ProjectCreate from './pages/ProjectCreate.jsx'
import ProjectDetail from './pages/ProjectDetail.jsx'
import AIConfig from './pages/AIConfig.jsx'
import AIChatWidget from './pages/AIChatWidget.jsx'
import FileManager from './pages/FileManager.jsx'
import FileEditor from './pages/FileEditor.jsx'
import WebTerminal from './pages/WebTerminal.jsx'

function ProtectedRoute({ children }) {
    const token = useAuthStore((s) => s.token)
    if (!token) return <Navigate to="/login" replace />
    return children
}

export default function App() {
    const { token, checkSetup, fetchMe } = useAuthStore()

    useEffect(() => {
        checkSetup()
        if (token) fetchMe()
    }, [])

    return (
        <BrowserRouter>
            <Routes>
                <Route path="/login" element={<Login />} />
                <Route
                    path="/"
                    element={
                        <ProtectedRoute>
                            <Layout />
                            <AIChatWidget />
                        </ProtectedRoute>
                    }
                >
                    <Route index element={<Dashboard />} />
                    <Route path="hosts" element={<HostList />} />
                    <Route path="logs" element={<Logs />} />
                    <Route path="users" element={<Users />} />
                    <Route path="audit" element={<AuditLogs />} />
                    <Route path="editor" element={<CaddyfileEditor />} />
                    <Route path="dns" element={<DnsProviders />} />
                    <Route path="certificates" element={<Certificates />} />
                    <Route path="templates" element={<Templates />} />
                    <Route path="docker" element={<DockerOverview />} />
                    <Route path="docker/containers" element={<DockerContainers />} />
                    <Route path="docker/images" element={<DockerImages />} />
                    <Route path="docker/networks" element={<DockerNetworks />} />
                    <Route path="docker/volumes" element={<DockerVolumes />} />
                    <Route path="deploy" element={<ProjectList />} />
                    <Route path="deploy/create" element={<ProjectCreate />} />
                    <Route path="deploy/:id" element={<ProjectDetail />} />
                    <Route path="plugins" element={<Plugins />} />
                    <Route path="ai/config" element={<AIConfig />} />
                    <Route path="files" element={<FileManager />} />
                    <Route path="files/edit" element={<FileEditor />} />
                    <Route path="terminal" element={<WebTerminal />} />
                    <Route path="settings" element={<Settings />} />
                </Route>
                <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
        </BrowserRouter>
    )
}
