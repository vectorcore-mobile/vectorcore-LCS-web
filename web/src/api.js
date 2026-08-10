async function req(method, path, body, extraHeaders) {
  const opts = { method, headers: { ...(extraHeaders || {}) } }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const res = await fetch(path, opts)
  let data = null
  try { data = await res.json() } catch { /* empty body */ }
  if (!res.ok || (data && data.error)) {
    const err = data?.error || {}
    throw new Error(err.detail || err.code || `HTTP ${res.status}`)
  }
  return data
}

function newIdempotencyKey() {
  if (crypto.randomUUID) return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

// createLocationApi builds the small client the console's pages use,
// bound to one base path — "/api/v1" for the default GMLC profile,
// "/api/v1/emergency" for the emergency one (see L3 in
// docs/mlp-le-interface-plan.md). Same shape either way; only the
// destination differs, since the console backend — not the browser —
// is what actually knows which GMLC client credential a given base path
// maps to.
export function createLocationApi(base) {
  return {
    gmlcStatus: () => req('GET', `${base}/gmlc/status`),
    submitLocationRequest: (body) =>
      req('POST', `${base}/location-requests`, body, { 'Idempotency-Key': newIdempotencyKey() }),
    getLocationRequest: (id) => req('GET', `${base}/location-requests/${encodeURIComponent(id)}`),
    cancelLocationRequest: (id) => req('DELETE', `${base}/location-requests/${encodeURIComponent(id)}`),
    // getHistory queries GMLC's own recorded location_history for a
    // target (server-side, persistent — see gmlc.LocationClient.History),
    // not a browser-local record of past submissions. stop is optional;
    // omitted means "up to now".
    getHistory: ({ targetKind, targetValue, start, stop }) => {
      const params = new URLSearchParams({ target_kind: targetKind, target_value: targetValue, start })
      if (stop) params.set('stop', stop)
      return req('GET', `${base}/history?${params.toString()}`)
    },
  }
}

export const api = createLocationApi('/api/v1')
