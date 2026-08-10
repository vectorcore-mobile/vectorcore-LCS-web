import React from 'react'
import { createLocationApi } from '../api.js'
import LocationConsole from '../components/LocationConsole.jsx'

// The emergency console talks to a separate route prefix, which the
// backend binds to a separately-configured GMLC client credential (the
// "emergency" gmlc_clients profile — see L3 in
// docs/mlp-le-interface-plan.md). It is not a mode toggle on the default
// request: GMLC resolves LCS-Client-Type purely from which client
// authenticated, so this page's requests are only "emergency" because of
// which backend route/credential they go through, not anything in the
// request body itself.
const emergencyApi = createLocationApi('/api/v1/emergency')

export default function Emergency() {
  return (
    <LocationConsole
      api={emergencyApi}
      title="VectorCore LCS — Emergency"
      subtitle="Emergency Location Console"
    />
  )
}
