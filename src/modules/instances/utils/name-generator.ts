export class NameGenerator {
  // Generate random instance name
  static generateRandomName(): string {
    const adjectives = ['fast', 'swift', 'bright', 'cool', 'smart', 'quick', 'active', 'dynamic']
    const nouns = ['app', 'service', 'worker', 'server', 'instance', 'process', 'handler', 'engine']
    const adjective = adjectives[Math.floor(Math.random() * adjectives.length)]
    const noun = nouns[Math.floor(Math.random() * nouns.length)]
    const randomNum = Math.floor(Math.random() * 1000)
    return `${adjective}-${noun}-${randomNum}`
  }
}
