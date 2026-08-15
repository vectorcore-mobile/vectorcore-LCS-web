import React, { useEffect, useRef, useState } from 'react'
import LocationMap from './LocationMap.jsx'

const TERMINAL_STATES = new Set(['completed', 'failed', 'cancelled', 'expired', 'indeterminate'])
const POLL_INTERVAL_MS = 2000
const POLL_TIMEOUT_MS = 90000
const GMLC_STATUS_POLL_MS = 15000

// buildQos assembles gmlc.SubmitRequest's qos object from the form's
// individually-optional fields, omitting anything left unset — every
// child field of qos is independently optional. Returns undefined (not
// {}) when nothing was set, so a submit with no QoS preferences sends no
// qos key at all.
function buildQos({ qosClass, horizontalAccuracy, verticalAccuracy, verticalRequested, responseTime }) {
  const qos = {}
  if (qosClass) qos.class = qosClass
  if (horizontalAccuracy !== '') qos.horizontal_accuracy_meters = Number(horizontalAccuracy)
  if (verticalAccuracy !== '') qos.vertical_accuracy_meters = Number(verticalAccuracy)
  if (verticalRequested) qos.vertical_requested = true
  if (responseTime) qos.response_time = responseTime
  return Object.keys(qos).length ? qos : undefined
}

function StateBadge({ state }) {
  return <span className={`badge state-${state}`}>{state}</span>
}

