#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const rootDir = path.resolve(__dirname, '..')

const sourceImage = path.join(rootDir, 'assets/avatar-system/office-sheet.png')
const webOut = path.join(rootDir, 'web/src/lib/avatarSprites.generated.ts')
const videoOut = path.join(rootDir, 'video/src/components/avatarSprites.generated.ts')
const goOut = path.join(rootDir, 'cmd/wuphf/avatars_office_generated.go')

const portraitSize = 16

const avatarRoleSpecs = {
  ceo: { baseSpriteId: 'office05', aliases: ['ceo'], face: 'michael', outfit: 'suit' },
  pm: { baseSpriteId: 'office15', aliases: ['pm'], face: 'warm', outfit: 'manager' },
  fe: { baseSpriteId: 'office23', aliases: ['fe', 'frontend'], face: 'warmEngineer', outfit: 'engineer' },
  be: { baseSpriteId: 'office19', aliases: ['be', 'backend'], face: 'preserve', outfit: 'engineer' },
  eng: { baseSpriteId: 'office20', aliases: ['eng'], face: 'glassesSoft', outfit: 'shirtTie' },
  gtm: { baseSpriteId: 'office14', aliases: ['gtm'], face: 'preserve', outfit: 'sales' },
  ai: { baseSpriteId: 'office16', aliases: ['ai', 'ai-eng', 'ai_eng'], face: 'silver', outfit: 'suit' },
  designer: { baseSpriteId: 'office25', aliases: ['designer'], face: 'lightNoBrow', outfit: 'creative' },
  cmo: { baseSpriteId: 'office18', aliases: ['cmo'], face: 'smileOnly', outfit: 'creative' },
  cro: { baseSpriteId: 'office10', aliases: ['cro'], face: 'mouthOnly', outfit: 'suit' },
  pam: {
    baseSpriteId: 'office23',
    aliases: ['pam'],
    face: 'pamOffice',
    hair: 'softBob',
    outfit: 'pinkCardigan',
    charm: 'officePam',
  },
  pamCute: {
    baseSpriteId: 'office23',
    aliases: ['pam-cute', 'pam-soft', 'pam-legacy'],
    face: 'pamSoft',
    hair: 'softBob',
    outfit: 'pinkCardigan',
    charm: 'legacyPam',
  },
  human: { baseSpriteId: 'office22', aliases: ['human', 'you'], face: 'humanSoft', outfit: 'casual' },
  nex: { baseSpriteId: 'office17', aliases: ['nex'], face: 'preserve', outfit: 'ops' },
  generic: { baseSpriteId: 'office05', aliases: ['generic'], face: 'preserve', outfit: 'suit' },
  qa: { baseSpriteId: 'office03', aliases: ['qa'], face: 'preserve', outfit: 'dwight' },
  ae: { baseSpriteId: 'office10', aliases: ['ae'], face: 'mouthOnly', outfit: 'suit' },
  sdr: { baseSpriteId: 'office14', aliases: ['sdr'], face: 'preserve', outfit: 'sales' },
  research: { baseSpriteId: 'office16', aliases: ['research'], face: 'silver', outfit: 'suit' },
  content: { baseSpriteId: 'office25', aliases: ['content'], face: 'lightNoBrow', outfit: 'creative' },
}

function hybridSpriteIdForRole(role) {
  return `hybrid${role[0].toUpperCase()}${role.slice(1).replace(/[-_](.)/g, (_match, char) => char.toUpperCase())}`
}

const slugToRole = Object.fromEntries(
  Object.entries(avatarRoleSpecs).flatMap(([role, spec]) =>
    [role, ...spec.aliases].map((slug) => [slug.toLowerCase(), role]),
  ),
)

const slugToSpriteId = Object.fromEntries(
  Object.entries(slugToRole).map(([slug, role]) => [slug, hybridSpriteIdForRole(role)]),
)

const videoFallbackSpriteId = hybridSpriteIdForRole('generic')

function applyPamPalette(palette) {
  palette[0] = '#2c1a12'
  palette[1] = '#4a2d1e'
  palette[2] = '#6e432b'
  palette[4] = '#efb78e'
  palette[8] = '#2f2118'
  palette[10] = '#d8879e'
  palette[11] = '#b7657e'
  palette[13] = '#8f5a45'
  palette[14] = '#e4a07d'
  palette[16] = '#2c1a12'

  while (palette.length <= 18) {
    palette.push('#000000')
  }

  palette[17] = '#fff7f5'
  palette[18] = '#8a5639'
}

