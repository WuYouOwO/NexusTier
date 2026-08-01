import styles from './StatCard.module.css'

export default function StatCard({ label, value, sub, accent }) {
  return (
    <div className={`${styles.card} ${accent ? styles.accent : ''}`}>
      <span className={styles.label}>{label}</span>
      <strong className={styles.value}>{value ?? '—'}</strong>
      {sub && <small className={styles.sub}>{sub}</small>}
    </div>
  )
}
