import { relativeTime, shortID } from '../utils.js'
import styles from './MachineTable.module.css'

export default function MachineTable({ machines, onSelect, hasMore, onLoadMore }) {
  return (
    <section className={styles.section}>
      <div className={styles.heading}>
        <div>
          <p className={styles.eyebrow}>持久化当前状态</p>
          <h2 className={styles.title}>节点清单</h2>
        </div>
        <span className={styles.count}>{machines.length} 条记录</span>
      </div>
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>节点名称</th>
              <th>状态</th>
              <th>实例数</th>
              <th>Peer 数</th>
              <th>最近观测</th>
              <th>版本</th>
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
                      <strong>{m.hostname || '未命名'}</strong>
                      <small>{shortID(m.machine_id)}</small>
                    </div>
                  </td>
                  <td>
                    <span className={`${styles.badge} ${m.active ? styles.active : styles.inactive}`}>
                      {m.active ? '活跃' : '非活跃'}
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
              <tr><td colSpan={6} className={styles.empty}>当前筛选条件下无节点数据</td></tr>
            )}
          </tbody>
        </table>
      </div>
      {hasMore && (
        <button className={styles.loadMore} onClick={onLoadMore}>加载更多</button>
      )}
    </section>
  )
}
