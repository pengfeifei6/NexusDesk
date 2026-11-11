/** Move within the page without triggering route-scope resets or losing drafts. */
export function focusSection(id: string) {
  const section = document.getElementById(id)
  if (!section) return
  section.focus({ preventScroll: true })
  section.scrollIntoView?.({ block: 'start', behavior: 'auto' })
}
