import { shortID, formatDate, formatBytes, formatLatency, formatLoss, deviceLabel, relativeTime } from '../utils.js'
import styles from './DetailsPanel.module.css'

function Row({ label, value }) {
  if (value == null || value === '') return null
  return (
    <>
      <dt className={styles.dt}>{label}</dt>
      <dd className={styles.dd}>{value}</dd>
    </>
  )
}

function Tags({ items }) {
  if (!items?.length) return null
  return (
    <div className={styles.tags}>
      {items.map((t, i) => <span key={i} className={styles.tag}>{t}</span>)}
    </div>
  )
}

function IssueList({ errors }) {
  if (!errors?.length) return <p className={styles.healthy}>无采集错误</p>
  return (
    <div className={styles.issues}>
      {errors.map((e, i) => (
        <article key={i} className={styles.issue}>
          <strong>{e.code}</strong>
          <span>{e.operation} · {e.message}</span>
        </article>
      ))}
    </div>
  )
}

export default function DetailsPanel({ selected, latestCollection, latestErrors }) {
  if (!selected) {
    return (
      <aside className={styles.panel}>
        <div className={styles.header}>
          <p className={styles.eyebrow}>当前采集</p>
          <h2 className={styles.title}>最新 Collection</h2>
        </div>
        <div className={styles.body}>
          {latestCollection ? (
            <>
              <dl className={styles.grid}>
                <Row label="采集 ID"  value={shortID(latestCollection.collection_id)} />
                <Row label="状态"     value={latestCollection.status === 'complete' ? '完整' : latestCollection.status === 'partial' ? '部分成功' : latestCollection.status} />
                <Row label="节点数"   value={latestCollection.machine_count} />
                <Row label="错误数"   value={latestCollection.error_count} />
                <Row label="完成时间" value={formatDate(new Date(latestCollection.completed_at))} />
                <Row label="入库时间" value={formatDate(new Date(latestCollection.ingested_at))} />
              </dl>
              <IssueList errors={(latestErrors || []).slice(0, 6)} />
            </>
          ) : (
            <p className={styles.placeholder}>等待控制器完成首次采集…</p>
          )}
        </div>
      </aside>
    )
  }

  if (selected.type === 'machine') {
    const m = selected.data
    const instances = m.network_instances || []
    return (
      <aside className={styles.panel}>
        <div className={styles.header}>
          <p className={styles.eyebrow}>节点详情</p>
          <h2 className={styles.title}>{m.hostname || '未命名节点'}</h2>
          <span className={`${styles.badge} ${m.active ? styles.active : styles.inactive}`}>
            {m.active ? '活跃' : '非活跃'}
          </span>
        </div>
        <div className={styles.body}>
          <dl className={styles.grid}>
            <Row label="Machine ID"  value={m.machine_id} />
            <Row label="远端地址"    value={m.remote_url} />
            <Row label="EasyTier"    value={m.easytier_version} />
            <Row label="操作系统"    value={deviceLabel(m.device)} />
            <Row label="实例数"      value={instances.length} />
            <Row label="最近心跳"    value={m.last_heartbeat_at ? relativeTime(new Date(m.last_heartbeat_at)) : null} />
            <Row label="最近观测"    value={m.last_observed_at ? relativeTime(new Date(m.last_observed_at)) : null} />
          </dl>
          <Tags items={instances.map(i => shortID(i.instance_id))} />
        </div>
      </aside>
    )
  }

  if (selected.type === 'peer') {
    const p = selected.data
    return (
      <aside className={styles.panel}>
        <div className={styles.header}>
          <p className={styles.eyebrow}>Peer 详情</p>
          <h2 className={styles.title}>{p.hostname || `Peer ${p.peer_id}`}</h2>
          <span className={`${styles.badge} ${p.direct ? styles.direct : styles.relayed}`}>
            {p.direct ? '直连' : '中继'}
          </span>
        </div>
        <div className={styles.body}>
          <dl className={styles.grid}>
            <Row label="Peer ID"   value={p.peer_id} />
            <Row label="IPv4"      value={p.ipv4} />
            <Row label="延迟"      value={formatLatency(p.latency_ms)} />
            <Row label="丢包率"    value={formatLoss(p.loss_rate)} />
            <Row label="接收"      value={formatBytes(p.rx_bytes)} />
            <Row label="发送"      value={formatBytes(p.tx_bytes)} />
            <Row label="路径代价"  value={p.path_cost} />
            <Row label="下一跳"    value={p.next_hop_peer_id} />
            <Row label="最近观测"  value={p.last_observed_at ? relativeTime(new Date(p.last_observed_at)) : null} />
          </dl>
          <Tags items={p.tunnel_protocols} />
        </div>
      </aside>
    )
  }

  return null
}
