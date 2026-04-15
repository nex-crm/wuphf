// WUPHF Pixel Office — scene engine
// Loaded by website/index.html. No dependencies.
// See DESIGN.md for the full spec.

(function () {
  'use strict';

  const canvas = document.getElementById('officeCanvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');

  // ── Canvas sizing ──────────────────────────────────────────────
  const W = 800, H = 460;
  canvas.width  = W;
  canvas.height = H;
  canvas.style.width  = '100%';
  canvas.style.height = 'auto';

  // ── Design tokens ──────────────────────────────────────────────
  const C = {
    bg:         '#1A1610',
    surface:    '#242018',
    surfaceHi:  '#2E2820',
    border:     '#3A3028',
    text:       '#F0EBD8',
    textMuted:  '#8A7D6A',
    yellow:     '#ECB22E',
    yellowDark: '#C49020',
    blue:       '#5A9AC8',
    green:      '#5AAA7A',
    carpet:     '#3A3228',
    carpetAlt:  '#302A20',
    carpetLine: '#2A2418',
    wall:       '#201C14',
    wallLight:  '#2A2418',
    desk:       '#7A5A18',
    deskDark:   '#5A3C08',
    deskSide:   '#3A2404',
    skin:       '#F4C890',
    light:      '#FFFEF0',
    shadow:     'rgba(0,0,0,0.5)',
    plant:      '#3A6028',
  };

  // ── Isometric grid ─────────────────────────────────────────────
  const TW = 60, TH = 30;
  const OX = 420, OY = 100;
  const COLS = 9, ROWS = 6;

  function iso(gx, gy) {
    return {
      x: OX + (gx - gy) * TW / 2,
      y: OY + (gx + gy) * TH / 2,
    };
  }
  function isoCenter(gx, gy) {
    const p = iso(gx, gy);
    return { x: p.x + TW / 2, y: p.y + TH / 2 };
  }

  // ── Floor tile ─────────────────────────────────────────────────
  function drawFloorTile(gx, gy, color) {
    const p = iso(gx, gy);
    ctx.beginPath();
    ctx.moveTo(p.x + TW / 2, p.y);
    ctx.lineTo(p.x + TW,     p.y + TH / 2);
    ctx.lineTo(p.x + TW / 2, p.y + TH);
    ctx.lineTo(p.x,           p.y + TH / 2);
    ctx.closePath();
    ctx.fillStyle = color;
    ctx.fill();
    ctx.strokeStyle = C.carpetLine;
    ctx.lineWidth = 0.5;
    ctx.stroke();
  }

  // ── Iso box (w tiles wide × d tiles deep × h px tall) ─────────
  function drawIsoBox(gx, gy, w, d, h, top, left, right) {
    const p0 = iso(gx,     gy);
    const pw = iso(gx + w, gy);
    const pd = iso(gx,     gy + d);
    const pf = iso(gx + w, gy + d);

    // top face
    ctx.beginPath();
    ctx.moveTo(p0.x + TW/2, p0.y - h);
    ctx.lineTo(pw.x + TW/2, pw.y - h);
    ctx.lineTo(pf.x + TW/2, pf.y - h);
    ctx.lineTo(pd.x + TW/2, pd.y - h);
    ctx.closePath();
    ctx.fillStyle = top; ctx.fill();

    // left face
    ctx.beginPath();
    ctx.moveTo(p0.x + TW/2, p0.y - h);
    ctx.lineTo(pd.x + TW/2, pd.y - h);
    ctx.lineTo(pd.x + TW/2, pd.y);
    ctx.lineTo(p0.x + TW/2, p0.y);
    ctx.closePath();
    ctx.fillStyle = left; ctx.fill();

    // right face
    ctx.beginPath();
    ctx.moveTo(pw.x + TW/2, pw.y - h);
    ctx.lineTo(pf.x + TW/2, pf.y - h);
    ctx.lineTo(pf.x + TW/2, pf.y);
    ctx.lineTo(pw.x + TW/2, pw.y);
    ctx.closePath();
    ctx.fillStyle = right; ctx.fill();
  }

  // ── Main draw ──────────────────────────────────────────────────
  function draw() {
    ctx.clearRect(0, 0, W, H);
    ctx.fillStyle = C.wall;
    ctx.fillRect(0, 0, W, OY + 30);

    for (let gy = 0; gy < ROWS; gy++) {
      for (let gx = 0; gx < COLS; gx++) {
        drawFloorTile(gx, gy, (gx + gy) % 2 === 0 ? C.carpet : C.carpetAlt);
      }
    }
  }

  function loop() { draw(); requestAnimationFrame(loop); }
  loop();

})();
