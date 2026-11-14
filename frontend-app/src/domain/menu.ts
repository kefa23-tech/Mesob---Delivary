import type { components } from '@/infrastructure/api/generated';

export type MenuItem = components['schemas']['MenuItem'];
export type OptionGroup = components['schemas']['OptionGroup'];

export function selectedOptionCount(group: OptionGroup, selected: string[]) {
  return selected.filter((id) => group.options.some((option) => option.id === id)).length;
}

export function validateOptionGroups(
  groups: OptionGroup[] = [],
  selections: Record<string, string[]>,
): Record<string, string> {
  const errors: Record<string, string> = {};
  for (const group of groups) {
    const selected = selections[group.id] ?? [];
    if (selected.length < group.min_select) errors[group.id] = `Select at least ${group.min_select}`;
    if (selected.length > group.max_select) errors[group.id] = `Select no more than ${group.max_select}`;
    if (new Set(selected).size !== selected.length) errors[group.id] = 'Options can only be selected once';
  }
  return errors;
}

export function customizationTotal(item: MenuItem, selections: Record<string, string[]>) {
  return item.price_minor + (item.option_groups ?? []).reduce(
    (total, group) => total + group.options
      .filter((option) => (selections[group.id] ?? []).includes(option.id))
      .reduce((sum, option) => sum + option.price_delta, 0),
    0,
  );
}
