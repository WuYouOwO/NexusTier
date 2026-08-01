import styles from './Toolbar.module.css'

export default function Toolbar({ activeFilter, onFilterChange, interval, onIntervalChange, onRefresh, loading, search, onSearchChange }) {
  return (
    <div className={styles.toolbar}>
      <label className={styles.field}>
        <span>搜索</span>
        <input
          type="search"
          value={search}
          onChange={e => onSearchChange(e.target.value)}
          placeholder="主机名、节点 ID、IP 地址…"
          className={styles.input}
        />
      </label>
      <label className={styles.field}>
        <span>状态</span>
        <select value={activeFilter} onChange={e => onFilterChange(e.target.value)} className={styles.select}>
          <option value="true">活跃节点</option>
          <option value="">全部节点</option>
          <option value="false">非活跃</option>
        </select>
      </label>
      <label className={styles.field}>
        <span>刷新</span>
        <select value={interval} onChange={e => onIntervalChange(Number(e.target.value))} className={styles.select}>
          <option value={5000}>5 秒</option>
          <option value={15000}>15 秒</option>
          <option value={30000}>30 秒</option>
          <option value={0}>手动</option>
        </select>
      </label>
      <button className={styles.btn} onClick={onRefresh} disabled={loading}>
        {loading ? '刷新中…' : '立即刷新'}
      </button>
    </div>
  )
}
