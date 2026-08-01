import { useEffect, useRef, useCallback } from 'react'
import { latencyColor, formatLatency, shortID } from '../utils.js'
import styles from './TopologyGraph.module.css'

// --- 力模拟（无外部库）---
function createSimulation(nodes, edges) {
  const REPEL = 3600, SPRING_LEN = 140, SPRING_K = 0.04, DAMP = 0.82, CENTER_K = 0.007
  nodes.forEach(n => { if (n.vx == null) { n.vx = (Math.random()-.5)*2; n.vy = (Math.random()-.5)*2 } })
  return function tick(cx, cy) {
    for (let i = 0; i < nodes.length; i++) {
      let fx = 0, fy = 0; const a = nodes[i]
      for (let j = 0; j < nodes.length; j++) {
        if (i === j) continue
        const b = nodes[j]; const dx = a.x - b.x, dy = a.y - b.y
        const dist = Math.sqrt(dx*dx + dy*dy) || 1; const f = REPEL/(dist*dist)
        fx += (dx/dist)*f; fy += (dy/dist)*f
      }
      for (const e of edges) {
        let other = null
        if (e.source === a.key) other = nodes.find(n => n.key === e.target)
        else if (e.target === a.key) other = nodes.find(n => n.key === e.source)
        if (!other) continue
        const dx = other.x - a.x, dy = other.y - a.y, dist = Math.sqrt(dx*dx+dy*dy)||1
        const stretch = dist - SPRING_LEN
        fx += (dx/dist)*stretch*SPRING_K; fy += (dy/dist)*stretch*SPRING_K
      }
      fx += (cx - a.x)*CENTER_K; fy += (cy - a.y)*CENTER_K
      a.vx = (a.vx + fx)*DAMP; a.vy = (a.vy + fy)*DAMP
      a.x += a.vx; a.y += a.vy
    }
  }
}

function buildGraph(machines) {
  const nodes = [], edges = [], peerSeen = new Set()
  for (const m of machines) {
    const mk = `m:${m.machine_id}`
    if (!nodes.find(n => n.key === mk)) nodes.push({ key: mk, type: 'machine', active: m.active, label: m.hostname || shortID(m.machine_id), sub: shortID(m.machine_id), data: m, x: Math.random()*600+100, y: Math.random()*400+100, vx: 0, vy: 0 })
    for (const inst of (m.network_instances || [])) {
      for (const peer of (inst.peers || [])) {
        const pk = `p:${inst.instance_id}:${peer.peer_id}`
        if (!peerSeen.has(pk)) {
          peerSeen.add(pk)
          nodes.push({ key: pk, type: 'peer', label: peer.hostname || `Peer ${peer.peer_id}`, sub: peer.ipv4 || `#${peer.peer_id}`, data: peer, instance: inst, latency: peer.latency_ms, x: Math.random()*600+100, y: Math.random()*400+100, vx: 0, vy: 0 })
          edges.push({ source: mk, target: pk, direct: peer.direct, latency: peer.latency_ms, key: `e:${pk}` })
        }
      }
    }
  }
  return { nodes, edges }
}

function svgEl(name, attrs) {
  const el = document.createElementNS('http://www.w3.org/2000/svg', name)
  Object.entries(attrs).forEach(([k, v]) => el.setAttribute(k, v))
  return el
}

