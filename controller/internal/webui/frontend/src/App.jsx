import { useState, useMemo } from 'react'
import { useTopology } from './hooks/useTopology.js'
import Header from './components/Header.jsx'
import StatCard from './components/StatCard.jsx'
import Toolbar from './components/Toolbar.jsx'
import TopologyGraph from './components/TopologyGraph.jsx'
import DetailsPanel from './components/DetailsPanel.jsx'
import MachineTable from './components/MachineTable.jsx'
import styles from './App.module.css'
import { relativeTime } from './utils.js'

export default function App() {
  const {
    data, status, loading,
    interval, setIntervalMs,
    activeFilter, setActiveFilter,
    refresh, loadMore,
  } = useTopology()

  const [selected, setSelected] = useState(null)
  const [search, setSearch] = useState('')
  const [updatedAt] = useState(() => new Date())

  const machines = data?.machines ?? []
  const latestCollection = data?.latestCollection ?? null
  const latestErrors = data?.latestErrors ?? []
  const hasMore = data?.hasMore ?? false

  const filtered = useMemo(() => {
    if (!search.trim()) return machines
    const q = search.trim().toLowerCase()
    return machines.filter(m => {
      const texts = [
        m.machine_id, m.hostname, m.remote_url,
        ...(m.network_instances || []).flatMap(i => [
          i.instance_id,
          i.node?.hostname, i.node?.ipv4,
          ...(i.peers || []).flatMap(p => [p.hostname, p.ipv4, String(p.peer_id)]),
        ]),
      ]
      return texts.filter(Boolean).some(t => String(t).toLowerCase().includes(q))
    })
  }, [machines, search])

  // summary stats
  const instances = filtered.flatMap(m => m.network_instances || [])
  const peers = instances.flatMap(i => i.peers || [])
  const inactive = filtered.filter(m => !m.active).length
  const direct = peers.filter(p => p.direct).length
  const freshness = latestCollection
    ? relativeTime(new Date(latestCollection.completed_at))
    : 'No data'

  return (
    <div className={styles.app}>
      <Header status={status} updatedAt={updatedAt} />
      <main className={styles.main}>
        <div className={styles.statsRow}>
          <StatCard label="Machines"  value={filtered.length} sub={`${inactive} inactive`} />
          <StatCard label="Instances" value={instances.length} sub={`${instances.filter(i=>i.node).length} with node data`} />
          <StatCard label="Peer routes" value={peers.length} sub={`${direct} direct`} />
          <StatCard label="Collection freshness" value={freshness}
            sub={latestCollection ? `${latestCollection.status} · ${latestCollection.error_count} errors` : 'Waiting'}
            accent />
        </div>

        <Toolbar
          search={search} onSearchChange={setSearch}
          activeFilter={activeFilter} onFilterChange={v => { setActiveFilter(v); setSelected(null) }}
          interval={interval} onIntervalChange={setIntervalMs}
          onRefresh={refresh} loading={loading}
        />

        <div className={styles.workspace}>
          <div className={styles.graphCard}>
            <div className={styles.graphHeader}>
              <div>
                <p className={styles.eyebrow}>Live control view</p>
                <h2 className={styles.sectionTitle}>Connectivity map</h2>
              </div>
              <div className={styles.legend}>
                <span><i className={`${styles.dot} ${styles.machineNode}`} />Machine</span>
                <span><i className={`${styles.dot} ${styles.peerNode}`} />Peer</span>
                <span><i className={styles.lineD} />Direct</span>
                <span><i className={styles.lineR} />Relayed</span>
              </div>
            </div>
            <TopologyGraph machines={filtered} selected={selected} onSelect={setSelected} />
          </div>
          <DetailsPanel selected={selected} latestCollection={latestCollection} latestErrors={latestErrors} />
        </div>

        <MachineTable
          machines={filtered}
          onSelect={setSelected}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      </main>
    </div>
  )
}
