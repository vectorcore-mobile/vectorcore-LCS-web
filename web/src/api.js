const BASE = '/api/v1'

async function req(method, path, body, extraHeaders) {
  const opts = { method, headers: { ...(extraHeaders || {}) } }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const res = await fetch(BASE + path, opts)
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

export const api = {
  gmlcStatus: () => req('GET', '/gmlc/status'),

  submitLocationRequest: (body) =>
    req('POST', '/location-requests', body, { 'Idempotency-Key': newIdempotencyKey() }),

  getLocationRequest: (id) => req('GET', `/location-requests/${encodeURIComponent(id)}`),

  cancelLocationRequest: (id) => req('DELETE', `/location-requests/${encodeURIComponent(id)}`),
}
