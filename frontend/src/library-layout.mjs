// Keep every non-final library page aligned to complete rows at the current width.
export function pageSizeForColumns(columns) {
  const count = Number.isFinite(Number(columns)) ? Math.max(1, Math.floor(Number(columns))) : 1;
  return Math.min(100, count * 4);
}
