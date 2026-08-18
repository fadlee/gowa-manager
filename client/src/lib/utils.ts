import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

const USERNAME_CHARS = "abcdefghijklmnopqrstuvwxyz0123456789"
const PASSWORD_CHARS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*"

function randomString(length: number, charset: string): string {
  const array = new Uint32Array(length)
  crypto.getRandomValues(array)
  return Array.from(array, (x) => charset[x % charset.length]).join("")
}

export function generateUsername(length: number = 8): string {
  return "user_" + randomString(length, USERNAME_CHARS)
}

export function generatePassword(length: number = 16): string {
  return randomString(length, PASSWORD_CHARS)
}

const NAME_ADJECTIVES = ["fast", "swift", "bright", "cool", "smart", "quick", "active", "dynamic"]
const NAME_NOUNS = ["app", "service", "worker", "server", "instance", "process", "handler", "engine"]

export function generateInstanceName(): string {
  const arr = new Uint32Array(3)
  crypto.getRandomValues(arr)
  const adjective = NAME_ADJECTIVES[arr[0] % NAME_ADJECTIVES.length]
  const noun = NAME_NOUNS[arr[1] % NAME_NOUNS.length]
  const number = arr[2] % 1000
  return `${adjective}-${noun}-${number}`
}