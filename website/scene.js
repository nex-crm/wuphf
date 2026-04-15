// WUPHF Pixel Office — scene engine
// Loaded by website/index.html. No dependencies.
// See DESIGN.md for the full spec.

(function () {
  'use strict';

  const canvas = document.getElementById('officeCanvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');

  // ── Mobile detection ───────────────────────────────────────────
  const isMobile = window.innerWidth < 768;

  // ── Canvas sizing ──────────────────────────────────────────────
  const W = isMobile ? window.innerWidth : 800;
  const H = isMobile ? 260 : 460;
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

  // ── State ──────────────────────────────────────────────────────
  let flashOn   = true;
  let animF     = 0;
  let drawerHit = null;
  let drawerOpen = false;
  const interactHits = [];  // { id, x, y, w, h }
  let   activeReveal = null; // { id, x, y, content: string[] }
  let   revealTimer  = null;

  setInterval(() => { flashOn = !flashOn; }, 500);
  setInterval(() => { animF = (animF + 1) % 4; }, 280);

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

  // ── Reveal popup ──────────────────────────────────────────────
  function drawReveal(x, y, lines) {
    const PW = 220;
    const PH = lines.length * 14 + 16;
    let rx = Math.min(x, W - PW - 8);
    let ry = Math.max(y - PH - 8, 5);

    ctx.fillStyle = C.surface;
    ctx.fillRect(rx, ry, PW, PH);
    ctx.strokeStyle = C.yellow; ctx.lineWidth = 2;
    ctx.strokeRect(rx, ry, PW, PH);

    ctx.fillStyle    = C.text;
    ctx.font = '7px "Press Start 2P"'; ctx.textAlign = 'left'; ctx.textBaseline = 'top';
    lines.forEach((line, i) => {
      if (line.startsWith('$')) ctx.fillStyle = C.yellow;
      else if (line.startsWith('//')) ctx.fillStyle = C.blue;
      else ctx.fillStyle = C.text;
      ctx.fillText(line, rx + 8, ry + 8 + i * 14);
    });
  }

  // ── Mobile scene (2D fallback) ─────────────────────────────────
  function drawMobileScene() {
    // Dark background
    ctx.fillStyle = C.wall; ctx.fillRect(0, 0, W, H);

    // Floor strip (bottom 60px)
    ctx.fillStyle = C.carpet; ctx.fillRect(0, H - 60, W, 60);
    ctx.fillStyle = C.carpetLine; ctx.fillRect(0, H - 61, W, 2);

    // Back wall stripe
    ctx.fillStyle = '#201C14'; ctx.fillRect(0, 0, W, H - 60);

    // WUPHF sign centered
    const sw = Math.min(280, W - 40), sh = 48;
    const sx = (W - sw) / 2, sy = 14;
    ctx.fillStyle = '#0E0C08'; ctx.fillRect(sx, sy, sw, sh);
    ctx.fillStyle = C.yellow;
    ctx.fillRect(sx, sy, sw, 3); ctx.fillRect(sx, sy + sh - 3, sw, 3);
    ctx.fillRect(sx, sy, 3, sh); ctx.fillRect(sx + sw - 3, sy, 3, sh);
    ctx.shadowColor = C.yellow; ctx.shadowBlur = 10;
    ctx.fillStyle   = C.yellow;
    ctx.font = `bold ${Math.floor(sw / 6)}px "Press Start 2P"`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillText('WUPHF', sx + sw / 2, sy + sh / 2);
    ctx.shadowBlur = 0;

    // Three character silhouettes in a row (Pam, Michael, Dwight)
    const charY = H - 60;
    const positions = [W * 0.25, W * 0.5, W * 0.75];
    const charFns   = [drawPam, drawMichael, drawDwight];
    for (let i = 0; i < 3; i++) {
      const cx = positions[i] - 10;
      const cy = charY - 52;
      ctx.fillStyle = C.shadow;
      ctx.beginPath(); ctx.ellipse(positions[i], charY + 2, 11, 5, 0, 0, Math.PI*2); ctx.fill();
      charFns[i](cx, cy, animF);
    }

    // Hint text
    ctx.fillStyle    = C.textMuted;
    ctx.font = '6px "Press Start 2P"'; ctx.textAlign = 'center'; ctx.textBaseline = 'top';
    ctx.fillText('Best viewed on desktop', W / 2, H - 20);
  }

  // ── Back wall ──────────────────────────────────────────────────
  function drawWall() {
    // Back wall base
    ctx.fillStyle = C.wall;
    ctx.fillRect(0, 0, W, OY + 30);

    // Baseboard strip
    ctx.fillStyle = C.wallLight;
    ctx.fillRect(0, OY + 22, W, 6);

    // Fluorescent light fixtures (4 ceiling-mounted)
    for (let i = 0; i < 4; i++) {
      const lx = 60 + i * 170, ly = 6;
      ctx.fillStyle = '#302820';
      ctx.fillRect(lx, ly, 130, 10);
      ctx.fillStyle = 'rgba(255,254,230,0.6)';
      ctx.fillRect(lx + 4, ly + 2, 122, 6);
      const grad = ctx.createLinearGradient(lx + 65, ly + 8, lx + 65, ly + 40);
      grad.addColorStop(0, 'rgba(255,254,220,0.12)');
      grad.addColorStop(1, 'rgba(255,254,220,0)');
      ctx.fillStyle = grad;
      ctx.fillRect(lx, ly + 8, 130, 32);
    }

    // WUPHF sign (dark panel, golden amber letters, amber glow)
    const sx = 250, sy = 26;
    ctx.fillStyle = '#0E0C08';
    ctx.fillRect(sx, sy, 300, 52);
    ctx.fillStyle = C.yellow;
    ctx.fillRect(sx,       sy,      300, 4);
    ctx.fillRect(sx,       sy + 48, 300, 4);
    ctx.fillRect(sx,       sy,      4,   52);
    ctx.fillRect(sx + 296, sy,      4,   52);
    ctx.fillStyle = 'rgba(236,178,46,0.08)';
    ctx.fillRect(sx + 4, sy + 4, 292, 44);
    ctx.shadowColor = C.yellow;
    ctx.shadowBlur  = 12;
    ctx.fillStyle   = C.yellow;
    ctx.font = 'bold 28px "Press Start 2P"';
    ctx.textAlign    = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('WUPHF', sx + 150, sy + 28);
    ctx.shadowBlur = 0;
    // sign mounting brackets
    ctx.fillStyle = C.deskDark;
    ctx.fillRect(sx + 40,  sy + 50, 8, 14);
    ctx.fillRect(sx + 252, sy + 50, 8, 14);

    // Wall clock (top-right)
    ctx.fillStyle = C.wallLight;
    ctx.beginPath(); ctx.arc(740, 48, 18, 0, Math.PI * 2); ctx.fill();
    ctx.fillStyle = C.surface;
    ctx.beginPath(); ctx.arc(740, 48, 14, 0, Math.PI * 2); ctx.fill();
    ctx.strokeStyle = C.text; ctx.lineWidth = 2;
    ctx.beginPath(); ctx.moveTo(740, 36); ctx.lineTo(740, 48); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(740, 48); ctx.lineTo(750, 53); ctx.stroke();

    // Beet farm map (left side, Dwight's territory)
    const bx = iso(0, 3).x - 65;
    ctx.fillStyle = '#2A2818';
    ctx.fillRect(bx, OY - 22, 50, 38);
    ctx.strokeStyle = '#504830'; ctx.lineWidth = 1.5;
    ctx.strokeRect(bx, OY - 22, 50, 38);
    ctx.fillStyle = '#807020';
    ctx.font = '6px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText('BEET', bx + 25, OY - 7);
    ctx.fillText('FARM', bx + 25, OY + 5);
    ctx.fillStyle = '#2A5018';
    ctx.fillRect(bx + 10, OY + 10, 8, 4);
    ctx.fillRect(bx + 30, OY + 8,  8, 4);
    ctx.fillRect(bx + 18, OY + 6,  8, 4);

    // Conference room partition (left corner)
    const cp = iso(0, 0);
    ctx.fillStyle = '#252018';
    ctx.fillRect(cp.x - 40, OY - 2, 40, 30);
    ctx.strokeStyle = C.border; ctx.lineWidth = 1;
    ctx.strokeRect(cp.x - 40, OY - 2, 40, 30);
    ctx.fillStyle   = C.textMuted;
    ctx.font = '5px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText('CONF', cp.x - 20, OY + 8);
    ctx.fillText('ROOM', cp.x - 20, OY + 17);
  }

  // ── Furniture ──────────────────────────────────────────────────
  // Returns the hit-testable drawer rect: { drawerX, drawerY }
  function drawDesk(gx, gy, w, flash) {
    const DH = 22;
    drawIsoBox(gx, gy, w, 1, DH, C.desk, C.deskDark, C.deskSide);

    // Monitor
    const p  = iso(gx, gy);
    const mx = p.x + TW * w / 2 + 6;
    const my = p.y - DH - 22;
    ctx.fillStyle = '#1A2030'; ctx.fillRect(mx, my, 28, 18);
    ctx.fillStyle = '#1A3858'; ctx.fillRect(mx + 2, my + 2, 24, 14);
    ctx.fillStyle = C.blue;
    for (let i = 0; i < 3; i++) ctx.fillRect(mx + 4, my + 4 + i * 4, 8 + i * 4, 2);
    ctx.fillStyle = '#1A1820';
    ctx.fillRect(mx + 10, my + 18, 8, 5);
    ctx.fillRect(mx + 6,  my + 22, 16, 3);

    // Drawer (flashes amber when flash=true)
    const dp = iso(gx + w - 1, gy);
    const dx = dp.x + 6;
    const dy = dp.y - DH + 6;
    ctx.fillStyle = (flash && flashOn) ? C.yellow : C.deskDark;
    if (flash && flashOn) { ctx.shadowColor = C.yellow; ctx.shadowBlur = 8; }
    ctx.fillRect(dx, dy, 20, 12);
    ctx.shadowBlur = 0;
    ctx.strokeStyle = (flash && flashOn) ? C.yellow : C.deskSide;
    ctx.lineWidth = 1.5;
    ctx.strokeRect(dx, dy, 20, 12);
    ctx.fillStyle = C.yellow;
    ctx.fillRect(dx + 7, dy + 4, 6, 4);

    // Paper on desk
    const pp = iso(gx, gy);
    ctx.fillStyle = C.surfaceHi; ctx.fillRect(pp.x + 16, pp.y - DH - 2, 16, 12);
    ctx.fillStyle = C.border;
    for (let i = 0; i < 3; i++) ctx.fillRect(pp.x + 18, pp.y - DH + 1 + i * 3, 10, 1);

    return { drawerX: dx, drawerY: dy };
  }

  function drawPlant(gx, gy) {
    const c = isoCenter(gx, gy);
    ctx.fillStyle = '#5A3A18'; ctx.fillRect(c.x - 5, c.y - 14, 10, 10);
    ctx.fillStyle = C.plant;   ctx.fillRect(c.x - 10, c.y - 28, 20, 18);
    ctx.fillStyle = '#2A4818'; ctx.fillRect(c.x - 7,  c.y - 32, 14, 8);
    ctx.fillStyle = C.plant;   ctx.fillRect(c.x - 4,  c.y - 36, 8, 10);
  }

  function drawSnackJar(gx, gy) {
    const c = isoCenter(gx, gy);
    ctx.fillStyle = '#3A5878'; ctx.fillRect(c.x - 6, c.y - 18, 12, 14);
    ctx.fillStyle = C.surface; ctx.fillRect(c.x - 5, c.y - 17, 10, 12);
    ctx.fillStyle = C.yellow;  ctx.fillRect(c.x - 3, c.y - 12, 6, 6);
    ctx.fillStyle = C.deskDark; ctx.fillRect(c.x - 5, c.y - 18, 12, 4);
    ctx.fillStyle = C.text;
    ctx.font = '4px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText('NO',    c.x, c.y - 10);
    ctx.fillText('WASTE', c.x, c.y - 6);
  }

  // ── Sprite helpers ─────────────────────────────────────────────
  // Generic head: hair, skin, eyes (dark), mouth
  function drawHead(x, y, hair, skin, f) {
    const b = f < 2 ? 0 : 1;
    ctx.fillStyle = hair; ctx.fillRect(x + 2, y + b,      16, 9);
    ctx.fillStyle = skin; ctx.fillRect(x + 2, y + 7 + b,  16, 13);
    ctx.fillStyle = C.bg; ctx.fillRect(x + 5, y + 10 + b, 3, 3);
                          ctx.fillRect(x + 12, y + 10 + b, 3, 3);
    ctx.fillStyle = '#804040'; ctx.fillRect(x + 7, y + 17 + b, 6, 2);
  }

  // ── Office cast ────────────────────────────────────────────────
  function drawPam(x, y, f) {
    const b = f < 2 ? 0 : 1;
    // Hair bun
    ctx.fillStyle = '#C49838'; ctx.fillRect(x + 5, y - 4 + b, 10, 7);
    drawHead(x, y + b, '#D4A850', C.skin, f);
    // Pink cardigan + white collar
    ctx.fillStyle = '#D09098'; ctx.fillRect(x,     y + 20 + b, 20, 14);
    ctx.fillStyle = C.light;   ctx.fillRect(x + 7, y + 20 + b, 6, 4);
    // Purple skirt
    ctx.fillStyle = '#6868A8'; ctx.fillRect(x, y + 34 + b, 20, 12);
    // Legs + shoes
    ctx.fillStyle = C.skin;    ctx.fillRect(x + 2,  y + 46, 6, 6); ctx.fillRect(x + 12, y + 46, 6, 6);
    ctx.fillStyle = '#2A1808'; ctx.fillRect(x + 2,  y + 52, 8, 4); ctx.fillRect(x + 10, y + 52, 8, 4);
  }

  function drawMichael(x, y, f) {
    const b = f < 2 ? 0 : 1;
    drawHead(x, y + b, '#2A1A0A', C.skin, f);
    // Big smile
    ctx.fillStyle = '#A05050'; ctx.fillRect(x + 4, y + 16 + b, 12, 3);
    // Blue suit
    ctx.fillStyle = '#1A3858'; ctx.fillRect(x, y + 20 + b, 20, 14);
    // White shirt
    ctx.fillStyle = C.light;   ctx.fillRect(x + 7, y + 20 + b, 6, 10);
    // Amber tie (he tries)
    ctx.fillStyle = C.yellow;  ctx.fillRect(x + 9, y + 23 + b, 3, 8);
    // Dark pants + shoes
    ctx.fillStyle = '#0E2840'; ctx.fillRect(x, y + 34 + b, 20, 12);
    ctx.fillStyle = '#0A0E14'; ctx.fillRect(x + 2, y + 46, 7, 4); ctx.fillRect(x + 11, y + 46, 7, 4);
  }

  function drawDwight(x, y, f) {
    const b = f < 2 ? 0 : 1;
    drawHead(x, y + b, '#3A2010', '#C88858', f);
    // Glasses
    ctx.fillStyle = C.bg;
    ctx.fillRect(x + 3, y + 9 + b, 5, 3); ctx.fillRect(x + 11, y + 9 + b, 5, 3);
    ctx.fillRect(x + 8, y + 10 + b, 3, 1);
    // Scowl
    ctx.fillStyle = '#604010'; ctx.fillRect(x + 5, y + 16 + b, 10, 2);
    // Mustard shirt
    ctx.fillStyle = '#A88018'; ctx.fillRect(x, y + 20 + b, 20, 14);
    ctx.fillStyle = C.bg;      ctx.fillRect(x + 7, y + 20 + b, 6, 4);
    // Dark pants
    ctx.fillStyle = '#202020'; ctx.fillRect(x, y + 34 + b, 20, 12);
    ctx.fillStyle = '#080808'; ctx.fillRect(x + 2, y + 46, 7, 4); ctx.fillRect(x + 11, y + 46, 7, 4);
  }

  function drawJim(x, y, f) {
    const b = f < 2 ? 0 : 1;
    drawHead(x, y + b, '#5A3828', C.skin, f);
    // Slightly messy hair
    ctx.fillStyle = '#7A4838'; ctx.fillRect(x + 14, y + 2 + b, 4, 4);
    // Smirk (asymmetric)
    ctx.fillStyle = '#804848'; ctx.fillRect(x + 9, y + 17 + b, 7, 2);
    // Casual blue shirt
    ctx.fillStyle = '#3A5A78'; ctx.fillRect(x, y + 20 + b, 20, 14);
    // Dark slacks
    ctx.fillStyle = '#1A2428'; ctx.fillRect(x, y + 34 + b, 20, 12);
    ctx.fillStyle = '#080C10'; ctx.fillRect(x + 2, y + 46, 7, 4); ctx.fillRect(x + 11, y + 46, 7, 4);
  }

  function drawKevin(x, y, f) {
    const b = f < 2 ? 0 : 1;
    // Kevin is wider
    ctx.fillStyle = '#201810'; ctx.fillRect(x, y + b, 24, 7);
    ctx.fillStyle = '#D09858'; ctx.fillRect(x, y + 5 + b, 24, 15);
    ctx.fillStyle = C.bg;      ctx.fillRect(x + 4, y + 9 + b, 4, 4); ctx.fillRect(x + 16, y + 9 + b, 4, 4);
    ctx.fillStyle = '#805038'; ctx.fillRect(x + 8, y + 17 + b, 8, 2);
    ctx.fillStyle = '#2A3A58'; ctx.fillRect(x, y + 20 + b, 24, 14);
    ctx.fillStyle = '#181818'; ctx.fillRect(x, y + 34 + b, 24, 12);
    ctx.fillStyle = '#080808'; ctx.fillRect(x + 2, y + 46, 8, 4); ctx.fillRect(x + 14, y + 46, 8, 4);
  }

  function drawCreed(x, y, f) {
    const b = f < 3 ? 0 : 1;
    // Gray hair, knowing expression
    drawHead(x, y + b, '#706860', '#B88858', f);
    ctx.fillStyle = '#805038'; ctx.fillRect(x + 4, y + 17 + b, 12, 2);
    // Green shirt (questionable origin)
    ctx.fillStyle = '#284818'; ctx.fillRect(x, y + 20 + b, 20, 14);
    ctx.fillStyle = '#201808'; ctx.fillRect(x, y + 34 + b, 20, 12);
    ctx.fillStyle = '#100808'; ctx.fillRect(x + 2, y + 46, 7, 4); ctx.fillRect(x + 11, y + 46, 7, 4);
  }

  function drawAgent(x, y, color, label, f) {
    const b = f < 2 ? 0 : 1;
    // Robot head block (solid color)
    ctx.fillStyle = color; ctx.fillRect(x + 2, y + b, 16, 14);
    // Screen face
    ctx.fillStyle = C.bg; ctx.fillRect(x + 4, y + 3 + b, 12, 7);
    const eyeCol = color === C.yellow ? '#AA7800' : C.blue;
    ctx.fillStyle = eyeCol;
    ctx.fillRect(x + 5,  y + 4 + b, 4, 4);
    ctx.fillRect(x + 11, y + 4 + b, 4, 4);
    // Blink on frame 3
    if (f === 3) {
      ctx.fillStyle = C.bg;
      ctx.fillRect(x + 5,  y + 6 + b, 4, 2);
      ctx.fillRect(x + 11, y + 6 + b, 4, 2);
    }
    // Body
    ctx.fillStyle = color; ctx.fillRect(x, y + 14 + b, 20, 16);
    // Nameplate badge
    ctx.fillStyle = C.bg;    ctx.fillRect(x + 2, y + 20 + b, 16, 8);
    ctx.fillStyle = color;
    ctx.font = '5px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText(label.substring(0, 3).toUpperCase(), x + 10, y + 27 + b);
    // Legs
    ctx.fillStyle = '#202020';
    ctx.fillRect(x + 2,  y + 30 + b, 7,  16);
    ctx.fillRect(x + 11, y + 30 + b, 7,  16);
    ctx.fillRect(x,      y + 46,     9,  4);
    ctx.fillRect(x + 11, y + 46,     9,  4);
  }

  // ── Characters ─────────────────────────────────────────────────
  const CHARS = [
    { id: 'pam',     name: 'Pam Beesly',    quote: 'WUPHF!',
      gx: 3.5, gy: 0.5, fn: drawPam },
    { id: 'michael', name: 'Michael Scott', quote: "I'm not superstitious, but I am a little stitious.",
      gx: 6.5, gy: 1.5, fn: drawMichael },
    { id: 'dwight',  name: 'Dwight Schrute', quote: 'Bears. Beets. Battlestar Galactica.',
      gx: 1,   gy: 3,   fn: drawDwight },
    { id: 'jim',     name: 'Jim Halpert',    quote: 'How the turntables...',
      gx: 3,   gy: 3,   fn: drawJim },
    { id: 'kevin',   name: 'Kevin Malone',   quote: '... (stares at snacks)',
      gx: 5.5, gy: 4,   fn: drawKevin, wide: true },
    { id: 'creed',   name: 'Creed Bratton',  quote: "Nobody steals from Creed Bratton and gets away with it. The website is fine.",
      gx: 0.5, gy: 5,   fn: drawCreed },
    { id: 'ceo', name: 'CEO Agent',      quote: 'Routing task to engineering team. ETA: 3 minutes.',
      gx: 5.5, gy: 2,   isAgent: true, color: C.yellow,  label: 'CEO' },
    { id: 'eng', name: 'Engineer Agent', quote: 'Implementing feature... 47% complete.',
      gx: 2.5, gy: 4,   isAgent: true, color: C.blue,    label: 'ENG' },
    { id: 'cmo', name: 'CMO Agent',      quote: 'Drafting launch post. You will not believe this lede.',
      gx: 4.2, gy: 3.2, isAgent: true, color: '#5AAA7A', label: 'CMO' },
  ];

  const charHits = [];
  let activeThought = null;
  let thoughtTimer  = null;

  // ── Thought bubble ─────────────────────────────────────────────
  function drawThought(char, centerX, charTopY) {
    if (!activeThought || activeThought.id !== char.id) return;

    const BW = 210, BH = 82;
    let bx = Math.min(centerX - 95, W - BW - 10);
    bx = Math.max(bx, 10);
    let by = Math.max(charTopY - 90, 5);

    // Panel
    ctx.fillStyle = C.surface;
    ctx.fillRect(bx, by, BW, BH);
    ctx.strokeStyle = C.yellow; ctx.lineWidth = 3;
    ctx.strokeRect(bx, by, BW, BH);

    // Tail (triangle below panel pointing to character)
    ctx.beginPath();
    ctx.moveTo(bx + 20, by + BH);
    ctx.lineTo(bx + 30, by + BH + 14);
    ctx.lineTo(bx + 42, by + BH);
    ctx.closePath();
    ctx.fillStyle = C.surface; ctx.fill();
    ctx.strokeStyle = C.yellow; ctx.stroke();
    // Hide inner line of tail
    ctx.fillStyle = C.surface; ctx.fillRect(bx + 21, by + BH - 1, 21, 4);

    // Speaker name
    ctx.fillStyle    = C.yellow;
    ctx.font = '6px "Press Start 2P"'; ctx.textAlign = 'left'; ctx.textBaseline = 'top';
    ctx.fillText(char.name, bx + 10, by + 8);

    // Quote (word-wrapped)
    ctx.fillStyle = C.text;
    ctx.font = '18px "VT323"'; ctx.textBaseline = 'top';
    const words  = char.quote.split(' ');
    let line = '', lineY = by + 22;
    for (const w of words) {
      const test = line ? line + ' ' + w : w;
      if (ctx.measureText(test).width > BW - 20 && line) {
        ctx.fillText(line, bx + 10, lineY);
        line  = w;
        lineY += 18;
        if (lineY > by + BH - 6) { ctx.fillText(line + '...', bx + 10, lineY - 18); return; }
      } else {
        line = test;
      }
    }
    ctx.fillText(line, bx + 10, lineY);
  }

  // ── Main draw ──────────────────────────────────────────────────
  function draw() {
    ctx.clearRect(0, 0, W, H);
    if (isMobile) { drawMobileScene(); return; }
    drawWall();

    // Floor tiles
    for (let gy = 0; gy < ROWS; gy++) {
      for (let gx = 0; gx < COLS; gx++) {
        drawFloorTile(gx, gy, (gx + gy) % 2 === 0 ? C.carpet : C.carpetAlt);
      }
    }

    // Props
    drawPlant(8, 0);
    drawPlant(8, 2);
    drawSnackJar(5, 4);

    // Desks (back-to-front: lower gx+gy first)
    drawerHit = drawDesk(2, 0, 2, true);  // reception desk — flashing drawer
    drawDesk(0, 3, 1, false);             // Dwight's desk
    drawDesk(2, 3, 1, false);             // Jim's desk
    drawDesk(5, 1, 1, false);             // CEO Agent desk (back right)
    drawDesk(2, 4, 1, false);             // Engineer Agent desk
    drawDesk(4, 3, 1, false);             // CMO Agent desk

    // "Click Me!" tooltip above the reception desk flashing drawer
    if (!drawerOpen && flashOn && drawerHit) {
      const { drawerX: dx, drawerY: dy } = drawerHit;
      ctx.fillStyle = C.yellow;
      ctx.fillRect(dx - 4, dy - 22, 72, 18);
      // Down-pointing triangle
      ctx.beginPath();
      ctx.moveTo(dx + 8,  dy - 4);
      ctx.lineTo(dx + 16, dy - 4);
      ctx.lineTo(dx + 12, dy);
      ctx.closePath(); ctx.fill();
      ctx.fillStyle = C.bg;
      ctx.font = '6px "Press Start 2P"'; ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
      ctx.fillText('Click Me!', dx, dy - 13);
    }

    // Drawer reveal
    if (drawerOpen && drawerHit) {
      const { drawerX: dx, drawerY: dy } = drawerHit;
      const rx = dx - 8, ry = dy + 14;
      ctx.fillStyle = C.surface;
      ctx.fillRect(rx, ry, 190, 44);
      ctx.strokeStyle = C.yellow; ctx.lineWidth = 2;
      ctx.strokeRect(rx, ry, 190, 44);
      ctx.fillStyle = C.text;
      ctx.font = '7px "Press Start 2P"'; ctx.textAlign = 'left'; ctx.textBaseline = 'top';
      ctx.fillText('One command.', rx + 8, ry + 6);
      ctx.fillText('One office.', rx + 8, ry + 18);
      ctx.fillStyle = C.yellow;
      ctx.fillText('./wuphf', rx + 8, ry + 30);
    }

    // Register interactable hit areas (rebuilt each frame from current positions)
    interactHits.length = 0;

    // 1. Paper on Pam's desk
    const pamDesk = iso(2, 0);
    interactHits.push({ id: 'paper', x: pamDesk.x + 16, y: pamDesk.y - 22 - 2, w: 16, h: 12,
      content: ['CEO, PM, engineers,', 'all visible,', 'all working.'] });

    // 2. Conference room whiteboard
    const cp2 = iso(0, 0);
    interactHits.push({ id: 'whiteboard', x: cp2.x - 40, y: OY - 2, w: 40, h: 30,
      content: ['agent → broker', '→ channel', '→ human'] });

    // 3. Agent monitor (CEO desk)
    const ceoDesk = iso(5, 1);
    interactHits.push({ id: 'monitor', x: ceoDesk.x + TW / 2 + 6, y: ceoDesk.y - 22 - 22, w: 28, h: 18,
      content: ['Open source.', 'MIT license.', '$ go build -o wuphf'] });

    // 4. Plant (first one — at gx=8,gy=0)
    const plantC2 = isoCenter(8, 0);
    interactHits.push({ id: 'plant', x: plantC2.x - 10, y: plantC2.y - 36, w: 20, h: 36,
      content: ["Unlike Ryan Howard's", 'WUPHF, this one works.'] });

    // 5. Snack jar (Kevin's area)
    const jarC2 = isoCenter(5, 4);
    interactHits.push({ id: 'snackjar', x: jarC2.x - 6, y: jarC2.y - 18, w: 12, h: 14,
      content: ['No tokens wasted', 'on pleasantries.'] });

    // 6. Beet farm map (Dwight's wall)
    const bx2 = iso(0, 3).x - 65;
    interactHits.push({ id: 'beetmap', x: bx2, y: OY - 22, w: 50, h: 38,
      content: ['Identity theft is', 'not a joke, Jim!', '(easter egg found)'] });

    // 7. Dundie award — small icon near conference room
    const cp3 = iso(0, 0);
    interactHits.push({ id: 'dundie', x: cp3.x - 36, y: OY + 18, w: 18, h: 18,
      content: ['Best AI Office,', '2025.', '(Self-awarded.)'] });

    // 8. Break room fridge — draw + register
    const fridgeX = iso(8, 4).x + TW / 2 - 8;
    const fridgeY = iso(8, 4).y - 20;
    ctx.fillStyle = '#2A3040'; ctx.fillRect(fridgeX, fridgeY, 16, 20);
    ctx.fillStyle = '#1A2030'; ctx.fillRect(fridgeX + 1, fridgeY + 10, 14, 9);
    ctx.fillStyle = C.border;  ctx.fillRect(fridgeX + 13, fridgeY + 3, 2, 6);
    if (flashOn) {
      ctx.shadowColor = C.blue; ctx.shadowBlur = 4;
      ctx.fillStyle   = C.blue; ctx.fillRect(fridgeX + 3, fridgeY + 5, 8, 3);
      ctx.shadowBlur  = 0;
    }
    interactHits.push({ id: 'fridge', x: fridgeX, y: fridgeY, w: 16, h: 20,
      content: ['$ git clone', '  github.com/', '  nex-crm/wuphf', '$ ./wuphf'] });

    // Draw active reveal
    if (activeReveal) {
      drawReveal(activeReveal.x, activeReveal.y, activeReveal.content);
    }

    // Characters (back-to-front sort by gx+gy)
    charHits.length = 0;
    const sorted = [...CHARS].sort((a, b) => (a.gx + a.gy) - (b.gx + b.gy));
    for (const char of sorted) {
      const c  = isoCenter(char.gx, char.gy);
      const cw = char.wide ? 24 : 20;
      const cx = c.x - cw / 2 - 2;
      const cy = c.y - 52;

      // Shadow
      ctx.fillStyle = C.shadow;
      ctx.beginPath();
      ctx.ellipse(c.x, c.y + 2, char.wide ? 14 : 11, 5, 0, 0, Math.PI * 2);
      ctx.fill();

      // Sprite (agent or cast)
      if (char.isAgent) {
        drawAgent(cx, cy, char.color, char.label, animF);
      } else {
        char.fn(cx, cy, animF);
      }

      // Nametag: Pam (so visitors know who she is) + all agents
      if (char.id === 'pam' || char.isAgent) {
        const tagColor  = char.isAgent ? char.color : C.yellow;
        const firstName = char.name.split(' ')[0].substring(0, 8);
        const tagW      = firstName.length * 6 + 16;
        ctx.fillStyle   = tagColor;
        ctx.fillRect(c.x - tagW / 2, cy - 14, tagW, 11);
        ctx.fillStyle    = C.bg;
        ctx.font = '5px "Press Start 2P"'; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
        ctx.fillText(firstName, c.x, cy - 8);
      }

      drawThought(char, c.x, cy);

      charHits.push({ char, cx, cy, w: cw + 4, h: 54 });
    }
  }

  function loop() { draw(); requestAnimationFrame(loop); }
  loop();

  canvas.addEventListener('click', e => {
    const rect = canvas.getBoundingClientRect();
    const scaleX = W / rect.width;
    const scaleY = H / rect.height;
    const mx = (e.clientX - rect.left) * scaleX;
    const my = (e.clientY - rect.top)  * scaleY;

    // Check reception drawer click
    if (drawerHit) {
      const { drawerX: dx, drawerY: dy } = drawerHit;
      if (mx >= dx - 4 && mx <= dx + 74 &&
          my >= dy - 26 && my <= dy + 14) {
        drawerOpen = !drawerOpen;
        return;
      }
    }

    // Check interactable props
    for (const hit of interactHits) {
      if (mx >= hit.x && mx <= hit.x + hit.w &&
          my >= hit.y && my <= hit.y + hit.h) {
        if (activeReveal && activeReveal.id === hit.id) {
          activeReveal = null;
        } else {
          activeReveal = { id: hit.id, x: hit.x + hit.w / 2, y: hit.y, content: hit.content };
          clearTimeout(revealTimer);
          revealTimer = setTimeout(() => { activeReveal = null; }, 5000);
        }
        return;
      }
    }

    // Check characters
    for (const hit of charHits) {
      if (mx >= hit.cx && mx <= hit.cx + hit.w &&
          my >= hit.cy && my <= hit.cy + hit.h) {
        if (activeThought && activeThought.id === hit.char.id) {
          activeThought = null;
        } else {
          activeThought = hit.char;
          clearTimeout(thoughtTimer);
          thoughtTimer = setTimeout(() => {
            if (activeThought && activeThought.id === hit.char.id) activeThought = null;
          }, 5000);
        }
        return;
      }
    }

    // Click elsewhere — dismiss thought bubble
    activeThought = null;
  });

  canvas.addEventListener('mousemove', e => {
    const rect   = canvas.getBoundingClientRect();
    const scaleX = W / rect.width;
    const scaleY = H / rect.height;
    const mx = (e.clientX - rect.left) * scaleX;
    const my = (e.clientY - rect.top)  * scaleY;

    let pointer = false;

    // Drawer
    if (drawerHit) {
      const { drawerX: dx, drawerY: dy } = drawerHit;
      if (mx >= dx - 4 && mx <= dx + 74 && my >= dy - 26 && my <= dy + 14) pointer = true;
    }

    // Characters
    if (!pointer) {
      for (const hit of charHits) {
        if (mx >= hit.cx && mx <= hit.cx + hit.w &&
            my >= hit.cy && my <= hit.cy + hit.h) { pointer = true; break; }
      }
    }

    // Interactables
    if (!pointer) {
      for (const hit of interactHits) {
        if (mx >= hit.x && mx <= hit.x + hit.w &&
            my >= hit.y && my <= hit.y + hit.h) { pointer = true; break; }
      }
    }

    canvas.style.cursor = pointer ? 'pointer' : 'default';
  });

})();
