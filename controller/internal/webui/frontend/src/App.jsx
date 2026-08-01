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
  const updatedAt = useMemo(() => new Date(), [])

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
          i.instance_id, i.node?.hostname, i.node?.ipv4,
          ...(i.peers || []).flatMap(p => [p.hostname, p.ipv4, String(p.peer_id)]),
        ]),
      ]
      return texts.filter(Boolean).some(t => String(t).toLowerCase().includes(q))
    })
  }, [machines, search])

  const instances = filtered.flatMap(m => m.network_instances || [])
  const peers = instances.flatMap(i => i.peers || [])
  const inactive = filtered.filter(m => !m.active).length
  const direct = peers.filter(p => p.direct).length
  const freshness = latestCollection
    ? relativeTime(new Date(latestCollection.completed_at))
    : '暂无数据'
  const collectionSub = latestCollection
    ? `${latestCollection.status === 'complete' ? '完整' : latestCollection.status === 'partial' ? '部分' : latestCollection.status} · ${latestCollection.error_count} 错误`
    : '等待采集'

  return (
    <div className={styles.app}>
      <Header status={status} updatedAt={updatedAt} />

      <main className={styles.main}>
        {/* 统计卡片 */}
        <div className={styles.statsRow}>
          <StatCard label="节点总数"   value={filtered.length}    sub={`${inactive} 非活跃`}          color="blue" />
          <StatCard label="网络实例"   value={instances.length}   sub={`${instances.filter(i=>i.node).length} 含节点信息`} color="blue" />
          <StatCard label="Peer 路由"  value={peers.length}       sub={`${direct} 直连`}              color="green" />
          <StatCard label="数据新鲜度" value={freshness}          sub={collectionSub}                 color="amber" />
        </div>

        {/* 工具栏 */}
        <Toolbar
          search={search} onSearchChange={setSearch}
          activeFilter={activeFilter} onFilterChange={v => { setActiveFilter(v); setSelected(null) }}
          interval={interval} onIntervalChange={setIntervalMs}
          onRefresh={refresh} loading={loading}
        />

        {/* 拓扑图 + 详情面板 */}
        <div className={styles.workspace}>
          <div className={styles.graphCard}>
            <div className={styles.graphHeader}>
              <div>
                <p className={styles.eyebrow}>实时控制视图</p>
                <h2 className={styles.sectionTitle}>网络拓扑图</h2>
              </div>
              <div className={styles.legend}>
                <span><i className={`${styles.dot} ${styles.mNode}`} />节点</span>
                <span><i className={`${styles.dot} ${styles.pNode}`} />Peer</span>
                <span><i className={styles.lineD} />直连</span>
                <span><i className={styles.lineR} />中继</span>
              </div>
            </div>
            <TopologyGraph machines={filtered} selected={selected} onSelect={setSelected} />
          </div>
          <DetailsPanel
            selected={selected}
            latestCollection={latestCollection}
            latestErrors={latestErrors}
          />
        </div>

        {/* 节点清单 */}
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
