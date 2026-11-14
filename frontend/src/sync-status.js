// Keep editable credentials intact when a background sync finishes.
export function applySyncMetadata(settings, latest) {
 for (const key of ['lastSyncAt', 'lastSyncSuccessAt', 'lastSyncError']) settings[key] = latest[key]
}
export function currentSyncError(job, settings) {
 if (job.running) return ''
 return (job.finishedAt ? job.error : settings.lastSyncError) || ''
}
