/** Right-floated Wikipedia-style infobox: dark title band + structured dt/dd. */

export interface InfoboxField {
  dt: string
  dd: string
}

export interface InfoboxSection {
  fields: InfoboxField[]
}

interface InfoboxProps {
  title: string
  fields: InfoboxField[]
  sections?: InfoboxSection[]
}

export default function Infobox({ title, fields, sections }: InfoboxProps) {
  return (
    <aside className="wk-infobox" aria-label={`Infobox: ${title}`}>
      <div className="wk-ib-title">{title}</div>
      <div className="wk-ib-body">
        <dl>
          {fields.map((f) => (
            <FieldRow key={f.dt} field={f} />
          ))}
        </dl>
        {sections?.map((section, i) => (
          <div key={`ib-section-${i}`} className="wk-ib-section">
            <dl>
              {section.fields.map((f) => (
                <FieldRow key={f.dt} field={f} />
              ))}
            </dl>
          </div>
        ))}
      </div>
    </aside>
  )
}

function FieldRow({ field }: { field: InfoboxField }) {
  return (
    <>
      <dt>{field.dt}</dt>
      <dd>{field.dd}</dd>
    </>
  )
}
