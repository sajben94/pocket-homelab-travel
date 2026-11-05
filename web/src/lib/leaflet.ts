// Leaflet plus the clustering plugin. The plugin is a UMD script that
// extends the global L, so L must be on window before it loads.
import L from 'leaflet'
;(window as unknown as { L: typeof L }).L = L
export default L
