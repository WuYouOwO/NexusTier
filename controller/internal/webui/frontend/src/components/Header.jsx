import styles from './Header.module.css'

const STATUS = {
  connecting: { label: '连接中',   cls: 'connecting' },
  online:     { label: '运行正常', cls: 'online' },
  offline:    { label: '连接失败', cls: 'offline' },
}

export default function Header({ status, updatedAt }) {
  const s = STATUS[status] || STATUS.connecting
  return (
    <header className={styles.header}>
      <div className={styles.left}>
        <div className={styles.logo}>
          <span className={styles.logoMark}>N</span>
          <span className={styles.logoName}>NexusTier</span>
        </div>
        <nav className={styles.nav}>
          <span className={`${styles.navItem} ${styles.active}`}>拓扑概览</span>
          <span className={styles.navItem}>节点管理</span>
          <span className={styles.navItem}>网络状态</span>
        </nav>
      </div>
      <div className={styles.right}>
        {updatedAt && (
          <span className={styles.time}>
            最后更新：{new Intl.DateTimeFormat('zh-CN', { timeStyle: 'medium' }).format(updatedAt)}
          </span>
        )}
        <div className={`${styles.badge} ${styles[s.cls]}`}>
          <i className={styles.dot} />
          {s.label}
        </div>
      </div>
    </header>
  )
}
