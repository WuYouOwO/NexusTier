import { shortID, formatDate, formatBytes, formatLatency, formatLoss, deviceLabel, relativeTime } from '../utils.js'
import styles from './DetailsPanel.module.css'

function DetailGrid({ rows }) {
  return (
    <dl className={styles.grid}>
      {rows.map(([k, v]) => v != null && v !== '' && (
        <>
          <dt key={`k-${k}`} className={styles.dt}>{k}</dt>
          <dd key={`v-${k}`} className={styles.dd}>{v}</dd>
        </>
      ))}
    </dl>
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
  if (!errors?.length) return <p className={styles.healthy}>No collection errors.</p>
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
          <p className={styles.eyebrow}>Selection</p>
          <h2 className={styles.title}>Latest collection</h2>
        </div>
        <div className={styles.body}>
          {latestCollection ? (
            <>
              <DetailGrid rows={[
                ['Collection', shortID(latestCollection.collection_id)],
                ['Status', latestCollection.status],
                ['Machines', latestCollection.machine_count],
                ['Errors', latestCollection.error_count],
                ['Completed', formatDate(new Date(latestCollection.completed_at))],
                ['Ingested', formatDate(new Date(latestCollection.ingested_at))],
              ]} />
              <IssueList errors={(latestErrors || []).slice(0, 6)} />
            </>
          ) : (
            <p className={styles.placeholder}>No topology collection yet. Waiting for controller to poll.</p>
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
          <p className={styles.eyebrow}>Machine</p>
          <h2 className={styles.title}>{m.hostname || 'Unnamed'}</h2>
          <span className={`${styles.badge} ${m.active ? styles.active : styles.inactive}`}>
            {m.active ? 'Active' : 'Inactive'}
          </span>
        </div>
        <div className={styles.body}>
          <DetailGrid rows={[
            ['Machine ID', m.machine_id],
            ['Remote', m.remote_url],
            ['EasyTier', m.easytier_version],
            ['OS', deviceLabel(m.device)],
            ['Instances', instances.length],
            ['Last heartbeat', m.last_heartbeat_at ? relativeTime(new Date(m.last_heartbeat_at)) : null],
            ['Last observed', m.last_observed_at ? relativeTime(new Date(m.last_observed_at)) : null],
          ]} />
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
          <p className={styles.eyebrow}>Peer</p>
          <h2 className={styles.title}>{p.hostname || `Peer ${p.peer_id}`}</h2>
          <span className={`${styles.badge} ${p.direct ? styles.direct : styles.relayed}`}>
            {p.direct ? 'Direct' : 'Relayed'}
          </span>
        </div>
        <div className={styles.body}>
          <DetailGrid rows={[
            ['Peer ID', p.peer_id],
            ['IPv4', p.ipv4],
            ['Latency', formatLatency(p.latency_ms)],
            ['Loss', formatLoss(p.loss_rate)],
            ['RX', formatBytes(p.rx_bytes)],
            ['TX', formatBytes(p.tx_bytes)],
            ['Path cost', p.path_cost],
            ['Next hop', p.next_hop_peer_id],
            ['Observed', p.last_observed_at ? relativeTime(new Date(p.last_observed_at)) : null],
          ]} />
          <Tags items={p.tunnel_protocols} />
        </div>
      </aside>
    )
  }

  return null
}
