import { useEffect, useRef, useCallback } from 'react'
import { latencyColor, formatLatency, shortID } from '../utils.js'
import styles from './TopologyGraph.module.css'

// --- force simulation (no external lib) ---
function createSimulation(nodes, edges) {
  const REPEL = 3200
  const SPRING_LEN = 130
  const SPRING_K = 0.04
  const DAMP = 0.82
  const CENTER_K = 0.008

  nodes.forEach(n => {
    if (n.vx == null) { n.vx = (Math.random() - 0.5) * 2; n.vy = (Math.random() - 0.5) * 2 }
  })

  return function tick(cx, cy) {
    for (let i = 0; i < nodes.length; i++) {
      let fx = 0, fy = 0
      const a = nodes[i]

      // repulsion between all pairs
      for (let j = 0; j < nodes.length; j++) {
        if (i === j) continue
        const b = nodes[j]
        const dx = a.x - b.x, dy = a.y - b.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const f = REPEL / (dist * dist)
        fx += (dx / dist) * f
        fy += (dy / dist) * f
      }

      // spring attraction along edges
      for (const e of edges) {
        let other = null
        if (e.source === a.key) other = nodes.find(n => n.key === e.target)
        else if (e.target === a.key) other = nodes.find(n => n.key === e.source)
        if (!other) continue
        const dx = other.x - a.x, dy = other.y - a.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const stretch = dist - SPRING_LEN
        fx += (dx / dist) * stretch * SPRING_K
        fy += (dy / dist) * stretch * SPRING_K
      }

      // center gravity
      fx += (cx - a.x) * CENTER_K
      fy += (cy - a.y) * CENTER_K

      a.vx = (a.vx + fx) * DAMP
      a.vy = (a.vy + fy) * DAMP
      a.x += a.vx
      a.y += a.vy
    }
  }
}

// --- build graph data from topology ---
function buildGraph(machines) {
  const nodes = []
  const edges = []
  const peerSeen = new Set()

  for (const m of machines) {
    const mk = `m:${m.machine_id}`
    if (!nodes.find(n => n.key === mk)) {
      nodes.push({
        key: mk, type: 'machine', active: m.active,
        label: m.hostname || shortID(m.machine_id),
        sub: shortID(m.machine_id),
        data: m,
        x: Math.random() * 600 + 100,
        y: Math.random() * 400 + 100,
        vx: 0, vy: 0,
      })
    }
    for (const inst of (m.network_instances || [])) {
      for (const peer of (inst.peers || [])) {
        const pk = `p:${inst.instance_id}:${peer.peer_id}`
        if (!peerSeen.has(pk)) {
          peerSeen.add(pk)
          nodes.push({
            key: pk, type: 'peer',
            label: peer.hostname || `Peer ${peer.peer_id}`,
            sub: peer.ipv4 || `#${peer.peer_id}`,
            data: peer, instance: inst,
            latency: peer.latency_ms,
            x: Math.random() * 600 + 100,
            y: Math.random() * 400 + 100,
            vx: 0, vy: 0,
          })
          edges.push({
            source: mk, target: pk,
            direct: peer.direct,
            latency: peer.latency_ms,
            key: `e:${pk}`,
          })
        }
      }
    }
  }
  return { nodes, edges }
}

