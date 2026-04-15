// WUPHF Pixel Office — scene engine
// Loaded by website/index.html. No dependencies.

(function () {
  'use strict';

  const canvas = document.getElementById('officeCanvas');
  if (!canvas) return;
  const ctx = canvas.getContext('2d');

  // ── Mobile detection ──────────────────────────────────────────
  const isMobile = window.innerWidth < 768;

  // ── Canvas sizing (full viewport, DPR-aware) ──────────────────
  const W = window.innerWidth;
  const H = isMobile ? 260 : (window.innerHeight - 44);
  const DPR = window.devicePixelRatio || 1;

  canvas.width  = Math.round(W * DPR);
  canvas.height = Math.round(H * DPR);
  canvas.style.width  = W + 'px';
  canvas.style.height = H + 'px';
  ctx.scale(DPR, DPR);

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
  const COLS = 12, ROWS = 8;
  const TW = 80, TH = 40;
  const OX = Math.round(W * 0.52);
  const OY = 150;

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

  // ── Animation state ────────────────────────────────────────────
  let animF = 0;
  setInterval(() => { animF = (animF + 1) % 4; }, 280);

  // ── Ambient speech bubble system ───────────────────────────────
  const ambientMessages = [
    { charId: 'pam',     text: 'WUPHF!' },
    { charId: 'ceo',     text: 'One command. One office.' },
    { charId: 'eng',     text: '$ go build -o wuphf && ./wuphf' },
    { charId: 'michael', text: "I'm not superstitious, but I am a little stitious." },
    { charId: 'cmo',     text: 'Open source. MIT license.' },
    { charId: 'dwight',  text: 'Bears. Beets. Battlestar Galactica.' },
    { charId: 'ceo',     text: 'Routing task to engineering. ETA: 3 minutes.' },
    { charId: 'pam',     text: 'CEO, PM, engineers — all visible, all working.' },
    { charId: 'eng',     text: 'Implementing feature... 47% complete.' },
    { charId: 'jim',     text: "Unlike Ryan Howard's WUPHF, this one works." },
    { charId: 'cmo',     text: 'Drafting launch post. You will not believe this lede.' },
    { charId: 'kevin',   text: '... (stares at snacks)' },
    { charId: 'creed',   text: 'Nobody steals from Creed Bratton and gets away with it.' },
  ];

  const activeBubbles = [];
  const BUBBLE_DURATION = 5000;
  const BUBBLE_INTERVAL = 2400;
  let lastBubbleTime = 0;
  let msgIndex = 0;
  const charScreenPos = {};

  function updateBubbles(now) {
    for (let i = activeBubbles.length - 1; i >= 0; i--) {
      if (now - activeBubbles[i].startTime > BUBBLE_DURATION) activeBubbles.splice(i, 1);
    }
    if (activeBubbles.length < 2 && now - lastBubbleTime > BUBBLE_INTERVAL) {
      const activeIds = new Set(activeBubbles.map(b => b.charId));
      for (let t = 0; t < ambientMessages.length; t++) {
        const msg = ambientMessages[msgIndex % ambientMessages.length];
        msgIndex++;
        if (!activeIds.has(msg.charId)) {
          activeBubbles.push({ charId: msg.charId, text: msg.text, startTime: now });
          lastBubbleTime = now;
          break;
        }
      }
    }
  }

  function drawBubble(bubble, now) {
    const pos = charScreenPos[bubble.charId];
    if (!pos) return;
    const age = now - bubble.startTime;

    // Discrete pop-in / pop-out (no smooth easing — steps only)
    let alpha = 1;
    if      (age < 100)                        alpha = 0;
    else if (age < 220)                        alpha = 0.6;
    else if (age > BUBBLE_DURATION - 220)      alpha = 0.6;
    else if (age > BUBBLE_DURATION - 100)      alpha = 0;
    if (alpha === 0) return;

    ctx.globalAlpha = alpha;
    const BW = 240, BH = 94;
    let bx = Math.min(pos.centerX - 110, W - BW - 12);
    bx = Math.max(bx, 12);
    const by = Math.max(pos.topY - 108, 5);

    // Panel
    ctx.fillStyle = C.surface;
    ctx.fillRect(bx, by, BW, BH);
    ctx.strokeStyle = C.yellow; ctx.lineWidth = 3;
    ctx.strokeRect(bx, by, BW, BH);
    // Pixel shadow
    ctx.fillStyle = C.yellowDark;
    ctx.fillRect(bx + 4, by + BH, BW, 4);
    ctx.fillRect(bx + BW, by + 4, 4, BH);

    // Tail
    ctx.beginPath();
    ctx.moveTo(bx + 22, by + BH);
    ctx.lineTo(bx + 33, by + BH + 15);
    ctx.lineTo(bx + 46, by + BH);
    ctx.closePath();
    ctx.fillStyle = C.surface; ctx.fill();
    ctx.strokeStyle = C.yellow; ctx.stroke();
    ctx.fillStyle = C.surface; ctx.fillRect(bx + 23, by + BH - 1, 23, 4);

    // Speaker name
    ctx.fillStyle = C.yellow;
    ctx.font = '6px "Press Start 2P"'; ctx.textAlign = 'left'; ctx.textBaseline = 'top';
    const char = CHARS.find(c => c.id === bubble.charId);
    if (char) ctx.fillText(char.name, bx + 10, by + 9);

    // Quote (VT323, word-wrapped)
    ctx.fillStyle = C.text;
    ctx.font = '22px "VT323"'; ctx.textBaseline = 'top';
    const words = bubble.text.split(' ');
    let line = '', lineY = by + 24;
    for (const w of words) {
      const test = line ? line + ' ' + w : w;
      if (ctx.measureText(test).width > BW - 22 && line) {
        ctx.fillText(line, bx + 10, lineY);
        line = w; lineY += 22;
        if (lineY > by + BH - 8) { ctx.fillText(line + '...', bx + 10, lineY - 22); break; }
      } else { line = test; }
    }
    ctx.fillText(line, bx + 10, lineY);
    ctx.globalAlpha = 1;
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
    ctx.fillStyle = color; ctx.fill();
    ctx.strokeStyle = C.carpetLine; ctx.lineWidth = 0.5; ctx.stroke();
  }

  // ── Iso box ────────────────────────────────────────────────────
  function drawIsoBox(gx, gy, w, d, h, top, left, right) {
    const p0 = iso(gx,     gy);
    const pw = iso(gx + w, gy);
    const pd = iso(gx,     gy + d);
    const pf = iso(gx + w, gy + d);
    ctx.beginPath();
    ctx.moveTo(p0.x + TW/2, p0.y - h); ctx.lineTo(pw.x + TW/2, pw.y - h);
    ctx.lineTo(pf.x + TW/2, pf.y - h); ctx.lineTo(pd.x + TW/2, pd.y - h);
    ctx.closePath(); ctx.fillStyle = top; ctx.fill();
    ctx.beginPath();
    ctx.moveTo(p0.x + TW/2, p0.y - h); ctx.lineTo(pd.x + TW/2, pd.y - h);
    ctx.lineTo(pd.x + TW/2, pd.y);    ctx.lineTo(p0.x + TW/2, p0.y);
    ctx.closePath(); ctx.fillStyle = left; ctx.fill();
    ctx.beginPath();
    ctx.moveTo(pw.x + TW/2, pw.y - h); ctx.lineTo(pf.x + TW/2, pf.y - h);
    ctx.lineTo(pf.x + TW/2, pf.y);    ctx.lineTo(pw.x + TW/2, pw.y);
    ctx.closePath(); ctx.fillStyle = right; ctx.fill();
  }

  // ── Back wall ──────────────────────────────────────────────────
  function drawWall() {
    ctx.fillStyle = C.wall;
    ctx.fillRect(0, 0, W, OY + 30);
    ctx.fillStyle = C.wallLight;
    ctx.fillRect(0, OY + 22, W, 6);

    // Fluorescent lights distributed across viewport
    const nLights = Math.max(3, Math.floor(W / 220));
    const spacing = W / nLights;
    for (let i = 0; i < nLights; i++) {
      const lx = spacing * i + (spacing - 140) / 2, ly = 6;
      ctx.fillStyle = '#302820'; ctx.fillRect(lx, ly, 140, 10);
      ctx.fillStyle = 'rgba(255,254,230,0.6)'; ctx.fillRect(lx + 4, ly + 2, 132, 6);
      const grad = ctx.createLinearGradient(lx + 70, ly + 8, lx + 70, ly + 50);
      grad.addColorStop(0, 'rgba(255,254,220,0.14)');
      grad.addColorStop(1, 'rgba(255,254,220,0)');
      ctx.fillStyle = grad; ctx.fillRect(lx, ly + 8, 140, 42);
    }

    // WUPHF sign (centered on viewport)
    const sw = Math.min(360, W * 0.28), sh = 60;
    const sx = Math.round(W / 2 - sw / 2), sy = 22;
    ctx.fillStyle = '#0E0C08'; ctx.fillRect(sx, sy, sw, sh);
    ctx.fillStyle = C.yellow;
    ctx.fillRect(sx, sy, sw, 4); ctx.fillRect(sx, sy + sh - 4, sw, 4);
    ctx.fillRect(sx, sy, 4, sh); ctx.fillRect(sx + sw - 4, sy, 4, sh);
    ctx.fillStyle = 'rgba(236,178,46,0.08)'; ctx.fillRect(sx + 4, sy + 4, sw - 8, sh - 8);
    ctx.shadowColor = C.yellow; ctx.shadowBlur = 18;
    ctx.fillStyle = C.yellow;
    ctx.font = `bold ${Math.round(sw / 6.5)}px "Press Start 2P"`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillText('WUPHF', W / 2, sy + sh / 2);
    ctx.shadowBlur = 0;
    ctx.fillStyle = C.deskDark;
    ctx.fillRect(sx + 50, sy + sh - 2, 10, 16); ctx.fillRect(sx + sw - 60, sy + sh - 2, 10, 16);

    // Tagline below sign
    ctx.fillStyle = C.textMuted;
    ctx.font = '7px "Press Start 2P"';
    ctx.textAlign = 'center'; ctx.textBaseline = 'top';
    ctx.fillText('Your AI team. Visible and working.', W / 2, sy + sh + 8);

    // Wall clock (right side)
    const clkX = W - 90, clkY = 52;
    ctx.fillStyle = C.wallLight;
    ctx.beginPath(); ctx.arc(clkX, clkY, 22, 0, Math.PI * 2); ctx.fill();
    ctx.fillStyle = C.surface;
    ctx.beginPath(); ctx.arc(clkX, clkY, 17, 0, Math.PI * 2); ctx.fill();
    ctx.strokeStyle = C.text; ctx.lineWidth = 2;
    ctx.beginPath(); ctx.moveTo(clkX, clkY - 14); ctx.lineTo(clkX, clkY); ctx.stroke();
    ctx.beginPath(); ctx.moveTo(clkX, clkY); ctx.lineTo(clkX + 11, clkY + 6); ctx.stroke();

    // Beet farm map
    const bx = iso(0, 3).x - 80;
    ctx.fillStyle = '#2A2818'; ctx.fillRect(bx, OY - 24, 62, 44);
    ctx.strokeStyle = '#504830'; ctx.lineWidth = 1.5; ctx.strokeRect(bx, OY - 24, 62, 44);
    ctx.fillStyle = '#807020';
    ctx.font = '7px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText('BEET', bx + 31, OY - 8); ctx.fillText('FARM', bx + 31, OY + 6);
    ctx.fillStyle = '#2A5018';
    ctx.fillRect(bx + 12, OY + 14, 10, 5); ctx.fillRect(bx + 36, OY + 11, 10, 5); ctx.fillRect(bx + 24, OY + 8, 10, 5);

    // Conference room partition
    const cp = iso(0, 0);
    ctx.fillStyle = '#252018'; ctx.fillRect(cp.x - 50, OY - 2, 50, 34);
    ctx.strokeStyle = C.border; ctx.lineWidth = 1; ctx.strokeRect(cp.x - 50, OY - 2, 50, 34);
    ctx.fillStyle = C.textMuted;
    ctx.font = '5px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText('CONF', cp.x - 25, OY + 10); ctx.fillText('ROOM', cp.x - 25, OY + 22);
  }

  // ── Furniture ──────────────────────────────────────────────────
  function drawDesk(gx, gy, w) {
    const DH = 30;
    drawIsoBox(gx, gy, w, 1, DH, C.desk, C.deskDark, C.deskSide);
    const p = iso(gx, gy);
    const mx = p.x + TW * w / 2 + 8;
    const my = p.y - DH - 28;
    ctx.fillStyle = '#1A2030'; ctx.fillRect(mx, my, 36, 24);
    ctx.fillStyle = '#1A3858'; ctx.fillRect(mx + 2, my + 2, 32, 20);
    ctx.fillStyle = C.blue;
    for (let i = 0; i < 3; i++) ctx.fillRect(mx + 4, my + 4 + i * 5, 12 + i * 4, 3);
    ctx.fillStyle = '#1A1820'; ctx.fillRect(mx + 13, my + 24, 10, 7); ctx.fillRect(mx + 9, my + 30, 18, 3);
    const pp = iso(gx, gy);
    ctx.fillStyle = C.surfaceHi; ctx.fillRect(pp.x + 18, pp.y - DH - 2, 22, 16);
    ctx.fillStyle = C.border;
    for (let i = 0; i < 3; i++) ctx.fillRect(pp.x + 20, pp.y - DH + 2 + i * 4, 16, 1.5);
  }

  function drawPlant(gx, gy) {
    const c = isoCenter(gx, gy);
    ctx.fillStyle = '#5A3A18'; ctx.fillRect(c.x - 7, c.y - 18, 14, 14);
    ctx.fillStyle = C.plant;   ctx.fillRect(c.x - 14, c.y - 36, 28, 22);
    ctx.fillStyle = '#2A4818'; ctx.fillRect(c.x - 9,  c.y - 44, 18, 12);
    ctx.fillStyle = C.plant;   ctx.fillRect(c.x - 6,  c.y - 52, 12, 14);
  }

  function drawSnackJar(gx, gy) {
    const c = isoCenter(gx, gy);
    ctx.fillStyle = '#3A5878'; ctx.fillRect(c.x - 8, c.y - 26, 16, 18);
    ctx.fillStyle = C.surface; ctx.fillRect(c.x - 7, c.y - 24, 14, 16);
    ctx.fillStyle = C.yellow;  ctx.fillRect(c.x - 4, c.y - 16, 9, 9);
    ctx.fillStyle = C.deskDark; ctx.fillRect(c.x - 7, c.y - 26, 16, 5);
    ctx.fillStyle = C.text;
    ctx.font = '4px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText('NO', c.x, c.y - 12); ctx.fillText('WASTE', c.x, c.y - 7);
  }

  // ── Sprite helpers ─────────────────────────────────────────────
  // bob: 0 or 2 based on frame; lean: -1, 0, or 1
  function drawHead(x, y, hair, skin, f) {
    const bob  = (f < 2) ? 0 : 2;
    const lean = [0, 1, 0, -1][f];
    ctx.fillStyle = hair; ctx.fillRect(x + 2 + lean, y + bob,      16, 9);
    ctx.fillStyle = skin; ctx.fillRect(x + 2 + lean, y + 7 + bob,  16, 13);
    ctx.fillStyle = C.bg;
    ctx.fillRect(x + 5 + lean, y + 10 + bob, 3, 3);
    ctx.fillRect(x + 12 + lean, y + 10 + bob, 3, 3);
    ctx.fillStyle = '#804040'; ctx.fillRect(x + 7 + lean, y + 17 + bob, 6, 2);
  }

  // Shared leg drawing — feet alternate forward on frames 1 and 3
  function drawLegs(x, y, f, skinColor, shoeColor, legColor, wide) {
    const lOff = (f === 1) ? -3 : 0;
    const rOff = (f === 3) ? -3 : 0;
    ctx.fillStyle = legColor;
    ctx.fillRect(x + 1,           y + 34, wide ? 10 : 8, 12);
    ctx.fillRect(x + (wide?13:11), y + 34, wide ? 10 : 8, 12);
    ctx.fillStyle = skinColor;
    ctx.fillRect(x + 2,           y + 46 + lOff, wide ? 7 : 6, 6);
    ctx.fillRect(x + (wide?14:12), y + 46 + rOff, wide ? 7 : 6, 6);
    ctx.fillStyle = shoeColor;
    ctx.fillRect(x + 2,           y + 52 + lOff, wide ? 9 : 8, 4);
    ctx.fillRect(x + (wide?13:10), y + 52 + rOff, wide ? 9 : 8, 4);
  }

  // ── Office cast ────────────────────────────────────────────────
  function drawPam(x, y, f) {
    const bob = (f < 2) ? 0 : 2;
    ctx.fillStyle = '#C49838'; ctx.fillRect(x + 5, y - 4 + bob, 10, 7);
    drawHead(x, y + bob, '#D4A850', C.skin, f);
    ctx.fillStyle = '#D09098'; ctx.fillRect(x,     y + 20 + bob, 20, 14);
    ctx.fillStyle = C.light;   ctx.fillRect(x + 7, y + 20 + bob, 6, 4);
    ctx.fillStyle = '#6868A8'; ctx.fillRect(x, y + 34 + bob, 20, 12);
    drawLegs(x, y + bob, f, C.skin, '#2A1808', '#6868A8', false);
  }

  function drawMichael(x, y, f) {
    const bob = (f < 2) ? 0 : 2;
    drawHead(x, y + bob, '#2A1A0A', C.skin, f);
    ctx.fillStyle = '#A05050'; ctx.fillRect(x + 4, y + 16 + bob, 12, 3);
    ctx.fillStyle = '#1A3858'; ctx.fillRect(x, y + 20 + bob, 20, 14);
    ctx.fillStyle = C.light;   ctx.fillRect(x + 7, y + 20 + bob, 6, 10);
    ctx.fillStyle = C.yellow;  ctx.fillRect(x + 9, y + 23 + bob, 3, 8);
    drawLegs(x, y + bob, f, C.skin, '#0A0E14', '#0E2840', false);
  }

  function drawDwight(x, y, f) {
    const bob = (f < 2) ? 0 : 2;
    drawHead(x, y + bob, '#3A2010', '#C88858', f);
    ctx.fillStyle = C.bg;
    ctx.fillRect(x + 3, y + 9 + bob, 5, 3); ctx.fillRect(x + 11, y + 9 + bob, 5, 3);
    ctx.fillRect(x + 8, y + 10 + bob, 3, 1);
    ctx.fillStyle = '#604010'; ctx.fillRect(x + 5, y + 16 + bob, 10, 2);
    ctx.fillStyle = '#A88018'; ctx.fillRect(x, y + 20 + bob, 20, 14);
    ctx.fillStyle = C.bg;      ctx.fillRect(x + 7, y + 20 + bob, 6, 4);
    drawLegs(x, y + bob, f, '#C88858', '#080808', '#202020', false);
  }

  function drawJim(x, y, f) {
    const bob = (f < 2) ? 0 : 2;
    drawHead(x, y + bob, '#5A3828', C.skin, f);
    ctx.fillStyle = '#7A4838'; ctx.fillRect(x + 14, y + 2 + bob, 4, 4);
    ctx.fillStyle = '#804848'; ctx.fillRect(x + 9, y + 17 + bob, 7, 2);
    ctx.fillStyle = '#3A5A78'; ctx.fillRect(x, y + 20 + bob, 20, 14);
    drawLegs(x, y + bob, f, C.skin, '#080C10', '#1A2428', false);
  }

  function drawKevin(x, y, f) {
    const bob = (f < 2) ? 0 : 2;
    ctx.fillStyle = '#201810'; ctx.fillRect(x, y + bob, 24, 8);
    ctx.fillStyle = '#D09858'; ctx.fillRect(x, y + 6 + bob, 24, 16);
    ctx.fillStyle = C.bg;
    ctx.fillRect(x + 4, y + 10 + bob, 4, 4); ctx.fillRect(x + 16, y + 10 + bob, 4, 4);
    ctx.fillStyle = '#805038'; ctx.fillRect(x + 8, y + 18 + bob, 8, 2);
    ctx.fillStyle = '#2A3A58'; ctx.fillRect(x, y + 22 + bob, 24, 12);
    drawLegs(x, y + bob, f, '#D09858', '#080808', '#181818', true);
  }

  function drawCreed(x, y, f) {
    const bob = (f < 3) ? 0 : 2;
    drawHead(x, y + bob, '#706860', '#B88858', f);
    ctx.fillStyle = '#805038'; ctx.fillRect(x + 4, y + 17 + bob, 12, 2);
    ctx.fillStyle = '#284818'; ctx.fillRect(x, y + 20 + bob, 20, 14);
    drawLegs(x, y + bob, f, '#B88858', '#100808', '#201808', false);
  }

  function drawAgent(x, y, color, label, f) {
    const bob = (f < 2) ? 0 : 2;
    ctx.fillStyle = color; ctx.fillRect(x + 2, y + bob, 16, 14);
    ctx.fillStyle = C.bg; ctx.fillRect(x + 4, y + 3 + bob, 12, 7);
    const eyeCol = color === C.yellow ? '#AA7800' : (color === C.green ? '#2A6040' : C.blue);
    ctx.fillStyle = eyeCol;
    ctx.fillRect(x + 5, y + 4 + bob, 4, 4); ctx.fillRect(x + 11, y + 4 + bob, 4, 4);
    if (f === 3) {
      ctx.fillStyle = C.bg;
      ctx.fillRect(x + 5, y + 6 + bob, 4, 2); ctx.fillRect(x + 11, y + 6 + bob, 4, 2);
    }
    ctx.fillStyle = color; ctx.fillRect(x, y + 14 + bob, 20, 16);
    ctx.fillStyle = C.bg; ctx.fillRect(x + 2, y + 20 + bob, 16, 8);
    ctx.fillStyle = color;
    ctx.font = '5px "Press Start 2P"'; ctx.textAlign = 'center';
    ctx.fillText(label.substring(0, 3).toUpperCase(), x + 10, y + 27 + bob);
    ctx.fillStyle = '#202020';
    ctx.fillRect(x + 2,  y + 30 + bob, 7, 16); ctx.fillRect(x + 11, y + 30 + bob, 7, 16);
    ctx.fillRect(x,      y + 46, 9, 4);         ctx.fillRect(x + 11, y + 46, 9, 4);
  }

  // ── Characters ─────────────────────────────────────────────────
  const CHARS = [
    { id: 'pam',     name: 'Pam Beesly',     gx: 4.5, gy: 0.5, fn: drawPam },
    { id: 'michael', name: 'Michael Scott',  gx: 8.5, gy: 1.5, fn: drawMichael },
    { id: 'dwight',  name: 'Dwight Schrute', gx: 1.5, gy: 3.5, fn: drawDwight },
    { id: 'jim',     name: 'Jim Halpert',    gx: 4,   gy: 3,   fn: drawJim },
    { id: 'kevin',   name: 'Kevin Malone',   gx: 7,   gy: 5,   fn: drawKevin, wide: true },
    { id: 'creed',   name: 'Creed Bratton',  gx: 0.5, gy: 6.5, fn: drawCreed },
    { id: 'ceo',     name: 'CEO Agent',      gx: 7,   gy: 2,   isAgent: true, color: C.yellow, label: 'CEO' },
    { id: 'eng',     name: 'Engineer Agent', gx: 3,   gy: 4.5, isAgent: true, color: C.blue,   label: 'ENG' },
    { id: 'cmo',     name: 'CMO Agent',      gx: 5.5, gy: 3.5, isAgent: true, color: C.green,  label: 'CMO' },
  ];

  const charHits = [];

  // ── Mobile fallback ────────────────────────────────────────────
  function drawMobileScene() {
    ctx.fillStyle = C.wall; ctx.fillRect(0, 0, W, H);
    ctx.fillStyle = C.carpet; ctx.fillRect(0, H - 70, W, 70);
    ctx.fillStyle = C.carpetLine; ctx.fillRect(0, H - 71, W, 2);
    const sw = Math.min(300, W - 32), sh = 52;
    const sx = (W - sw) / 2, sy = 16;
    ctx.fillStyle = '#0E0C08'; ctx.fillRect(sx, sy, sw, sh);
    ctx.fillStyle = C.yellow;
    ctx.fillRect(sx, sy, sw, 4); ctx.fillRect(sx, sy + sh - 4, sw, 4);
    ctx.fillRect(sx, sy, 4, sh); ctx.fillRect(sx + sw - 4, sy, 4, sh);
    ctx.shadowColor = C.yellow; ctx.shadowBlur = 12;
    ctx.fillStyle = C.yellow;
    ctx.font = `bold ${Math.floor(sw / 6)}px "Press Start 2P"`;
    ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
    ctx.fillText('WUPHF', sx + sw / 2, sy + sh / 2);
    ctx.shadowBlur = 0;
    ctx.fillStyle = C.textMuted;
    ctx.font = '7px "Press Start 2P"'; ctx.textBaseline = 'top';
    ctx.fillText('Your AI team. Visible and working.', W / 2, sy + sh + 8);
    const charY = H - 70;
    [[W * 0.2, drawPam], [W * 0.5, drawMichael], [W * 0.8, drawDwight]].forEach(([cx, fn]) => {
      ctx.fillStyle = C.shadow;
      ctx.beginPath(); ctx.ellipse(cx, charY + 2, 11, 5, 0, 0, Math.PI * 2); ctx.fill();
      fn(cx - 10, charY - 56, animF);
    });
    ctx.fillStyle = C.textMuted;
    ctx.font = '6px "Press Start 2P"'; ctx.textAlign = 'center'; ctx.textBaseline = 'top';
    ctx.fillText('Best viewed on desktop', W / 2, H - 22);
  }

  // ── Main draw ──────────────────────────────────────────────────
  function draw(now) {
    ctx.clearRect(0, 0, W, H);
    if (isMobile) { drawMobileScene(); return; }

    updateBubbles(now);
    drawWall();

    for (let gy = 0; gy < ROWS; gy++) {
      for (let gx = 0; gx < COLS; gx++) {
        drawFloorTile(gx, gy, (gx + gy) % 2 === 0 ? C.carpet : C.carpetAlt);
      }
    }

    // Props
    drawPlant(COLS - 1, 0);
    drawPlant(COLS - 1, 2);
    drawPlant(COLS - 1, 5);
    drawSnackJar(7, 5);

    // Desks
    drawDesk(3, 0, 2);   // reception
    drawDesk(1, 3, 1);   // Dwight
    drawDesk(3, 3, 1);   // Jim
    drawDesk(6, 1, 1);   // CEO Agent
    drawDesk(2, 4, 1);   // Engineer Agent
    drawDesk(5, 3, 1);   // CMO Agent
    drawDesk(9, 3, 1);   // extra desk

    // Characters (back-to-front depth sort)
    charHits.length = 0;
    const sorted = [...CHARS].sort((a, b) => (a.gx + a.gy) - (b.gx + b.gy));
    for (const char of sorted) {
      const c  = isoCenter(char.gx, char.gy);
      const cw = char.wide ? 24 : 20;
      const cx = c.x - cw / 2 - 2;
      const cy = c.y - 56;

      ctx.fillStyle = C.shadow;
      ctx.beginPath();
      ctx.ellipse(c.x, c.y + 2, char.wide ? 14 : 11, 5, 0, 0, Math.PI * 2);
      ctx.fill();

      if (char.isAgent) {
        drawAgent(cx, cy, char.color, char.label, animF);
      } else {
        char.fn(cx, cy, animF);
      }

      if (char.id === 'pam' || char.isAgent) {
        const tagColor = char.isAgent ? char.color : C.yellow;
        const firstName = char.name.split(' ')[0].substring(0, 8);
        const tagW = firstName.length * 6 + 16;
        ctx.fillStyle = tagColor;
        ctx.fillRect(c.x - tagW / 2, cy - 14, tagW, 11);
        ctx.fillStyle = C.bg;
        ctx.font = '5px "Press Start 2P"'; ctx.textAlign = 'center'; ctx.textBaseline = 'middle';
        ctx.fillText(firstName, c.x, cy - 8);
      }

      charScreenPos[char.id] = { centerX: c.x, topY: cy };
      charHits.push({ char, cx, cy, w: cw + 4, h: 56 });
    }

    // Ambient speech bubbles
    for (const bubble of activeBubbles) {
      drawBubble(bubble, now);
    }
  }

  // ── RAF loop ───────────────────────────────────────────────────
  let rafId;
  function loop(now) {
    draw(now);
    rafId = requestAnimationFrame(loop);
  }
  rafId = requestAnimationFrame(loop);

  document.addEventListener('visibilitychange', () => {
    if (document.hidden) cancelAnimationFrame(rafId);
    else rafId = requestAnimationFrame(loop);
  });

  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') activeBubbles.length = 0;
  });

})();
