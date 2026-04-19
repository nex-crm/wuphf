/** Amber banner at the top of an article: pulsing dot + live message + meta. */

interface ArticleStatusBannerProps {
  message: string
  liveAgent?: string
  revisions?: number
  contributors?: number
  wordCount?: number
}

export default function ArticleStatusBanner({
  message,
  liveAgent,
  revisions,
  contributors,
  wordCount,
}: ArticleStatusBannerProps) {
  const metaBits: string[] = []
  if (typeof revisions === 'number') metaBits.push(`${revisions} rev`)
  if (typeof contributors === 'number') metaBits.push(`${contributors} contrib`)
  if (typeof wordCount === 'number') metaBits.push(`${wordCount.toLocaleString()} words`)
  return (
    <div className="wk-status-banner" data-testid="wk-status-banner">
      <span className="wk-icon" />
      <span>
        {liveAgent ? <strong>Live: </strong> : <strong>Status: </strong>}
        {message}
      </span>
      {metaBits.length > 0 && <span className="wk-meta">{metaBits.join(' · ')}</span>}
    </div>
  )
}
