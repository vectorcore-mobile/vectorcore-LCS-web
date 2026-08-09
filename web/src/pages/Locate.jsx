import React, { useEffect, useRef, useState } from 'react'
import { api } from '../api.js'
import LocationMap from '../components/LocationMap.jsx'

const TERMINAL_STATES = new Set(['completed', 'failed', 'cancelled', 'expired', 'indeterminate'])
const POLL_INTERVAL_MS = 2000
const POLL_TIMEOUT_MS = 90000
const GMLC_STATUS_POLL_MS = 15000
const HISTORY_KEY = 'lcs-request-history'
const HISTORY_LIMIT = 20

// The GMLC API has no list/search endpoint (see api.md), so "recent
// requests" is a client-side record of ids this browser has submitted.
function loadHistory() {
  try { return JSON.parse(localStorage.getItem(HISTORY_KEY)) || [] } catch { return [] }
}
function saveHistory(list) {
  try { localStorage.setItem(HISTORY_KEY, JSON.stringify(list.slice(0, HISTORY_LIMIT))) } catch { /* ignore */ }
}

function StateBadge({ state }) {
  return <span className={`badge state-${state}`}>{state}</span>
}

export default function Locate() {
  const [idType, setIdType] = useState('msisdn')
  const [idValue, setIdValue] = useState('')
  const [locationType, setLocationType] = useState('current')
  const [status, setStatus] = useState(null)
  const [error, setError] = useState(null)
  const [submitting, setSubmitting] = useState(false)
  const [gmlcStatus, setGmlcStatus] = useState(null)
  const [history, setHistory] = useState(loadHistory)
  const pollRef = useRef(null)

  useEffect(() => {
    let cancelled = false
    const check = () => api.gmlcStatus().then(s => { if (!cancelled) setGmlcStatus(s) }).catch(() => {})
    check()
    const t = setInterval(check, GMLC_STATUS_POLL_MS)
    return () => { cancelled = true; clearInterval(t) }
  }, [])

  useEffect(() => () => clearTimeout(pollRef.current), [])

  function recordHistory(id, target) {
    const entry = { id, target, at: new Date().toISOString() }
    const next = [entry, ...history.filter(e => e.id !== id)].slice(0, HISTORY_LIMIT)
    setHistory(next)
    saveHistory(next)
  }

  function pollUntilTerminal(id, startedAt) {
    api.getLocationRequest(id).then(s => {
      setStatus(s)
      if (TERMINAL_STATES.has(s.state) || Date.now() - startedAt > POLL_TIMEOUT_MS) return
      pollRef.current = setTimeout(() => pollUntilTerminal(id, startedAt), POLL_INTERVAL_MS)
    }).catch(err => setError(err.message))
  }

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)
    setStatus(null)
    clearTimeout(pollRef.current)

    const value = idValue.trim()
    if (!/^\d{1,15}$/.test(value)) {
      setError(`${idType.toUpperCase()} must be 1-15 decimal digits`)
      return
    }

    const target = idType === 'imsi' ? { imsi: value } : { msisdn: value }
    setSubmitting(true)
    try {
      const s = await api.submitLocationRequest({
        target,
        service_type: 'immediate',
        location_type: locationType,
      })
      setStatus(s)
      recordHistory(s.id, target)
      if (!TERMINAL_STATES.has(s.state)) {
        pollRef.current = setTimeout(() => pollUntilTerminal(s.id, Date.now()), POLL_INTERVAL_MS)
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setSubmitting(false)
    }
  }

  function reopen(id) {
    clearTimeout(pollRef.current)
    setError(null)
    setStatus(null)
    api.getLocationRequest(id).then(s => {
      setStatus(s)
      if (!TERMINAL_STATES.has(s.state)) {
        pollRef.current = setTimeout(() => pollUntilTerminal(id, Date.now()), POLL_INTERVAL_MS)
      }
    }).catch(err => setError(err.message))
  }

  async function handleCancel() {
    if (!status) return
    try {
      const s = await api.cancelLocationRequest(status.id)
      setStatus(s)
      clearTimeout(pollRef.current)
    } catch (err) {
      setError(err.message)
    }
  }

  const result = status?.result
  const hasPoint = result && result.latitude != null && result.longitude != null
  const canCancel = status && !TERMINAL_STATES.has(status.state)

  return (
    <div className="page">
      <header className="topbar">
        <div>
          <div className="logo-title">VectorCore LCS</div>
          <div className="logo-sub">UE Location Console</div>
        </div>
        <div className="gmlc-indicator">
          <span className={`dot ${gmlcStatus == null ? 'dot-warn' : gmlcStatus.reachable ? (gmlcStatus.ready ? 'dot-ok' : 'dot-warn') : 'dot-err'}`} />
          {gmlcStatus == null ? 'checking GMLC…' : !gmlcStatus.reachable ? 'GMLC unreachable' : gmlcStatus.ready ? 'GMLC ready' : 'GMLC not ready'}
        </div>
      </header>

      <div className="layout-grid">
        <div className="col-main">
          <form className="card" onSubmit={handleSubmit}>
            <div className="form-row">
              <div style={{ width: 140 }}>
                <label>Identifier</label>
                <select value={idType} onChange={e => setIdType(e.target.value)}>
                  <option value="imsi">IMSI</option>
                  <option value="msisdn">MSISDN</option>
                </select>
              </div>
              <div style={{ flex: 1 }}>
                <label>{idType.toUpperCase()}</label>
                <input
                  type="text"
                  inputMode="numeric"
                  placeholder={idType === 'imsi' ? '311435000070572' : '15551234567'}
                  value={idValue}
                  onChange={e => setIdValue(e.target.value)}
                />
              </div>
              <div style={{ width: 200 }}>
                <label>Location Type</label>
                <select value={locationType} onChange={e => setLocationType(e.target.value)}>
                  <option value="current">Current</option>
                  <option value="current_or_last_known">Current or Last Known</option>
                </select>
              </div>
            </div>
            <div className="form-actions">
              <button type="submit" className="primary" disabled={submitting || !idValue.trim()}>
                {submitting ? 'Requesting…' : 'Locate UE'}
              </button>
              {canCancel && (
                <button type="button" className="danger" onClick={handleCancel}>Cancel Request</button>
              )}
            </div>
          </form>

          {error && <div className="card error-card">{error}</div>}

          {status && (
            <div className="card status-card">
              <div className="status-head">
                <StateBadge state={status.state} />
                <code>{status.id}</code>
              </div>
              <dl className="status-fields">
                <dt>Service Type</dt><dd>{status.service_type}</dd>
                <dt>Location Type</dt><dd>{status.location_type}</dd>
                {status.failure_code && (<><dt>Failure</dt><dd>{status.failure_code}</dd></>)}
                <dt>Created</dt><dd>{status.created_at}</dd>
                <dt>Updated</dt><dd>{status.updated_at}</dd>
                {result?.shape && (<><dt>Shape</dt><dd>{result.shape}</dd></>)}
                {hasPoint && (<><dt>Position</dt><dd>{result.latitude.toFixed(6)}, {result.longitude.toFixed(6)}</dd></>)}
                {result?.uncertainty_meters != null && (<><dt>Uncertainty</dt><dd>{result.uncertainty_meters.toFixed(1)} m</dd></>)}
                {result?.ecgi && (<><dt>ECGI</dt><dd>{result.ecgi}</dd></>)}
              </dl>
              {status.state === 'completed' && !hasPoint && (
                <p className="muted-note">Completed with no single-point geometry (ECGI-only or polygon shape).</p>
              )}
            </div>
          )}

          {hasPoint && <LocationMap result={result} />}
        </div>

        <aside className="col-side">
          <div className="card">
            <div className="side-title">Recent Requests</div>
            {history.length === 0 && <div className="muted-note">None yet this session.</div>}
            <ul className="history-list">
              {history.map(h => (
                <li key={h.id}>
                  <button type="button" className="history-item" onClick={() => reopen(h.id)}>
                    <span>{h.target.imsi ? `IMSI ${h.target.imsi}` : `MSISDN ${h.target.msisdn}`}</span>
                    <code>{h.id.slice(0, 8)}</code>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        </aside>
      </div>
    </div>
  )
}
