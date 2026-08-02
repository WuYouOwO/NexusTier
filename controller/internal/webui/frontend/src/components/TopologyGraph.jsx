import { useEffect, useRef } from 'react'
import { latencyColor, formatLatency, shortID } from '../utils.js'
import styles from './TopologyGraph.module.css'

const REPEL = 3600
const SPRING_LEN = 140
const SPRING_K = 0.04
const DAMP = 0.82
const CENTER_K = 0.007
const ALPHA_DECAY = 0.02
const ALPHA_MIN = 0.005
const SETTLE_EPSILON = 0.08

// 力导向布局。alpha 随时间衰减，使布局收敛后停止重绘。
function createSimulation() {
  let nodes = []
  let adjacency = new Map()
  let alpha = 1

  function setGraph(nextNodes, nextEdges) {
    nodes = nextNodes
    adjacency = new Map(nodes.map((n) => [n.key, []]))
    for (const edge of nextEdges) {
      adjacency.get(edge.source)?.push(edge.target)
      adjacency.get(edge.target)?.push(edge.source)
    }
  }

  // 返回本帧的最大位移，供调用方判断是否已收敛。
  function tick(cx, cy) {
    if (alpha < ALPHA_MIN) return 0
    const byKey = new Map(nodes.map((n) => [n.key, n]))
    let maxShift = 0

    for (const a of nodes) {
      if (a.fixed) continue
      let fx = 0
      let fy = 0

      for (const b of nodes) {
        if (a === b) continue
        const dx = a.x - b.x
        const dy = a.y - b.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const force = REPEL / (dist * dist)
        fx += (dx / dist) * force
        fy += (dy / dist) * force
      }

      for (const neighborKey of adjacency.get(a.key) || []) {
        const other = byKey.get(neighborKey)
        if (!other) continue
        const dx = other.x - a.x
        const dy = other.y - a.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 1
        const stretch = dist - SPRING_LEN
        fx += (dx / dist) * stretch * SPRING_K
        fy += (dy / dist) * stretch * SPRING_K
      }

      fx += (cx - a.x) * CENTER_K
      fy += (cy - a.y) * CENTER_K

      a.vx = (a.vx + fx * alpha) * DAMP
      a.vy = (a.vy + fy * alpha) * DAMP
      a.x += a.vx
      a.y += a.vy
      maxShift = Math.max(maxShift, Math.abs(a.vx), Math.abs(a.vy))
    }

    alpha *= 1 - ALPHA_DECAY
    return maxShift
  }

  return {
    setGraph,
    tick,
    reheat() {
      alpha = 1
    },
  }
}

function buildGraph(machines) {
  const nodes = []
  const edges = []
  const seen = new Set()

  for (const machine of machines) {
    const machineKey = `m:${machine.machine_id}`
    if (!seen.has(machineKey)) {
      seen.add(machineKey)
      nodes.push({
        key: machineKey,
        type: 'machine',
        active: machine.active,
        label: machine.hostname || shortID(machine.machine_id),
        sub: shortID(machine.machine_id),
        x: Math.random() * 600 + 100,
        y: Math.random() * 400 + 100,
        vx: 0,
        vy: 0,
      })
    }

    for (const instance of machine.network_instances || []) {
      for (const peer of instance.peers || []) {
        const peerKey = `p:${instance.instance_id}:${peer.peer_id}`
        if (seen.has(peerKey)) continue
        seen.add(peerKey)
        nodes.push({
          key: peerKey,
          type: 'peer',
          label: peer.hostname || `Peer ${peer.peer_id}`,
          sub: peer.ipv4 || `#${peer.peer_id}`,
          latency: peer.latency_ms,
          x: Math.random() * 600 + 100,
          y: Math.random() * 400 + 100,
          vx: 0,
          vy: 0,
        })
        edges.push({
          key: `e:${peerKey}`,
          source: machineKey,
          target: peerKey,
          direct: peer.direct,
          latency: peer.latency_ms,
        })
      }
    }
  }

  return { nodes, edges }
}

function svgEl(name, attrs) {
  const el = document.createElementNS('http://www.w3.org/2000/svg', name)
  for (const [key, value] of Object.entries(attrs)) el.setAttribute(key, value)
  return el
}

