import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { generateInstanceKey } from './db'

const originalRandom = Math.random

describe('generateInstanceKey', () => {
  afterEach(() => {
    Math.random = originalRandom
  })

  test('generates an 8-character uppercase alphanumeric key', () => {
    const key = generateInstanceKey()

    expect(key).toMatch(/^[A-Z0-9]{8}$/)
  })

  test('uses random values for each key character', () => {
    const random = spyOn(Math, 'random')
      .mockReturnValueOnce(0)
      .mockReturnValueOnce(1 / 36)
      .mockReturnValueOnce(25 / 36)
      .mockReturnValueOnce(26 / 36)
      .mockReturnValueOnce(27 / 36)
      .mockReturnValueOnce(35 / 36)
      .mockReturnValueOnce(0.999)
      .mockReturnValueOnce(10 / 36)

    const key = generateInstanceKey()

    expect(key).toBe('ABZ0199K')
    expect(random).toHaveBeenCalledTimes(8)
    random.mockRestore()
  })

  test('supports upper bound random values without exceeding character set', () => {
    const random = spyOn(Math, 'random').mockReturnValue(0.999)

    expect(generateInstanceKey()).toBe('99999999')
    expect(random).toHaveBeenCalledTimes(8)
    random.mockRestore()
  })
})
