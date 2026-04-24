/** Fraunces article title + italic strapline + thick horizontal divider. */

interface ArticleTitleProps {
  title: string;
  strapline?: string;
}

const DEFAULT_STRAPLINE = "From Team Wiki, your team's encyclopedia.";

export default function ArticleTitle({
  title,
  strapline = DEFAULT_STRAPLINE,
}: ArticleTitleProps) {
  return (
    <>
      <h1 className="wk-article-title">{title}</h1>
      <div className="wk-strapline">{strapline}</div>
      <hr className="wk-title-divider" />
    </>
  );
}
