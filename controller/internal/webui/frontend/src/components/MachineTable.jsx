import { relativeTime, shortID } from '../utils.js'
import styles from './MachineTable.module.css'

export default function MachineTable({ machines, onSelect, hasMore, onLoadMore }) {
  return (
    <section className={styles.section}>
      <div className={styles.heading}>
        <div>
          <p className={styles.eyebrow}>Persisted Current State</p>
          <h2 className={styles.title}>Machine inventory</h2>
        </div>
        <span className={styles.count}>{machines.length} result{machines.length !== 1 ? 's' : ''}</span>
      </div>
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Machine</th>
              <th>State</th>
              <th>Instances</th>
              <th>Peers</th>
              <th>Last observed</th>
              <th>EasyTier</th>
            </tr>
          </thead>
          <tbody>
            {machines.map((m) => {
              const instances = m.network_instances || []
              const peers = instances.reduce((s, i) => s + (i.peers || []).length, 0)
              return (
                <tr key={m.machine_id} onClick={() => onSelect?.({ key: `m:${m.machine_id}`, type: 'machine', data: m, label: m.hostname })}>
                  <td>
                    <div className={styles.machineName}>
                      <strong>{m.hostname || 'Unnamed'}</strong>
                      <small>{shortID(m.machine_id)}</small>
                    </div>
                  </td>
                  <td>
                    <span className={`${styles.badge} ${m.active ? styles.active : styles.inactive}`}>
                      {m.active ? 'Active' : 'Inactive'}
                    </span>
                  </td>
                  <td>{instances.length}</td>
                  <td>{peers}</td>
                  <td>{m.last_observed_at ? relativeTime(new Date(m.last_observed_at)) : '—'}</td>
                  <td>{m.easytier_version || '—'}</td>
                </tr>
              )
            })}
            {machines.length === 0 && (
              <tr><td colSpan={6} className={styles.empty}>No machines match the current filter.</td></tr>
            )}
          </tbody>
        </table>
      </div>
      {hasMore && (
        <button className={styles.loadMore} onClick={onLoadMore}>Load more</button>
      )}
    </section>
  )
}
