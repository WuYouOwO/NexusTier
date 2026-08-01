import styles from './StatCard.module.css'

export default function StatCard({ label, value, sub, color }) {
  return (
    <div className={`${styles.card} ${color ? styles[color] : ''}`}>
      <span className={styles.label}>{label}</span>
      <strong className={styles.value}>{value ?? '—'}</strong>
      {sub && <small className={styles.sub}>{sub}</small>}
    </div>
  )
}