function run(command, args, options = {}) {
  return execFileSync(command, args, {
    cwd: rootDir,
    maxBuffer: 128 * 1024 * 1024,
    ...options,
  })
}

function probeImage(imagePath) {
  const output = run('ffprobe', [
    '-v',
    'error',
    '-select_streams',
    'v:0',
    '-show_entries',
    'stream=width,height',
    '-of',
    'csv=p=0:s=x',
    imagePath,
  ]).toString().trim()
  const [width, height] = output.split('x').map((value) => Number.parseInt(value, 10))
  if (!width || !height) {
    throw new Error(`Could not read image dimensions for ${imagePath}`)
  }
  return { width, height }
}

function loadImage(imagePath) {
  const { width, height } = probeImage(imagePath)
  const rgba = run('ffmpeg', [
    '-v',
    'error',
    '-i',
    imagePath,
    '-f',
    'rawvideo',
    '-pix_fmt',
    'rgba',
    '-',
  ])

  return { width, height, rgba }
}

function pixelIndex(width, x, y) {
  return (y * width + x) * 4
}

function alphaAt(image, x, y) {
  return image.rgba[pixelIndex(image.width, x, y) + 3]
}

function hexAt(image, x, y) {
  const idx = pixelIndex(image.width, x, y)
  const alpha = image.rgba[idx + 3]
  if (alpha === 0) return null
  const red = image.rgba[idx]
  const green = image.rgba[idx + 1]
  const blue = image.rgba[idx + 2]
  return `#${[red, green, blue].map((value) => value.toString(16).padStart(2, '0')).join('')}`
}

function findComponents(image) {
  const seen = new Uint8Array(image.width * image.height)
  const components = []
  const directions = [
    [1, 0],
    [-1, 0],
    [0, 1],
    [0, -1],
  ]

  for (let y = 0; y < image.height; y++) {
    for (let x = 0; x < image.width; x++) {
      const pointer = y * image.width + x
      if (seen[pointer] || alphaAt(image, x, y) === 0) continue

      let minX = x
      let maxX = x
      let minY = y
      let maxY = y
      let count = 0

      const queue = [[x, y]]
      seen[pointer] = 1

      while (queue.length > 0) {
        const [currentX, currentY] = queue.pop()
        count++

        if (currentX < minX) minX = currentX
        if (currentX > maxX) maxX = currentX
        if (currentY < minY) minY = currentY
        if (currentY > maxY) maxY = currentY

        for (const [dx, dy] of directions) {
          const nextX = currentX + dx
          const nextY = currentY + dy
          if (nextX < 0 || nextY < 0 || nextX >= image.width || nextY >= image.height) continue

          const nextPointer = nextY * image.width + nextX
          if (seen[nextPointer] || alphaAt(image, nextX, nextY) === 0) continue

          seen[nextPointer] = 1
          queue.push([nextX, nextY])
        }
      }

      if (count < 50) continue

      components.push({
        minX,
        maxX,
        minY,
        maxY,
        width: maxX - minX + 1,
        height: maxY - minY + 1,
        count,
      })
    }
  }

  components.sort((left, right) => left.minY - right.minY || left.minX - right.minX)
  return components
}

function gcd(a, b) {
  let left = Math.abs(a)
  let right = Math.abs(b)
  while (right !== 0) {
    const remainder = left % right
    left = right
    right = remainder
  }
  return left
}

function detectScale(components) {
  const values = []
  for (const component of components) {
    values.push(component.width, component.height)
  }

  const scale = values.reduce((acc, value) => gcd(acc, value))
  if (scale <= 0) {
    throw new Error('Could not infer avatar scale factor from sprite sheet')
  }
  return scale
}

