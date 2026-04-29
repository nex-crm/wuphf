import { useAppStore } from "../../stores/app";

const PRESETS: { label: string; value: string | null }[] = [
  { label: "Default", value: null },
  { label: "Noir", value: "#0d0d10" },
  { label: "Slate", value: "#1f2933" },
  { label: "Forest", value: "#16321f" },
  { label: "Burgundy", value: "#3a1620" },
  { label: "Indigo", value: "#1c1f3d" },
];

export function SidebarColorPicker() {
  const sidebarBg = useAppStore((s) => s.sidebarBg);
  const setSidebarBg = useAppStore((s) => s.setSidebarBg);

  return (
    <div className="sidebar-color-picker">
      <div className="sidebar-color-picker-label">Sidebar color</div>
      <div className="sidebar-color-picker-row">
        {PRESETS.map((p) => {
          const active = (p.value ?? null) === (sidebarBg ?? null);
          return (
            <button
              key={p.label}
              type="button"
              className={`sidebar-color-swatch${active ? " is-active" : ""}`}
              title={p.label}
              aria-label={p.label}
              aria-pressed={active}
              onClick={() => setSidebarBg(p.value)}
              style={
                p.value
                  ? { background: p.value }
                  : {
                      background:
                        "repeating-linear-gradient(45deg, var(--border) 0 4px, transparent 4px 8px)",
                    }
              }
            />
          );
        })}
        <label
          className="sidebar-color-swatch sidebar-color-swatch-custom"
          title="Custom color"
        >
          <input
            type="color"
            value={sidebarBg ?? "#1f2933"}
            onChange={(e) => setSidebarBg(e.target.value)}
          />
          <span aria-hidden="true">+</span>
        </label>
      </div>
    </div>
  );
}
