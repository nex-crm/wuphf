export function renderStatusPaneFallback(root: HTMLElement): void {
  const pane = document.createElement("main");
  pane.className = "grid min-h-screen place-items-center bg-muted px-6 text-foreground";
  pane.setAttribute("aria-label", "Loading WUPHF");

  const inner = document.createElement("section");
  inner.className = "w-full max-w-md rounded-lg border border-border bg-background p-5 shadow-sm";

  const title = document.createElement("h1");
  title.className = "text-base font-semibold";
  title.textContent = "WUPHF";

  const status = document.createElement("p");
  status.className = "mt-2 text-sm text-muted-foreground";
  status.textContent = "Starting renderer";

  inner.append(title, status);
  pane.append(inner);
  root.replaceChildren(pane);
}
