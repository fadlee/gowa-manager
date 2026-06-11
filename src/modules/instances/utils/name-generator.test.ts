import { afterEach, describe, expect, spyOn, test } from 'bun:test'
import { NameGenerator } from './name-generator'

describe('NameGenerator', () => {
  afterEach(() => {
    Math.random = originalRandom
  })

  const originalRandom = Math.random

  test('generates names in adjective-noun-number format', () => {
    const name = NameGenerator.generateRandomName()

    expect(name).toMatch(/^(fast|swift|bright|cool|smart|quick|active|dynamic)-(app|service|worker|server|instance|process|handler|engine)-\d{1,3}$/)
  })

  test('uses random values to select adjective, noun, and numeric suffix', () => {
    const random = spyOn(Math, 'random')
      .mockReturnValueOnce(0)
      .mockReturnValueOnce(0.999)
      .mockReturnValueOnce(0.123)

    const name = NameGenerator.generateRandomName()

    expect(name).toBe('fast-engine-123')
    expect(random).toHaveBeenCalledTimes(3)
    random.mockRestore()
  })

  test('supports upper bound random values without exceeding list indices', () => {
    const random = spyOn(Math, 'random')
      .mockReturnValueOnce(0.999)
      .mockReturnValueOnce(0.999)
      .mockReturnValueOnce(0.999)

    expect(NameGenerator.generateRandomName()).toBe('dynamic-engine-999')
    random.mockRestore()
  })
})
