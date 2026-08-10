import React from 'react'
import { api } from '../api.js'
import LocationConsole from '../components/LocationConsole.jsx'

export default function Locate() {
  return (
    <LocationConsole
      api={api}
      title="VectorCore LCS"
      subtitle="UE Location Console"
    />
  )
}