export default function TopologyGraph({ machines, selectedKey, onSelect }) {
  const svgRef = useRef(null)
  const onSelectRef = useRef(onSelect)
  const stateRef = useRef({
    nodes: [],
    edges: [],
    simulation: createSimulation(),
    dragging: null,
    zoom: 1,
    panX: 0,
    panY: 0,
    selectedKey: null,
    dirty: true,
    animId: null,
  })

  onSelectRef.current = onSelect

  useEffect(() => {
    const state = stateRef.current
    state.selectedKey = selectedKey ?? null
    state.dirty = true
  }, [selectedKey])

  useEffect(() => {
    const state = stateRef.current
    const { nodes, edges } = buildGraph(machines)
    // 保留既有节点坐标，避免刷新后布局跳变。
    const previous = new Map(state.nodes.map((n) => [n.key, n]))
    for (const node of nodes) {
      const prev = previous.get(node.key)
      if (prev) {
        node.x = prev.x
        node.y = prev.y
        node.vx = prev.vx
        node.vy = prev.vy
      }
    }
    state.nodes = nodes
    state.edges = edges
    state.simulation.setGraph(nodes, edges)
    // 指标刷新不应重排布局，只有拓扑成员变化才重新收敛
    const topologyChanged =
      nodes.length !== previous.size || nodes.some((node) => !previous.has(node.key))
    if (topologyChanged) state.simulation.reheat()
    state.dirty = true
  }, [machines])

  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const state = stateRef.current
    let stopped = false

    function render() {
      const width = svg.clientWidth || 800
      const height = svg.clientHeight || 500
      svg.setAttribute('viewBox', `0 0 ${width} ${height}`)

      const defs = svgEl('defs', {})
      for (const [id, color] of [
        ['arr-d', '#1677ff'],
        ['arr-r', '#faad14'],
      ]) {
        const marker = svgEl('marker', {
          id,
          markerWidth: '7',
          markerHeight: '7',
          refX: '6',
          refY: '3.5',
          orient: 'auto',
        })
        marker.appendChild(svgEl('polygon', { points: '0 0, 7 3.5, 0 7', fill: color, opacity: '0.7' }))
        defs.appendChild(marker)
      }

      const scene = svgEl('g', { transform: `translate(${state.panX},${state.panY}) scale(${state.zoom})` })
      const byKey = new Map(state.nodes.map((n) => [n.key, n]))

      for (const edge of state.edges) {
        const source = byKey.get(edge.source)
        const target = byKey.get(edge.target)
        if (!source || !target) continue
        const color = edge.direct
          ? edge.latency != null
            ? latencyColor(edge.latency)
            : '#1677ff'
          : '#faad14'
        const line = svgEl('line', {
          x1: source.x,
          y1: source.y,
          x2: target.x,
          y2: target.y,
          stroke: color,
          'stroke-width': edge.direct ? '1.8' : '1.5',
          'stroke-opacity': '0.55',
          'marker-end': `url(#${edge.direct ? 'arr-d' : 'arr-r'})`,
        })
        if (!edge.direct) line.setAttribute('stroke-dasharray', '6 4')
        scene.appendChild(line)

        if (edge.latency != null) {
          const mx = (source.x + target.x) / 2
          const my = (source.y + target.y) / 2
          scene.appendChild(
            svgEl('rect', {
              x: mx - 22,
              y: my - 9,
              width: '44',
              height: '14',
              rx: '3',
              fill: 'white',
              'fill-opacity': '0.9',
            }),
          )
          const label = svgEl('text', {
            x: mx,
            y: my + 1,
            'text-anchor': 'middle',
            'font-size': '9',
            fill: color,
            'font-weight': '600',
            'pointer-events': 'none',
          })
          label.textContent = formatLatency(edge.latency)
          scene.appendChild(label)
        }
      }

      for (const node of state.nodes) {
        const isMachine = node.type === 'machine'
        const isSelected = state.selectedKey === node.key
        const radius = isMachine ? 13 : 9

        const group = svgEl('g', {
          cursor: 'pointer',
          tabindex: '0',
          role: 'button',
          'aria-label': node.label,
        })
        group.appendChild(svgEl('circle', { cx: node.x, cy: node.y, r: radius + 8, fill: 'transparent' }))

        if (isSelected) {
          group.appendChild(
            svgEl('circle', {
              cx: node.x,
              cy: node.y,
              r: radius + 5,
              fill: 'none',
              stroke: '#1677ff',
              'stroke-width': '2.5',
              'stroke-opacity': '.5',
            }),
          )
        }

        const fill =
          node.active === false
            ? '#bfbfbf'
            : isMachine
              ? '#1677ff'
              : node.latency != null
                ? latencyColor(node.latency)
                : '#40a9ff'
        group.appendChild(
          svgEl('circle', {
            cx: node.x,
            cy: node.y,
            r: radius,
            fill,
            stroke: 'white',
            'stroke-width': '2',
            filter: 'drop-shadow(0 1px 3px rgba(0,0,0,.18))',
          }),
        )

        const label = svgEl('text', {
          x: node.x + radius + 6,
          y: node.y - 2,
          'font-size': '11',
          'font-weight': '600',
          fill: '#1f2937',
          'pointer-events': 'none',
        })
        label.textContent = node.label.length > 18 ? `${node.label.slice(0, 17)}…` : node.label
        group.appendChild(label)

        const sub = svgEl('text', {
          x: node.x + radius + 6,
          y: node.y + 10,
          'font-size': '9',
          fill: '#8c8c8c',
          'pointer-events': 'none',
        })
        sub.textContent = node.sub
        group.appendChild(sub)

        group.addEventListener('click', (event) => {
          event.stopPropagation()
          onSelectRef.current?.(node.key)
        })
        group.addEventListener('keydown', (event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault()
            onSelectRef.current?.(node.key)
          }
        })
        group.addEventListener('pointerdown', (event) => {
          event.stopPropagation()
          node.fixed = true
          state.dragging = { node, startX: event.clientX, startY: event.clientY, ox: node.x, oy: node.y }
          svg.setPointerCapture(event.pointerId)
        })

        scene.appendChild(group)
      }

      svg.replaceChildren(defs, scene)
    }

    function frame() {
      if (stopped) return
      const width = svg.clientWidth || 800
      const height = svg.clientHeight || 500
      const shift = state.simulation.tick(width / 2, height / 2)
      if (shift > SETTLE_EPSILON || state.dirty) {
        state.dirty = false
        render()
      }
      state.animId = requestAnimationFrame(frame)
    }

    state.animId = requestAnimationFrame(frame)
    return () => {
      stopped = true
      cancelAnimationFrame(state.animId)
    }
  }, [])

  useEffect(() => {
    const svg = svgRef.current
    if (!svg) return
    const state = stateRef.current
    let panning = false
    let panStart = { x: 0, y: 0 }
    let panOrigin = { x: 0, y: 0 }

    function onWheel(event) {
      event.preventDefault()
      state.zoom = Math.max(0.2, Math.min(4, state.zoom * (event.deltaY < 0 ? 1.1 : 0.91)))
      state.dirty = true
    }

    function onPointerDown(event) {
      if (state.dragging) return
      panning = true
      panStart = { x: event.clientX, y: event.clientY }
      panOrigin = { x: state.panX, y: state.panY }
      svg.setPointerCapture(event.pointerId)
    }

    function onPointerMove(event) {
      if (state.dragging) {
        const drag = state.dragging
        drag.node.x = drag.ox + (event.clientX - drag.startX) / state.zoom
        drag.node.y = drag.oy + (event.clientY - drag.startY) / state.zoom
        drag.node.vx = 0
        drag.node.vy = 0
        drag.moved = true
        state.dirty = true
        return
      }
      if (!panning) return
      state.panX = panOrigin.x + (event.clientX - panStart.x)
      state.panY = panOrigin.y + (event.clientY - panStart.y)
      state.dirty = true
    }

    function onPointerUp() {
      panning = false
      if (!state.dragging) return
      state.dragging.node.fixed = false
      // 仅在真正拖动后重新收敛，单击查看详情不扰动布局
      if (state.dragging.moved) state.simulation.reheat()
      state.dragging = null
    }

    svg.addEventListener('wheel', onWheel, { passive: false })
    svg.addEventListener('pointerdown', onPointerDown)
    svg.addEventListener('pointermove', onPointerMove)
    svg.addEventListener('pointerup', onPointerUp)
    svg.addEventListener('pointercancel', onPointerUp)
    return () => {
      svg.removeEventListener('wheel', onWheel)
      svg.removeEventListener('pointerdown', onPointerDown)
      svg.removeEventListener('pointermove', onPointerMove)
      svg.removeEventListener('pointerup', onPointerUp)
      svg.removeEventListener('pointercancel', onPointerUp)
    }
  }, [])

  const applyZoom = (factor) => {
    const state = stateRef.current
    state.zoom = Math.max(0.2, Math.min(4, state.zoom * factor))
    state.dirty = true
  }

  const resetView = () => {
    const state = stateRef.current
    state.zoom = 1
    state.panX = 0
    state.panY = 0
    state.simulation.reheat()
    state.dirty = true
  }

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
        <button className={styles.ctrlBtn} onClick={() => applyZoom(1.2)} title="放大" aria-label="放大">
          ＋
        </button>
        <button className={styles.ctrlBtn} onClick={() => applyZoom(0.83)} title="缩小" aria-label="缩小">
          －
        </button>
        <button className={styles.ctrlBtn} onClick={resetView} title="还原视图" aria-label="还原视图">
          ⊙
        </button>
      </div>
      <div className={styles.hint}>滚轮缩放 · 拖拽平移 · 点击节点查看详情</div>
    </div>
  )
}
