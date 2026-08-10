import React, { useEffect, useState } from 'react'
import Locate from './pages/Locate.jsx'
import Emergency from './pages/Emergency.jsx'
import './App.css'

export default function App() {
  const [page, setPage] = useState('locate')
  // The emergency nav entry only appears if the backend actually mounted
  // emergency routes (i.e. an "emergency" gmlc_clients profile is
  // configured — see internal/api.NewServer). A 404 here means the routes
  // were never registered, not a transient failure — same call the
  // Emergency page itself polls for its own status indicator.
  const [emergencyAvailable, setEmergencyAvailable] = useState(false)

  useEffect(() => {
    let cancelled = false
    fetch('/api/v1/emergency/gmlc/status')
      .then(res => { if (!cancelled) setEmergencyAvailable(res.ok) })
      .catch(() => { if (!cancelled) setEmergencyAvailable(false) })
    return () => { cancelled = true }
  }, [])

  if (!emergencyAvailable) return <Locate />

  return (
    <>
      <nav className="page-nav">
        <button className={page === 'locate' ? 'active' : ''} onClick={() => setPage('locate')}>Locate</button>
        <button className={page === 'emergency' ? 'active' : ''} onClick={() => setPage('emergency')}>Emergency</button>
      </nav>
      {page === 'emergency' ? <Emergency /> : <Locate />}
    </>
  )
}
