# Avatar Design System

The avatar system is generated from committed seed pixel grids plus role specs
in `scripts/generate-avatar-sprites.mjs`.

## Source Of Truth

- `avatarRoleSpecs` defines built-in roles, aliases, base sprite IDs,
  face profile, outfit, and charm notes.
- `base-sprites.json` stores indexed seed grids and palettes for the generated
  catalog.
- Slugs are normalized to lowercase during generation so aliases resolve
  consistently across web, video, and terminal renderers.
- `makeCuteOfficePortrait` creates the shared 16x16 portrait scale from a
  full-body seed sprite.
- `applyFaceProfile` normalizes eyes, brows, nose, and mouth so generated
  portraits read consistently at small sizes.
- `buildOfficePamPortrait` is the canonical Pam path: the shared office
  portrait system with Pam-from-The-Office hair, small face, and cardigan cues.
- `buildPamCutePortrait` keeps the current cute Pam variant available as
  `pam-cute`, `pam-soft`, and `pam-legacy`.

## Adding A New Avatar

1. Add a new entry to `avatarRoleSpecs`.
2. Pick a `baseSpriteId` from the seed catalog.
3. Add any aliases the app should resolve.
4. Choose an existing `face` profile, or add a new profile in
   `applyFaceProfile` only if the current profiles cannot express the role.
5. Run `node scripts/generate-avatar-sprites.mjs`.

Generated outputs are intentionally committed for web and terminal renderers so
all surfaces use the same avatar catalog.
