import React, { useEffect, useRef } from 'react'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'

// result is a GMLC "result" object: latitude/longitude plus whichever of
// uncertainty_meters (circle) or semi_major/minor_meters (ellipse) the
// shape carries. Leaflet has no native ellipse primitive, so an ellipse is
// approximated with a dashed circle at its semi-major radius.
export default function LocationMap({ result }) {
  const containerRef = useRef(null)
  const mapRef = useRef(null)
  const layerRef = useRef(null)

  // Init the base map once.
  useEffect(() => {
    if (!containerRef.current || mapRef.current) return
    mapRef.current = L.map(containerRef.current, {
      zoomControl: true,
      attributionControl: true,
    })
    mapRef.current.setView([20, 0], 2)

    // CartoDB light tiles — the rest of the console stays dark, but a light
    // basemap reads more clearly for imagery/street detail around a point.
    L.tileLayer('https://{s}.basemaps.cartocdn.com/light_all/{z}/{x}/{y}{r}.png', {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> &copy; <a href="https://carto.com/">CARTO</a>',
      subdomains: 'abcd',
      maxZoom: 19,
    }).addTo(mapRef.current)

    return () => {
      mapRef.current?.remove()
      mapRef.current = null
    }
  }, [])

  // Plot/replace the point marker + uncertainty geometry when result changes.
  useEffect(() => {
    const map = mapRef.current
    if (!map || !result || result.latitude == null || result.longitude == null) return

    const { latitude: lat, longitude: lon, uncertainty_meters: uncertaintyM, semi_major_meters: semiMajorM } = result

    const group = L.layerGroup()
    L.circleMarker([lat, lon], {
      radius: 6, weight: 2, color: '#f5a623', fillColor: '#f5a623', fillOpacity: 1,
    }).addTo(group)

    if (uncertaintyM) {
      L.circle([lat, lon], {
        radius: uncertaintyM, weight: 1, color: '#f5a623', fillColor: '#f5a623', fillOpacity: 0.12,
      }).addTo(group)
    } else if (semiMajorM) {
      L.circle([lat, lon], {
        radius: semiMajorM, weight: 1, dashArray: '4 3', color: '#f5a623', fillColor: '#f5a623', fillOpacity: 0.08,
      }).addTo(group)
    }

    group.addTo(map)

    const radius = uncertaintyM || semiMajorM || 500
    map.fitBounds(L.latLng(lat, lon).toBounds(Math.max(radius * 2.5, 300)), { padding: [24, 24] })

    return () => group.remove()
  }, [result])

  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 4, overflow: 'hidden' }}>
      <div style={{
        fontFamily: 'var(--font-ui)', fontSize: 11, fontWeight: 700,
        letterSpacing: '0.08em', textTransform: 'uppercase',
        color: 'var(--muted)', padding: '6px 10px',
        background: 'var(--surface2)', borderBottom: '1px solid var(--border)',
      }}>
        UE Location
      </div>
      <div ref={containerRef} style={{ height: 380, width: '100%' }} />
    </div>
  )
}
