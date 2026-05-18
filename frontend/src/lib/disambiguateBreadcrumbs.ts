// Q14 chip rule — a breadcrumb label held by rows from two distinct spaces
// gets the space-name chip on those rows so the user can disambiguate without
// hovering. Pure helper; matches the inline byte-for-byte logic that lived in
// the wikilink picker and the backlinks panel before extraction.

export interface DisambiguatedRow<T> {
  item: T
  breadcrumbLabel: string
  showSpaceChip: boolean
}

const labelOf = (breadcrumb: string[]) =>
  breadcrumb.length === 0 ? '(space root)' : breadcrumb.join(' › ')

export function disambiguateBreadcrumbs<
  T extends { space_id: number; breadcrumb: string[] },
>(items: T[]): DisambiguatedRow<T>[] {
  const spacesByLabel = new Map<string, Set<number>>()
  for (const item of items) {
    const label = labelOf(item.breadcrumb)
    let set = spacesByLabel.get(label)
    if (!set) {
      set = new Set()
      spacesByLabel.set(label, set)
    }
    set.add(item.space_id)
  }
  return items.map((item) => {
    const label = labelOf(item.breadcrumb)
    const spaces = spacesByLabel.get(label)
    return {
      item,
      breadcrumbLabel: label,
      showSpaceChip: !!spaces && spaces.size > 1,
    }
  })
}
