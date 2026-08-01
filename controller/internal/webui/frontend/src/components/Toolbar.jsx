import styles from './Toolbar.module.css'

export default function Toolbar({ activeFilter, onFilterChange, interval, onIntervalChange, onRefresh, loading, search, onSearchChange }) {
  return (
    <div className={styles.toolbar}>
      <label className={styles.field}>
        <span>Filter</span>
        <input
          type="search"
          value={search}
          onChange={e => onSearchChange(e.target.value)}
          placeholder="Hostname, machine, instance, IP…"
          className={styles.input}
        />
      </label>
      <label className={styles.field}>
        <span>State</span>
        <select value={activeFilter} onChange={e => onFilterChange(e.target.value)} className={styles.select}>
          <option value="true">Active</option>
          <option value="">All</option>
          <option value="false">Inactive</option>
        </select>
      </label>
      <label className={styles.field}>
        <span>Refresh</span>
        <select value={interval} onChange={e => onIntervalChange(Number(e.target.value))} className={styles.select}>
          <option value={5000}>5 s</option>
          <option value={15000}>15 s</option>
          <option value={30000}>30 s</option>
          <option value={0}>Manual</option>
        </select>
      </label>
      <button className={styles.btn} onClick={onRefresh} disabled={loading}>
        {loading ? 'Loading…' : 'Refresh'}
      </button>
    </div>
  )
}
