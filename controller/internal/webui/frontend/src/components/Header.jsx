import styles from './Header.module.css'

const STATUS_LABEL = { connecting: 'Connecting…', online: 'Live', offline: 'Unavailable' }

export default function Header({ status, updatedAt }) {
  return (
    <header className={styles.header}>
      <div className={styles.brand}>
        <span className={styles.eyebrow}>NexusTier</span>
        <h1 className={styles.title}>Control Plane</h1>
      </div>
      <div className={styles.statusBlock}>
        <span className={`${styles.dot} ${styles[status]}`} aria-hidden="true" />
        <span className={styles.statusLabel}>{STATUS_LABEL[status]}</span>
        {updatedAt && (
          <time className={styles.time} dateTime={updatedAt.toISOString()}>
            Updated {new Intl.DateTimeFormat(undefined, { timeStyle: 'medium' }).format(updatedAt)}
          </time>
        )}
      </div>
    </header>
  )
}
