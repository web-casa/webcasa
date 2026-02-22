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
                    <Route path="settings" element={<Settings />} />
                </Route>
                <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
        </BrowserRouter>
    )
}