function sampleCell(image, component, scale, cellX, cellY) {
  const counts = new Map()
  for (let offsetY = 0; offsetY < scale; offsetY++) {
    for (let offsetX = 0; offsetX < scale; offsetX++) {
      const sourceX = component.minX + cellX * scale + offsetX
      const sourceY = component.minY + cellY * scale + offsetY
      const key = hexAt(image, sourceX, sourceY) ?? 'transparent'
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
  }

  const sorted = [...counts.entries()].sort((left, right) => right[1] - left[1])
  const winner = sorted[0]?.[0] ?? 'transparent'
  return winner === 'transparent' ? null : winner
}

function downsampleSprite(image, component, scale) {
  const width = Math.round(component.width / scale)
  const height = Math.round(component.height / scale)
  const grid = []

  for (let y = 0; y < height; y++) {
    const row = []
    for (let x = 0; x < width; x++) {
      row.push(sampleCell(image, component, scale, x, y))
    }
    grid.push(row)
  }

  return grid
}

function makePortrait(fullGrid, size) {
  const fullHeight = fullGrid.length
  const fullWidth = fullGrid[0]?.length ?? 0
  const cropWidth = Math.min(size, fullWidth)
  const cropHeight = Math.min(size, fullHeight)
  const sourceX = Math.max(0, Math.floor((fullWidth - size) / 2))
  const destX = Math.max(0, Math.floor((size - cropWidth) / 2))
  const portrait = Array.from({ length: size }, () => Array.from({ length: size }, () => null))

  for (let y = 0; y < cropHeight; y++) {
    for (let x = 0; x < cropWidth; x++) {
      portrait[y][destX + x] = fullGrid[y][sourceX + x]
    }
  }

  return portrait
}

function cloneGrid(grid) {
  return grid.map((row) => [...row])
}

function padGrid(grid, width, height) {
  const sourceHeight = grid.length
  const sourceWidth = grid[0]?.length ?? 0
  const offsetX = Math.max(0, Math.floor((width - sourceWidth) / 2))
  const offsetY = Math.max(0, Math.floor((height - sourceHeight) / 2))
  const padded = Array.from({ length: height }, () => Array.from({ length: width }, () => 0))

  for (let y = 0; y < sourceHeight; y++) {
    for (let x = 0; x < sourceWidth; x++) {
      padded[offsetY + y][offsetX + x] = grid[y][x]
    }
  }

  return padded
}

function makeCuteOfficePortrait(fullGrid, size) {
  const fullHeight = fullGrid.length
  const fullWidth = fullGrid[0]?.length ?? 0
  if (fullHeight === 0 || fullWidth === 0) {
    return Array.from({ length: size }, () => Array.from({ length: size }, () => 0))
  }

  const targetHeight = size
  const targetWidth = Math.min(size, Math.max(10, Math.round((fullWidth * targetHeight) / fullHeight)))
  const offsetX = Math.floor((size - targetWidth) / 2)
  const portrait = Array.from({ length: size }, () => Array.from({ length: size }, () => 0))

  for (let y = 0; y < targetHeight; y++) {
    const sourceY = Math.min(fullHeight - 1, Math.floor((y / targetHeight) * fullHeight))
    for (let x = 0; x < targetWidth; x++) {
      const sourceX = Math.min(fullWidth - 1, Math.floor((x / targetWidth) * fullWidth))
      portrait[y][offsetX + x] = fullGrid[sourceY][sourceX]
    }
  }

  return portrait
}

function buildPalette(...grids) {
  const colors = []
  const seen = new Set()
  for (const grid of grids) {
    for (const row of grid) {
      for (const color of row) {
        if (!color || seen.has(color)) continue
        seen.add(color)
        colors.push(color)
      }
    }
  }
  return colors
}

function indexGrid(grid, palette) {
  const paletteMap = new Map(palette.map((color, index) => [color, index + 1]))
  return grid.map((row) => row.map((color) => (color ? (paletteMap.get(color) ?? 0) : 0)))
}

function rgbTuple(hex) {
  return [
    Number.parseInt(hex.slice(1, 3), 16),
    Number.parseInt(hex.slice(3, 5), 16),
    Number.parseInt(hex.slice(5, 7), 16),
  ]
}

function paintCells(grid, cells, value) {
  for (const [x, y] of cells) {
    if (grid[y]?.[x] !== undefined) grid[y][x] = value
  }
}

function fillCells(grid, cells, value) {
  for (const [x, y] of cells) {
    if (grid[y]?.[x] > 0) grid[y][x] = value
  }
}

function rectCells(x1, y1, x2, y2) {
  const cells = []
  for (let y = y1; y <= y2; y++) {
    for (let x = x1; x <= x2; x++) {
      cells.push([x, y])
    }
  }
  return cells
}

function blankPortrait() {
  return Array.from({ length: portraitSize }, () => Array.from({ length: portraitSize }, () => 0))
}

function buildPamCutePortrait(_officePortrait, spec) {
  const portrait = blankPortrait()

  if (spec.hair === 'softBob') {
    paintCells(portrait, rectCells(4, 1, 11, 1), 2)
    paintCells(portrait, rectCells(3, 2, 12, 2), 3)
    paintCells(portrait, rectCells(2, 3, 13, 3), 3)
    paintCells(portrait, rectCells(2, 4, 5, 4), 3)
    paintCells(portrait, rectCells(10, 4, 13, 4), 3)
    paintCells(portrait, rectCells(3, 5, 4, 6), 2)
    paintCells(portrait, rectCells(11, 5, 12, 6), 2)
    paintCells(portrait, rectCells(5, 2, 9, 2), 19)
  }

  paintCells(portrait, rectCells(5, 4, 11, 4), 1)
  paintCells(portrait, rectCells(5, 5, 11, 8), 5)
  paintCells(portrait, [
    [7, 6],
    [10, 6],
  ], 9)
  paintCells(portrait, [
    [6, 7],
    [11, 7],
  ], 15)
  paintCells(portrait, [
    [8, 8],
    [9, 8],
  ], 1)

  if (spec.outfit === 'pinkCardigan') {
    paintCells(portrait, [
      [5, 9],
      [6, 9],
      [9, 9],
      [10, 9],
      [4, 10],
      [5, 10],
      [6, 10],
      [9, 10],
      [10, 10],
      [11, 10],
      [4, 11],
      [5, 11],
      [6, 11],
      [7, 11],
      [8, 11],
      [9, 11],
      [10, 11],
      [11, 11],
      [5, 12],
      [6, 12],
      [7, 12],
      [8, 12],
      [9, 12],
      [10, 12],
    ], 11)
    paintCells(portrait, [
      [4, 10],
      [11, 10],
    ], 12)
    paintCells(portrait, [
      [7, 9],
      [8, 9],
      [7, 10],
      [8, 10],
    ], 18)
    paintCells(portrait, [
      [4, 10],
      [11, 10],
    ], 5)
  }

  paintCells(portrait, [
    [5, 13],
    [6, 13],
    [10, 13],
    [11, 13],
  ], 1)
  paintCells(portrait, [
    [6, 14],
    [7, 14],
    [10, 14],
    [11, 14],
  ], 17)

  return portrait
}

function buildOfficePamPortrait(officePortrait, spec) {
  const portrait = cloneGrid(officePortrait)

  if (spec.hair === 'softBob') {
    fillCells(portrait, rectCells(4, 1, 12, 3), 3)
    paintCells(portrait, rectCells(3, 3, 4, 6), 3)
    paintCells(portrait, rectCells(11, 3, 12, 6), 3)
    paintCells(portrait, rectCells(5, 2, 9, 2), 19)
    paintCells(portrait, [
      [4, 4],
      [11, 4],
    ], 2)
  }

  fillCells(portrait, rectCells(5, 5, 12, 9), 5)
  paintCells(portrait, [
    [7, 6],
    [11, 6],
  ], 9)
  paintCells(portrait, [
    [8, 8],
    [9, 8],
    [10, 8],
  ], 18)

  if (spec.outfit === 'pinkCardigan') {
    fillCells(portrait, rectCells(4, 10, 11, 13), 11)
    paintCells(portrait, [
      [4, 10],
      [11, 10],
      [4, 11],
      [11, 11],
    ], 12)
    paintCells(portrait, [
      [7, 10],
      [8, 10],
      [7, 11],
      [8, 11],
    ], 18)
    paintCells(portrait, [
      [4, 12],
      [11, 12],
    ], 5)
  }

  return portrait
}

function applyStandardFacePatch(portrait, config) {
  const {
    skin,
    skinLight = skin,
    dark,
    brow = dark,
    mouth = dark,
    nose = skinLight,
    face = rectCells(6, 5, 12, 8),
  } = config

  fillCells(portrait, face, skin)
  paintCells(portrait, [
    [7, 5],
    [8, 5],
    [10, 5],
    [11, 5],
  ], brow)
  paintCells(portrait, [
    [7, 6],
    [11, 6],
  ], dark)
  paintCells(portrait, [[9, 7]], nose)
  paintCells(portrait, [
    [8, 8],
    [9, 8],
    [10, 8],
  ], mouth)
}

function applyFaceProfile(face, portrait) {
  const clean = cloneGrid(portrait)

  switch (face) {
    case 'michael':
      applyStandardFacePatch(clean, { skin: 8, skinLight: 7, dark: 9, brow: 8, mouth: 6, face: rectCells(6, 5, 12, 9) })
      break
    case 'warm':
      applyStandardFacePatch(clean, { skin: 5, skinLight: 7, dark: 18, brow: 3, mouth: 11, face: rectCells(6, 5, 12, 9) })
      break
    case 'warmEngineer':
      applyStandardFacePatch(clean, { skin: 5, skinLight: 7, dark: 9, brow: 3, mouth: 10, face: rectCells(6, 5, 12, 9) })
      break
    case 'glassesSoft':
      fillCells(clean, rectCells(4, 4, 12, 4), 5)
      fillCells(clean, rectCells(6, 5, 12, 9), 5)
      paintCells(clean, [
        [6, 6],
        [8, 6],
        [10, 6],
        [12, 6],
        [6, 7],
        [8, 7],
        [10, 7],
        [12, 7],
      ], 24)
      paintCells(clean, [[7, 6], [11, 6]], 9)
      paintCells(clean, [[9, 8]], 15)
      break
    case 'silver':
      applyStandardFacePatch(clean, { skin: 5, skinLight: 4, dark: 6, brow: 1, mouth: 7, face: rectCells(6, 4, 12, 8) })
      break
    case 'lightNoBrow':
      applyStandardFacePatch(clean, { skin: 6, skinLight: 10, dark: 18, brow: 6, mouth: 8, face: rectCells(6, 5, 12, 9) })
      break
    case 'humanSoft':
      applyStandardFacePatch(clean, { skin: 5, skinLight: 8, dark: 18, brow: 5, mouth: 9, face: rectCells(6, 5, 12, 9) })
      break
    case 'smileOnly':
      fillCells(clean, rectCells(6, 5, 12, 6), 5)
      paintCells(clean, [[7, 5], [11, 5]], 7)
      break
    case 'mouthOnly':
      paintCells(clean, [[8, 8], [9, 8], [10, 8]], 3)
      break
    case 'preserve':
    default:
      break
  }

  return clean
}

function addManualSprites(spriteCatalog) {
  const spritesById = new Map(spriteCatalog.map((sprite) => [sprite.id, sprite]))
  const hybrids = Object.entries(avatarRoleSpecs).map(([role, spec]) => {
    const base = spritesById.get(spec.baseSpriteId)
    if (!base) {
      throw new Error(`Missing ${spec.baseSpriteId} base sprite for ${role} hybrid override`)
    }

    const palette = [...base.palette]
    let portrait = makeCuteOfficePortrait(base.full, portraitSize)

    if (spec.outfit === 'pinkCardigan') {
      applyPamPalette(palette)
    }

    if (spec.charm === 'legacyPam') {
      portrait = buildPamCutePortrait(portrait, spec)
    } else if (spec.charm === 'officePam') {
      portrait = buildOfficePamPortrait(portrait, spec)
    } else {
      portrait = applyFaceProfile(spec.face, portrait)
    }

    return {
      id: hybridSpriteIdForRole(role),
      sourceIndex: base.sourceIndex,
      palette,
      full: cloneGrid(base.full),
      portrait,
    }
  })

  return [...spriteCatalog, ...hybrids]
}

function json(data) {
  return JSON.stringify(data, null, 2)
}

function goGrid(name, grid) {
  const rows = grid.map((row) => `\t\t{${row.join(', ')}},`).join('\n')
  return `${name}: pixelSprite{\n${rows}\n\t\t},`
}

function goPalette(palette) {
  const lines = palette
    .map((hex, index) => {
      const [red, green, blue] = rgbTuple(hex)
      return `\t\t\t${index + 1}: {${red}, ${green}, ${blue}},`
    })
    .join('\n')
  return `map[int][3]int{\n${lines}\n\t\t}`
}

function writeWeb(spriteCatalog) {
  const webSprites = {}
  for (const sprite of spriteCatalog) {
    webSprites[sprite.id] = {
      id: sprite.id,
      palette: sprite.palette,
      portrait: sprite.portrait,
      sourceIndex: sprite.sourceIndex,
    }
  }

  const body = `// Code generated by scripts/generate-avatar-sprites.mjs from assets/avatar-system/office-sheet.png. DO NOT EDIT.\n\nexport type AvatarGrid = number[][]\n\nexport interface KnownAvatarSprite {\n  id: string\n  palette: string[]\n  portrait: AvatarGrid\n  sourceIndex: number\n}\n\nexport const KNOWN_AVATAR_SPRITES: Record<string, KnownAvatarSprite> = ${json(webSprites)}\n\nexport const KNOWN_AVATAR_SLUG_MAP: Record<string, string> = ${json(slugToSpriteId)}\n\nexport function resolveKnownPortraitSprite(slug: string): KnownAvatarSprite | null {\n  const key = KNOWN_AVATAR_SLUG_MAP[slug.toLowerCase()]\n  return key ? KNOWN_AVATAR_SPRITES[key] ?? null : null\n}\n`
  fs.writeFileSync(webOut, body)
}

function writeVideo(spriteCatalog) {
  if (!fs.existsSync(path.dirname(videoOut))) {
    return false
  }

  const videoSprites = {}
  for (const sprite of spriteCatalog) {
    videoSprites[sprite.id] = {
      id: sprite.id,
      palette: sprite.palette,
      portrait: sprite.portrait,
      sourceIndex: sprite.sourceIndex,
    }
  }

  const body = `// Code generated by scripts/generate-avatar-sprites.mjs from assets/avatar-system/office-sheet.png. DO NOT EDIT.\n\nexport type Sprite = number[][]\n\nexport interface SpriteData {\n  id: string\n  palette: string[]\n  portrait: Sprite\n  sourceIndex: number\n}\n\nexport const SPRITES: Record<string, SpriteData> = ${json(videoSprites)}\n\nexport const SPRITE_SLUG_MAP: Record<string, string> = ${json(slugToSpriteId)}\n\nexport function resolveSpriteData(slug: string): SpriteData {\n  const key = SPRITE_SLUG_MAP[slug.toLowerCase()] ?? '${videoFallbackSpriteId}'\n  return SPRITES[key]\n}\n\nexport function resolveSprite(slug: string): Sprite {\n  return resolveSpriteData(slug).portrait\n}\n`
  fs.writeFileSync(videoOut, body)
  return true
}

function writeGo(spriteCatalog) {
  const records = spriteCatalog
    .map((sprite) => {
      return `\t"${sprite.id}": {\n${goGrid('Full', sprite.full)}\n${goGrid('Portrait', sprite.portrait)}\n\t\tPalette: ${goPalette(sprite.palette)},\n\t},`
    })
    .join('\n')

  const slugMap = Object.entries(slugToSpriteId)
    .map(([slug, spriteId]) => `\t"${slug}": "${spriteId}",`)
    .join('\n')

  const body = `// Code generated by scripts/generate-avatar-sprites.mjs from assets/avatar-system/office-sheet.png. DO NOT EDIT.\n\npackage main\n\nimport "strings"\n\ntype knownOfficeSprite struct {\n\tFull     pixelSprite\n\tPortrait pixelSprite\n\tPalette  map[int][3]int\n}\n\nvar knownOfficeSprites = map[string]knownOfficeSprite{\n${records}\n}\n\nvar knownOfficeSlugMap = map[string]string{\n${slugMap}\n}\n\nfunc knownOfficeSpriteForSlug(slug string) (knownOfficeSprite, bool) {\n\tkey, ok := knownOfficeSlugMap[strings.ToLower(slug)]\n\tif !ok {\n\t\treturn knownOfficeSprite{}, false\n\t}\n\tsprite, ok := knownOfficeSprites[key]\n\treturn sprite, ok\n}\n`
  fs.writeFileSync(goOut, body)
}

function main() {
  if (!fs.existsSync(sourceImage)) {
    throw new Error(`Missing source avatar sheet: ${sourceImage}`)
  }

  const image = loadImage(sourceImage)
  const components = findComponents(image)
  if (components.length !== 27) {
    throw new Error(`Expected 27 sprite components in the sheet, found ${components.length}`)
  }

  const scale = detectScale(components)
  const spriteCatalog = components.map((component, index) => {
    const fullColorGrid = downsampleSprite(image, component, scale)
    const portraitColorGrid = makePortrait(fullColorGrid, portraitSize)
    const palette = buildPalette(fullColorGrid, portraitColorGrid)

    return {
      id: `office${String(index + 1).padStart(2, '0')}`,
      sourceIndex: index + 1,
      palette,
      full: indexGrid(fullColorGrid, palette),
      portrait: indexGrid(portraitColorGrid, palette),
    }
  })

  const spriteCatalogWithOverrides = addManualSprites(spriteCatalog)

  writeWeb(spriteCatalogWithOverrides)
  const wroteVideo = writeVideo(spriteCatalogWithOverrides)
  writeGo(spriteCatalogWithOverrides)

  console.log(`Generated ${spriteCatalogWithOverrides.length} office-sheet avatar sprites (scale=${scale})`)
  console.log(webOut)
  if (wroteVideo) {
    console.log(videoOut)
  }
  console.log(goOut)
}

main()
