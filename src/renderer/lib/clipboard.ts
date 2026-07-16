import { muesli } from '@/api'

export async function writeClipboardText(text: string): Promise<void> {
  if (typeof muesli.writeClipboardText !== 'function') {
    throw new Error('Clipboard is unavailable')
  }
  await muesli.writeClipboardText(text)
}