export default function TopologyGraph({ machines, selected, onSelect }) {
  const svgRef = useRef(null)
  const stateRef = useRef({ nodes: [], edges: [], tick: null, dragging: null, zoom: 1, panX: 0, panY: 0, animId: null })
  const selectedKeyRef = useRef(null)

  selectedKeyRef.current = selected?.key ?? null

  const rebuildGraph = useCallback((mList) => {
    const { nodes: newNodes, edges } = buildGraph(mList)
    const s = stateRef.current
    // preserve positions for existing nodes
    for (const n of newNodes) {
      const prev = s.nodes.find(p => p.key === n.key)
      if (prev) { n.x = prev.x; n.y = prev.y; n.vx = prev.vx; n.vy = prev.vy }
    }
    s.nodes = newNodes
    s.edges = edges
    const svg = svgRef.current
    if (svg) {
      const cx = (svg.clientWidth || 800) / 2
      const cy = (svg.clientHeight || 500) / 2
      s.tick = createSimulation(s.nodes, s.edges).bind(null, cx, cy)
    }
  }, [])

  // rebuild when machines change
  useEffect(() => { rebuildGraph(machines) }, [machines, rebuildGraph])

  // render loop
  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const s = stateRef.current
    let stopped = false

    function draw() {
      if (stopped) return
      if (s.nodes.length === 0) { s.animId = requestAnimationFrame(draw); return }

      // run simulation
      if (s.tick) {
        const cx = (svg.clientWidth || 800) / 2
        const cy = (svg.clientHeight || 500) / 2
        s.tick = createSimulation(s.nodes, s.edges).bind(null, cx, cy)
        for (let i = 0; i < 3; i++) s.tick(cx, cy)
      }

      const W = svg.clientWidth || 800
      const H = svg.clientHeight || 500
      svg.setAttribute('viewBox', `0 0 ${W} ${H}`)

      // clear and redraw
      svg.innerHTML = ''

      // defs: arrowhead markers
      const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs')
      for (const [id, color] of [['arr-direct', '#22c55e'], ['arr-relay', '#f59e0b']]) {
        const marker = document.createElementNS('http://www.w3.org/2000/svg', 'marker')
        marker.setAttribute('id', id)
        marker.setAttribute('markerWidth', '7')
        marker.setAttribute('markerHeight', '7')
        marker.setAttribute('refX', '6')
        marker.setAttribute('refY', '3.5')
        marker.setAttribute('orient', 'auto')
        const poly = document.createElementNS('http://www.w3.org/2000/svg', 'polygon')
        poly.setAttribute('points', '0 0, 7 3.5, 0 7')
        poly.setAttribute('fill', color)
        poly.setAttribute('opacity', '0.7')
        marker.appendChild(poly)
        defs.appendChild(marker)
      }
      svg.appendChild(defs)

      const g = document.createElementNS('http://www.w3.org/2000/svg', 'g')
      g.setAttribute('transform', `translate(${s.panX},${s.panY}) scale(${s.zoom})`)
      svg.appendChild(g)

      const byKey = new Map(s.nodes.map(n => [n.key, n]))

      // draw edges first
      for (const e of s.edges) {
        const src = byKey.get(e.source), tgt = byKey.get(e.target)
        if (!src || !tgt) continue
        const color = e.direct ? (e.latency != null ? latencyColor(e.latency) : '#22c55e') : '#f59e0b'
        const line = document.createElementNS('http://www.w3.org/2000/svg', 'line')
        line.setAttribute('x1', src.x); line.setAttribute('y1', src.y)
        line.setAttribute('x2', tgt.x); line.setAttribute('y2', tgt.y)
        line.setAttribute('stroke', color)
        line.setAttribute('stroke-width', e.direct ? '2' : '1.5')
        line.setAttribute('stroke-opacity', '0.6')
        if (!e.direct) line.setAttribute('stroke-dasharray', '6 4')
        line.setAttribute('marker-end', `url(#${e.direct ? 'arr-direct' : 'arr-relay'})`)
        g.appendChild(line)

        // RTT label on edge midpoint
        if (e.latency != null) {
          const mx = (src.x + tgt.x) / 2, my = (src.y + tgt.y) / 2
          const bg = document.createElementNS('http://www.w3.org/2000/svg', 'rect')
          bg.setAttribute('x', mx - 22); bg.setAttribute('y', my - 9)
          bg.setAttribute('width', '44'); bg.setAttribute('height', '14')
          bg.setAttribute('rx', '3'); bg.setAttribute('fill', 'white')
          bg.setAttribute('fill-opacity', '0.85')
          g.appendChild(bg)
          const lbl = document.createElementNS('http://www.w3.org/2000/svg', 'text')
          lbl.setAttribute('x', mx); lbl.setAttribute('y', my + 1)
          lbl.setAttribute('text-anchor', 'middle')
          lbl.setAttribute('font-size', '9')
          lbl.setAttribute('fill', color)
          lbl.setAttribute('font-weight', '600')
          lbl.setAttribute('pointer-events', 'none')
          lbl.textContent = formatLatency(e.latency)
          g.appendChild(lbl)
        }
      }

      // draw nodes
      for (const node of s.nodes) {
        const isMachine = node.type === 'machine'
        const isSel = selectedKeyRef.current === node.key
        const r = isMachine ? 14 : 10

        const grp = document.createElementNS('http://www.w3.org/2000/svg', 'g')
        grp.setAttribute('cursor', 'pointer')
        grp.setAttribute('tabindex', '0')
        grp.setAttribute('role', 'button')
        grp.setAttribute('aria-label', node.label)

        // hitbox
        const hit = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
        hit.setAttribute('cx', node.x); hit.setAttribute('cy', node.y)
        hit.setAttribute('r', r + 8)
        hit.setAttribute('fill', 'transparent')
        grp.appendChild(hit)

        // glow ring when selected
        if (isSel) {
          const ring = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
          ring.setAttribute('cx', node.x); ring.setAttribute('cy', node.y)
          ring.setAttribute('r', r + 5)
          ring.setAttribute('fill', 'none')
          ring.setAttribute('stroke', '#3b82f6')
          ring.setAttribute('stroke-width', '3')
          ring.setAttribute('stroke-opacity', '0.5')
          grp.appendChild(ring)
        }

        // node circle
        const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
        circle.setAttribute('cx', node.x); circle.setAttribute('cy', node.y)
        circle.setAttribute('r', r)
        const fill = node.active === false ? '#94a3b8'
          : isMachine ? '#3b82f6'
          : (node.latency != null ? latencyColor(node.latency) : '#60a5fa')
        circle.setAttribute('fill', fill)
        circle.setAttribute('stroke', 'white')
        circle.setAttribute('stroke-width', '2')
        circle.setAttribute('filter', 'drop-shadow(0 2px 4px rgba(0,0,0,0.18))')
        grp.appendChild(circle)

        // label
        const lbl = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        lbl.setAttribute('x', node.x + r + 6)
        lbl.setAttribute('y', node.y - 2)
        lbl.setAttribute('font-size', '11')
        lbl.setAttribute('font-weight', '700')
        lbl.setAttribute('fill', '#0f172a')
        lbl.setAttribute('pointer-events', 'none')
        lbl.textContent = node.label.length > 18 ? node.label.slice(0, 17) + '…' : node.label
        grp.appendChild(lbl)

        const sub = document.createElementNS('http://www.w3.org/2000/svg', 'text')
        sub.setAttribute('x', node.x + r + 6)
        sub.setAttribute('y', node.y + 10)
        sub.setAttribute('font-size', '9')
        sub.setAttribute('fill', '#64748b')
        sub.setAttribute('pointer-events', 'none')
        sub.textContent = node.sub
        grp.appendChild(sub)

        grp.addEventListener('click', (e) => { e.stopPropagation(); onSelect?.(node) })
        grp.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') onSelect?.(node) })

        // drag
        grp.addEventListener('pointerdown', (e) => {
          e.stopPropagation()
          s.dragging = { node, startX: e.clientX, startY: e.clientY, ox: node.x, oy: node.y }
        })

        g.appendChild(grp)
      }

      s.animId = requestAnimationFrame(draw)
    }

    s.animId = requestAnimationFrame(draw)
    return () => { stopped = true; cancelAnimationFrame(s.animId) }
  }, [onSelect])

  // pan + zoom on the SVG itself
  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const s = stateRef.current
    let panning = false, panStart = { x: 0, y: 0 }, panOrigin = { x: 0, y: 0 }

    function onWheel(e) {
      e.preventDefault()
      const delta = e.deltaY < 0 ? 1.1 : 0.91
      s.zoom = Math.max(0.2, Math.min(4, s.zoom * delta))
    }

    function onPointerDown(e) {
      if (s.dragging) return
      panning = true
      panStart = { x: e.clientX, y: e.clientY }
      panOrigin = { x: s.panX, y: s.panY }
      svg.setPointerCapture(e.pointerId)
    }

    function onPointerMove(e) {
      if (s.dragging) {
        const d = s.dragging
        d.node.x = d.ox + (e.clientX - d.startX) / s.zoom
        d.node.y = d.oy + (e.clientY - d.startY) / s.zoom
        d.node.vx = 0; d.node.vy = 0
        return
      }
      if (!panning) return
      s.panX = panOrigin.x + (e.clientX - panStart.x)
      s.panY = panOrigin.y + (e.clientY - panStart.y)
    }

    function onPointerUp() {
      panning = false
      s.dragging = null
    }

    svg.addEventListener('wheel', onWheel, { passive: false })
    svg.addEventListener('pointerdown', onPointerDown)
    svg.addEventListener('pointermove', onPointerMove)
    svg.addEventListener('pointerup', onPointerUp)
    return () => {
      svg.removeEventListener('wheel', onWheel)
      svg.removeEventListener('pointerdown', onPointerDown)
      svg.removeEventListener('pointermove', onPointerMove)
      svg.removeEventListener('pointerup', onPointerUp)
    }
  }, [])

  const isEmpty = machines.length === 0

  return (
    <div className={styles.wrap}>
      {isEmpty && (
        <div className={styles.empty}>
          <strong>No topology data</strong>
          <span>Connect an EasyTier client or adjust the filters.</span>
        </div>
      )}
      <svg ref={svgRef} className={styles.svg} role="img" aria-label="NexusTier topology graph" />
      <div className={styles.hint}>Scroll to zoom · Drag to pan · Click node to inspect</div>
    </div>
  )
}
