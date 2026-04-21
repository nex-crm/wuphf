import '@testing-library/jest-dom/vitest'

// Node 25+ exposes a built-in `globalThis.localStorage` with no real Storage
// API (empty object prototype). happy-dom's window.localStorage then gets
// shadowed, breaking `setItem`/`getItem`/`clear`. Install a tiny in-memory
// Storage polyfill for tests so draft-autosave logic can be exercised
// deterministically.
function createMemoryStorage(): Storage {
  const data: Record<string, string> = {}
  const storage: Storage = {
    get length() {
      return Object.keys(data).length
    },
    clear: () => {
      for (const k of Object.keys(data)) delete data[k]
    },
    getItem: (key: string) =>
      Object.prototype.hasOwnProperty.call(data, key) ? data[key] : null,
    key: (index: number) => Object.keys(data)[index] ?? null,
    removeItem: (key: string) => {
      delete data[key]
    },
    setItem: (key: string, value: string) => {
      data[key] = String(value)
    },
  }
  return storage
}

const memoryStorage = createMemoryStorage()
// Override on window AND globalThis so both lookup paths see the same store.
Object.defineProperty(window, 'localStorage', {
  configurable: true,
  value: memoryStorage,
})
Object.defineProperty(globalThis, 'localStorage', {
  configurable: true,
  value: memoryStorage,
})
