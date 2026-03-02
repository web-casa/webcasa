import { BrowserRouter, Routes, Route, Navigate } from 'react-router'
import { useEffect } from 'react'
import { useAuthStore } from './stores/auth.js'
import Login from './pages/Login.jsx'
import Layout from './pages/Layout.jsx'
import Dashboard from './pages/Dashboard.jsx'
import HostList from './pages/HostList.jsx'
import Settings from './pages/Settings.jsx'
import CaddyfileEditor from './pages/CaddyfileEditor.jsx'
import DockerOverview from './pages/DockerOverview.jsx'
import ProjectList from './pages/ProjectList.jsx'
import ProjectCreate from './pages/ProjectCreate.jsx'
import ProjectDetail from './pages/ProjectDetail.jsx'
import AIChatWidget from './pages/AIChatWidget.jsx'
import FileManager from './pages/FileManager.jsx'
import FileEditor from './pages/FileEditor.jsx'
import WebTerminal from './pages/WebTerminal.jsx'
import DatabaseInstances from './pages/DatabaseInstances.jsx'
import DatabaseDetail from './pages/DatabaseDetail.jsx'
import DatabaseQuery from './pages/DatabaseQuery.jsx'
import SQLiteBrowser from './pages/SQLiteBrowser.jsx'
import MonitoringDashboard from './pages/MonitoringDashboard.jsx'
import BackupManager from './pages/BackupManager.jsx'
import AppStore from './pages/AppStore.jsx'
import AppDetail from './pages/AppDetail.jsx'
import TemplateMarket from './pages/TemplateMarket.jsx'

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
                    <Route path="editor" element={<CaddyfileEditor />} />
                    <Route path="docker" element={<DockerOverview />} />
                    <Route path="deploy" element={<ProjectList />} />
                    <Route path="deploy/create" element={<ProjectCreate />} />
                    <Route path="deploy/:id" element={<ProjectDetail />} />
                    <Route path="files" element={<FileManager />} />
                    <Route path="files/edit" element={<FileEditor />} />
                    <Route path="terminal" element={<WebTerminal />} />
                    <Route path="database" element={<DatabaseInstances />} />
                    <Route path="database/sqlite" element={<SQLiteBrowser />} />
                    <Route path="database/query" element={<DatabaseQuery />} />
                    <Route path="database/:id" element={<DatabaseDetail />} />
                    <Route path="settings" element={<Settings />} />
                    <Route path="monitoring" element={<MonitoringDashboard />} />
                    <Route path="backup" element={<BackupManager />} />
                    <Route path="store" element={<AppStore />} />
                    <Route path="store/app/:id" element={<AppDetail />} />
                    <Route path="store/templates" element={<TemplateMarket />} />

                    {/* Redirects: old standalone pages → Settings */}
                    <Route path="logs" element={<Navigate to="/settings" replace />} />
                    <Route path="users" element={<Navigate to="/settings" replace />} />
                    <Route path="audit" element={<Navigate to="/settings" replace />} />
                    <Route path="dns" element={<Navigate to="/settings" replace />} />
                    <Route path="certificates" element={<Navigate to="/settings" replace />} />
                    <Route path="templates" element={<Navigate to="/settings" replace />} />
                    <Route path="plugins" element={<Navigate to="/settings" replace />} />
                    <Route path="ai/config" element={<Navigate to="/settings" replace />} />
                    <Route path="docker/containers" element={<Navigate to="/docker" replace />} />
                    <Route path="docker/images" element={<Navigate to="/docker" replace />} />
                    <Route path="docker/networks" element={<Navigate to="/docker" replace />} />
                    <Route path="docker/volumes" element={<Navigate to="/docker" replace />} />
                </Route>
                <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
        </BrowserRouter>
    )
}
