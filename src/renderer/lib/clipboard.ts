export async function writeClipboardText(text: string): Promise<void> {
  if (!navigator.clipboard?.writeText) {
    throw new Error('Clipboard is unavailable')
  }
  await navigator.clipboard.writeText(text)
}
