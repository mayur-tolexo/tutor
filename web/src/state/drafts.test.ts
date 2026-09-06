import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clearDraft, createDraftSaver, loadDraft, loadStdin, markOpened, saveDraft, saveStdin, wasOpened } from './drafts'
import { DEVICE_ID_KEY, getDeviceId, resetDeviceIdCache } from './deviceId'

describe('drafts', () => {
  beforeEach(() => {
    localStorage.clear()
    resetDeviceIdCache()
  })

  it('round-trips a draft per exercise and clears it', () => {
    expect(loadDraft('cbse-11/a')).toBeNull()
    saveDraft('cbse-11/a', 'print(1)')
    saveDraft('cbse-11/b', 'print(2)')
    expect(loadDraft('cbse-11/a')).toBe('print(1)')
    expect(loadDraft('cbse-11/b')).toBe('print(2)')
    clearDraft('cbse-11/a')
    expect(loadDraft('cbse-11/a')).toBeNull()
    expect(loadDraft('cbse-11/b')).toBe('print(2)')
  })

  it('remembers stdin and the opened flag', () => {
    expect(loadStdin('x')).toBe('')
    saveStdin('x', '3\n4\n')
    expect(loadStdin('x')).toBe('3\n4\n')
    expect(wasOpened('x')).toBe(false)
    markOpened('x')
    expect(wasOpened('x')).toBe(true)
  })

  it('debounces saves and keeps only the latest code', () => {
    vi.useFakeTimers()
    const saver = createDraftSaver(300)
    saver.schedule('x', 'a')
    saver.schedule('x', 'ab')
    vi.advanceTimersByTime(200)
    expect(loadDraft('x')).toBeNull()
    vi.advanceTimersByTime(150)
    expect(loadDraft('x')).toBe('ab')
    saver.schedule('x', 'abc')
    saver.flush()
    expect(loadDraft('x')).toBe('abc')
    vi.useRealTimers()
  })

  it('survives a throwing localStorage', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota')
    })
    expect(() => saveDraft('x', 'code')).not.toThrow()
    spy.mockRestore()
  })
})

describe('device id', () => {
  beforeEach(() => {
    localStorage.clear()
    resetDeviceIdCache()
  })

  it('generates a v4 uuid once and reuses it', () => {
    const id = getDeviceId()
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(localStorage.getItem(DEVICE_ID_KEY)).toBe(id)
    resetDeviceIdCache()
    expect(getDeviceId()).toBe(id)
  })
})
