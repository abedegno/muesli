import { uiText } from '@/i18n'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'

interface Props {
  existingNoteTitle: string
  onOpenExisting: () => void
  onTranscribeAgain: () => void
}

export function DuplicateAudioDialog({
  existingNoteTitle,
  onOpenExisting,
  onTranscribeAgain,
}: Props) {
  return (
    <Dialog open onOpenChange={() => {}} title={uiText('Already imported?')}>
      <p className="text-sm text-muted-foreground">
        {uiText('This looks like an existing recording — open it, or transcribe again?')}
      </p>
      {existingNoteTitle && (
        <p className="mt-1 text-sm font-medium truncate">{existingNoteTitle}</p>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="secondary" size="sm" onClick={onTranscribeAgain}>
          {uiText('Transcribe again')}
        </Button>
        <Button variant="primary" size="sm" onClick={onOpenExisting}>
          {uiText('Open existing recording')}
        </Button>
      </div>
    </Dialog>
  )
}
