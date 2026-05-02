/** Right-floated Wikipedia-style infobox: dark title band + structured dt/dd. */

import { keyedByOccurrence } from "../../lib/reactKeys";

export interface InfoboxField {
  dt: string;
  dd: string;
}

export interface InfoboxSection {
  fields: InfoboxField[];
}

interface InfoboxProps {
  title: string;
  fields: InfoboxField[];
  sections?: InfoboxSection[];
}

export default function Infobox({ title, fields, sections }: InfoboxProps) {
  return (
    <aside className="wk-infobox" aria-label={`Infobox: ${title}`}>
      <div className="wk-ib-title">{title}</div>
      <div className="wk-ib-body">
        <dl>
          {keyedByOccurrence(fields, (field) => field.dt).map(
            ({ key, value: field }) => (
              <FieldRow key={key} field={field} />
            ),
          )}
        </dl>
        {keyedByOccurrence(sections ?? [], (section) =>
          section.fields.map((field) => field.dt).join("|"),
        ).map(({ key, value: section }) => (
          <div key={key} className="wk-ib-section">
            <dl>
              {keyedByOccurrence(section.fields, (field) => field.dt).map(
                ({ key: fieldKey, value: field }) => (
                  <FieldRow key={fieldKey} field={field} />
                ),
              )}
            </dl>
          </div>
        ))}
      </div>
    </aside>
  );
}

function FieldRow({ field }: { field: InfoboxField }) {
  return (
    <>
      <dt>{field.dt}</dt>
      <dd>{field.dd}</dd>
    </>
  );
}
