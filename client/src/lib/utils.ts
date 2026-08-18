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