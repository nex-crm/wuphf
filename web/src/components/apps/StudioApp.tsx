export function StudioApp() {
  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      padding: '60px 20px',
      textAlign: 'center',
      flex: 1,
    }}>
      <div style={{ fontSize: 32, marginBottom: 16 }}>{'\uD83C\uDFAC'}</div>
      <h3 style={{ fontSize: 18, fontWeight: 700, marginBottom: 8 }}>Studio</h3>
      <p style={{ fontSize: 14, color: 'var(--text-secondary)', maxWidth: 400, lineHeight: 1.5 }}>
        The control room is coming soon. Studio handles blueprints, workflow orchestration,
        and broker memory. It will be wired up in a future pass.
      </p>
    </div>
  )
}