// LocationConsole is the console's core submit/poll/history UI,
// parameterized by which GMLC client profile it talks to (see L3 in
// docs/mlp-le-interface-plan.md) — Locate.jsx and Emergency.jsx are both
// thin wrappers around this, bound to different api instances.
export default function LocationConsole({ api, title, subtitle }) {
  const [idType, setIdType] = useState('msisdn')
  const [idValue, setIdValue] = useState('')
  const [locationType, setLocationType] = useState('current')
  const [qosClass, setQosClass] = useState('')
  const [horizontalAccuracy, setHorizontalAccuracy] = useState('')
  const [verticalAccuracy, setVerticalAccuracy] = useState('')
  const [verticalRequested, setVerticalRequested] = useState(false)
  const [responseTime, setResponseTime] = useState('')
  const [status, setStatus] = useState(null)
  const [error, setError] = useState(null)
  const [submitting, setSubmitting] = useState(false)
  const [gmlcStatus, setGmlcStatus] = useState(null)
  const pollRef = useRef(null)

  // History is queried live from GMLC's own Historic Location Immediate
  // service (see api.getHistory) for whichever identifier is currently
  // entered in the form above — it is no longer a browser-local record of
  // this session's own submitted request ids.
  const [historyWindowHours, setHistoryWindowHours] = useState('24')
  const [historyPoints, setHistoryPoints] = useState(null)
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState(null)
  const [previewPoint, setPreviewPoint] = useState(null)

  useEffect(() => {
    let cancelled = false
    const check = () => api.gmlcStatus().then(s => { if (!cancelled) setGmlcStatus(s) }).catch(() => {})
    check()
    const t = setInterval(check, GMLC_STATUS_POLL_MS)
    return () => { cancelled = true; clearInterval(t) }
  }, [api])

  useEffect(() => () => clearTimeout(pollRef.current), [])

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
    setPreviewPoint(null)
    clearTimeout(pollRef.current)

    const value = idValue.trim()
    if (!/^\d{1,15}$/.test(value)) {
      setError(`${idType.toUpperCase()} must be 1-15 decimal digits`)
      return
    }

    const target = idType === 'imsi' ? { imsi: value } : { msisdn: value }
    setSubmitting(true)
    try {
      const qos = buildQos({ qosClass, horizontalAccuracy, verticalAccuracy, verticalRequested, responseTime })
      const s = await api.submitLocationRequest({
        target,
        service_type: 'immediate',
        location_type: locationType,
        ...(qos && { qos }),
      })
      setStatus(s)
      if (!TERMINAL_STATES.has(s.state)) {
        pollRef.current = setTimeout(() => pollUntilTerminal(s.id, Date.now()), POLL_INTERVAL_MS)
      }
    } catch (err) {
      setError(err.message)
    } finally {
      setSubmitting(false)
    }
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

  async function viewHistory() {
    const value = idValue.trim()
    if (!/^\d{1,15}$/.test(value)) {
      setHistoryError(`${idType.toUpperCase()} must be 1-15 decimal digits`)
      return
    }
    setHistoryError(null)
    setHistoryLoading(true)
    setPreviewPoint(null)
    try {
      const start = new Date(Date.now() - Number(historyWindowHours) * 3600_000).toISOString()
      const { points } = await api.getHistory({ targetKind: idType, targetValue: value, start })
      setHistoryPoints(points || [])
    } catch (err) {
      setHistoryError(err.message)
      setHistoryPoints(null)
    } finally {
      setHistoryLoading(false)
    }
  }

  const result = previewPoint || status?.result
  const hasPoint = result && result.latitude != null && result.longitude != null
  const canCancel = status && !TERMINAL_STATES.has(status.state)

  return (
    <div className="page">
      <header className="topbar">
        <div>
          <div className="logo-title">{title}</div>
          <div className="logo-sub">{subtitle}</div>
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
            <details className="qos-details">
              <summary>QoS (optional)</summary>
              <div className="form-row">
                <div style={{ width: 160 }}>
                  <label>Class</label>
                  <select value={qosClass} onChange={e => setQosClass(e.target.value)}>
                    <option value="">(unset)</option>
                    <option value="assured">Assured</option>
                    <option value="best_effort">Best Effort</option>
                  </select>
                </div>
                <div style={{ width: 160 }}>
                  <label>Horizontal Accuracy (m)</label>
                  <input type="number" min="0" step="any" value={horizontalAccuracy} onChange={e => setHorizontalAccuracy(e.target.value)} />
                </div>
                <div style={{ width: 160 }}>
                  <label>Vertical Accuracy (m)</label>
                  <input type="number" min="0" step="any" value={verticalAccuracy} onChange={e => setVerticalAccuracy(e.target.value)} />
                </div>
                <div style={{ width: 160 }}>
                  <label>Response Time</label>
                  <select value={responseTime} onChange={e => setResponseTime(e.target.value)}>
                    <option value="">(unset)</option>
                    <option value="low_delay">Low Delay</option>
                    <option value="delay_tolerant">Delay Tolerant</option>
                  </select>
                </div>
                <div style={{ width: 150, display: 'flex', alignItems: 'flex-end', paddingBottom: 6 }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <input type="checkbox" checked={verticalRequested} onChange={e => setVerticalRequested(e.target.checked)} />
                    Vertical Requested
                  </label>
                </div>
              </div>
            </details>
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
                {status.priority != null && (<><dt>Priority</dt><dd>{status.priority}</dd></>)}
                {status.qos && (<><dt>QoS</dt><dd>{Object.entries(status.qos).map(([k, v]) => `${k}=${v}`).join(', ')}</dd></>)}
                {status.failure_code && (<><dt>Failure</dt><dd>{status.failure_code}</dd></>)}
                <dt>Created</dt><dd>{status.created_at}</dd>
                <dt>Updated</dt><dd>{status.updated_at}</dd>
                {status.result?.shape && (<><dt>Shape</dt><dd>{status.result.shape}</dd></>)}
                {status.result?.uncertainty_meters != null && (<><dt>Uncertainty</dt><dd>{status.result.uncertainty_meters.toFixed(1)} m</dd></>)}
                {status.result?.ecgi && (<><dt>ECGI</dt><dd>{status.result.ecgi}</dd></>)}
              </dl>
              {status.state === 'completed' && !status.result?.latitude && (
                <p className="muted-note">Completed with no single-point geometry (ECGI-only or polygon shape).</p>
              )}
            </div>
          )}

          {previewPoint && (
            <div className="card muted-note">
              Showing a history fix from {new Date(previewPoint.recorded_at).toLocaleString()} — not the form's current request.
            </div>
          )}

          {hasPoint && <LocationMap result={result} />}
        </div>

        <aside className="col-side">
          <div className="card">
            <div className="side-title">History</div>
            <div className="form-row" style={{ marginBottom: 10 }}>
              <select value={historyWindowHours} onChange={e => setHistoryWindowHours(e.target.value)} style={{ flex: 1 }}>
                <option value="1">Last 1h</option>
                <option value="24">Last 24h</option>
                <option value="168">Last 7d</option>
                <option value="720">Last 30d</option>
              </select>
            </div>
            <button
              type="button"
              disabled={!idValue.trim() || historyLoading}
              onClick={viewHistory}
              style={{ width: '100%', marginBottom: 10 }}
            >
              {historyLoading ? 'Loading…' : `View ${idType.toUpperCase()} History`}
            </button>
            {historyError && <div className="muted-note" style={{ color: 'var(--critical)' }}>{historyError}</div>}
            {historyPoints != null && historyPoints.length === 0 && !historyError && (
              <div className="muted-note">No recorded fixes in this window.</div>
            )}
            {historyPoints != null && historyPoints.length > 0 && (
              <ul className="history-list">
                {historyPoints.map((p, i) => (
                  <li key={i}>
                    <button type="button" className="history-item" onClick={() => setPreviewPoint(p)}>
                      <span>{new Date(p.recorded_at).toLocaleString()}</span>
                      <code>{p.uncertainty_meters != null ? `±${Math.round(p.uncertainty_meters)}m` : ''}</code>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </aside>
      </div>
    </div>
  )
}
