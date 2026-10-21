export function validateDisplayName(value: string): string {
  const name = value.trim()
  if (!name || [...name].length > 80) return 'Use a display name between 1 and 80 characters.'
  if (/[\p{Cc}\p{Cs}]/u.test(name)) return 'Display name cannot contain control characters.'
  return ''
}

export function ownerInitials(name: string): string {
  const words = name.trim().split(/\s+/u)
  return (words.length > 1 ? `${[...words[0]][0]}${[...words.at(-1)!][0]}` : [...(words[0] || 'Owner')].slice(0, 2).join('')).toLocaleUpperCase()
}