export default function TopologyGraph({ machines, selected, onSelect }) {
  const svgRef = useRef(null)
  const stateRef = useRef({ nodes: [], edges: [], tick: null, dragging: null, zoom: 1, panX: 0, panY: 0, animId: null })
  const selectedKeyRef = useRef(null)
  selectedKeyRef.current = selected?.key ?? null

  const rebuildGraph = useCallback((mList) => {
    const { nodes: newNodes, edges } = buildGraph(mList)
    const s = stateRef.current
    for (const n of newNodes) {
      const prev = s.nodes.find(p => p.key === n.key)
      if (prev) { n.x = prev.x; n.y = prev.y; n.vx = prev.vx; n.vy = prev.vy }
    }
    s.nodes = newNodes; s.edges = edges
    const svg = svgRef.current
    if (svg) { const cx = (svg.clientWidth||800)/2; const cy = (svg.clientHeight||500)/2; s.tick = createSimulation(s.nodes, s.edges).bind(null, cx, cy) }
  }, [])

  useEffect(() => { rebuildGraph(machines) }, [machines, rebuildGraph])

  useEffect(() => {
    const svg = svgRef.current; if (!svg) return
    const s = stateRef.current; let stopped = false

    function draw() {
      if (stopped) return
      if (s.nodes.length === 0) { s.animId = requestAnimationFrame(draw); return }
      if (s.tick) { const cx = (svg.clientWidth||800)/2; const cy = (svg.clientHeight||500)/2; s.tick = createSimulation(s.nodes, s.edges).bind(null, cx, cy); for (let i=0;i<3;i++) s.tick(cx, cy) }

      const W = svg.clientWidth||800, H = svg.clientHeight||500
      svg.setAttribute('viewBox', `0 0 ${W} ${H}`)
      svg.innerHTML = ''

      const defs = svgEl('defs', {})
      for (const [id, color] of [['arr-d', '#1677ff'], ['arr-r', '#faad14']]) {
        const mk = svgEl('marker', { id, markerWidth: '7', markerHeight: '7', refX: '6', refY: '3.5', orient: 'auto' })
        const poly = svgEl('polygon', { points: '0 0, 7 3.5, 0 7', fill: color, opacity: '0.7' })
        mk.appendChild(poly); defs.appendChild(mk)
      }
      svg.appendChild(defs)

      const g = svgEl('g', { transform: `translate(${s.panX},${s.panY}) scale(${s.zoom})` })
      svg.appendChild(g)
      const byKey = new Map(s.nodes.map(n => [n.key, n]))

      // 边
      for (const e of s.edges) {
        const src = byKey.get(e.source), tgt = byKey.get(e.target); if (!src||!tgt) continue
        const color = e.direct ? (e.latency != null ? latencyColor(e.latency) : '#1677ff') : '#faad14'
        const line = svgEl('line', { x1: src.x, y1: src.y, x2: tgt.x, y2: tgt.y, stroke: color, 'stroke-width': e.direct ? '1.8' : '1.5', 'stroke-opacity': '0.55' })
        if (!e.direct) line.setAttribute('stroke-dasharray', '6 4')
        line.setAttribute('marker-end', `url(#${e.direct ? 'arr-d' : 'arr-r'})`)
        g.appendChild(line)
        if (e.latency != null) {
          const mx = (src.x+tgt.x)/2, my = (src.y+tgt.y)/2
          const bg = svgEl('rect', { x: mx-22, y: my-9, width: '44', height: '14', rx: '3', fill: 'white', 'fill-opacity': '0.9' })
          const lbl = svgEl('text', { x: mx, y: my+1, 'text-anchor': 'middle', 'font-size': '9', fill: color, 'font-weight': '600', 'pointer-events': 'none' })
          lbl.textContent = formatLatency(e.latency)
          g.appendChild(bg); g.appendChild(lbl)
        }
      }

      // 节点
      for (const node of s.nodes) {
        const isMachine = node.type === 'machine', isSel = selectedKeyRef.current === node.key, r = isMachine ? 13 : 9
        const grp = svgEl('g', { cursor: 'pointer', tabindex: '0', role: 'button', 'aria-label': node.label })
        const hit = svgEl('circle', { cx: node.x, cy: node.y, r: r+8, fill: 'transparent' })
        grp.appendChild(hit)
        if (isSel) {
          const ring = svgEl('circle', { cx: node.x, cy: node.y, r: r+5, fill: 'none', stroke: '#1677ff', 'stroke-width': '2.5', 'stroke-opacity': '.5' })
          grp.appendChild(ring)
        }
        const fill = node.active === false ? '#bfbfbf' : isMachine ? '#1677ff' : (node.latency != null ? latencyColor(node.latency) : '#40a9ff')
        const circle = svgEl('circle', { cx: node.x, cy: node.y, r, fill, stroke: 'white', 'stroke-width': '2', filter: 'drop-shadow(0 1px 3px rgba(0,0,0,.18))' })
        grp.appendChild(circle)
        const lbl = svgEl('text', { x: node.x+r+6, y: node.y-2, 'font-size': '11', 'font-weight': '600', fill: '#1f2937', 'pointer-events': 'none' })
        lbl.textContent = node.label.length > 18 ? node.label.slice(0,17)+'…' : node.label
        const sub = svgEl('text', { x: node.x+r+6, y: node.y+10, 'font-size': '9', fill: '#8c8c8c', 'pointer-events': 'none' })
        sub.textContent = node.sub
        grp.appendChild(lbl); grp.appendChild(sub)
        grp.addEventListener('click', e => { e.stopPropagation(); onSelect?.(node) })
        grp.addEventListener('keydown', e => { if (e.key==='Enter'||e.key===' ') onSelect?.(node) })
        grp.addEventListener('pointerdown', e => { e.stopPropagation(); s.dragging = { node, startX: e.clientX, startY: e.clientY, ox: node.x, oy: node.y } })
        g.appendChild(grp)
      }
      s.animId = requestAnimationFrame(draw)
    }

    s.animId = requestAnimationFrame(draw)
    return () => { stopped = true; cancelAnimationFrame(s.animId) }
  }, [onSelect])

  // 缩放平移
  useEffect(() => {
    const svg = svgRef.current; if (!svg) return
    const s = stateRef.current; let panning = false, panStart = {x:0,y:0}, panOrigin = {x:0,y:0}
    const onWheel = e => { e.preventDefault(); s.zoom = Math.max(.2, Math.min(4, s.zoom*(e.deltaY<0?1.1:.91))) }
    const onDown = e => { if (s.dragging) return; panning=true; panStart={x:e.clientX,y:e.clientY}; panOrigin={x:s.panX,y:s.panY}; svg.setPointerCapture(e.pointerId) }
    const onMove = e => {
      if (s.dragging) { const d=s.dragging; d.node.x=d.ox+(e.clientX-d.startX)/s.zoom; d.node.y=d.oy+(e.clientY-d.startY)/s.zoom; d.node.vx=0; d.node.vy=0; return }
      if (!panning) return; s.panX=panOrigin.x+(e.clientX-panStart.x); s.panY=panOrigin.y+(e.clientY-panStart.y)
    }
    const onUp = () => { panning=false; s.dragging=null }
    svg.addEventListener('wheel', onWheel, {passive:false}); svg.addEventListener('pointerdown', onDown); svg.addEventListener('pointermove', onMove); svg.addEventListener('pointerup', onUp)
    return () => { svg.removeEventListener('wheel', onWheel); svg.removeEventListener('pointerdown', onDown); svg.removeEventListener('pointermove', onMove); svg.removeEventListener('pointerup', onUp) }
  }, [])

  const reset = () => { const s = stateRef.current; s.zoom=1; s.panX=0; s.panY=0 }
  const zoomIn  = () => { stateRef.current.zoom = Math.min(4, stateRef.current.zoom*1.2) }
  const zoomOut = () => { stateRef.current.zoom = Math.max(.2, stateRef.current.zoom*.83) }

  return (
    <div className={styles.wrap}>
      {machines.length === 0 && (
        <div className={styles.empty}>
          <strong>暂无拓扑数据</strong>
          <span>接入 EasyTier 客户端或调整筛选条件</span>
        </div>
      )}
      <svg ref={svgRef} className={styles.svg} role="img" aria-label="NexusTier 拓扑图" />
      <div className={styles.controls}>
        <button className={styles.ctrlBtn} onClick={zoomIn} title="放大">＋</button>
        <button className={styles.ctrlBtn} onClick={zoomOut} title="缩小">－</button>
        <button className={styles.ctrlBtn} onClick={reset} title="还原">⊙</button>
      </div>
      <div className={styles.hint}>滚轮缩放 · 拖拽平移 · 点击节点查看详情</div>
    </div>
  )
}
