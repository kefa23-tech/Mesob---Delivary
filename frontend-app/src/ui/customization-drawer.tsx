'use client';

import { useMemo, useState } from 'react';
import { formatETB } from '@/domain/money';
import { customizationTotal, validateOptionGroups } from '@/domain/menu';
import type { MenuItem } from '@/domain/menu';
import { Button } from './button';

export function CustomizationDrawer({ item, onClose }: { item: MenuItem; onClose: () => void }) {
  const [selections, setSelections] = useState<Record<string, string[]>>({});
  const groups = item.option_groups ?? [];
  const errors = useMemo(() => validateOptionGroups(groups, selections), [groups, selections]);
  const total = customizationTotal(item, selections);
  function toggle(groupId: string, optionId: string, max: number) { setSelections((current) => { const selected = current[groupId] ?? []; return { ...current, [groupId]: selected.includes(optionId) ? selected.filter((id) => id !== optionId) : selected.length < max ? [...selected, optionId] : selected }; }); }
  return <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-950/40 p-0 sm:items-center sm:p-6" role="dialog" aria-modal="true" aria-label={`Customize ${item.name}`}><div className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-t-[2rem] bg-white p-6 shadow-2xl sm:rounded-[2rem]"><div className="flex items-start justify-between"><div><p className="text-sm font-semibold text-emerald-600">Customize your order</p><h2 className="mt-1 text-2xl font-black">{item.name}</h2></div><button onClick={onClose} aria-label="Close" className="rounded-full bg-slate-100 px-3 py-2 text-slate-600">×</button></div><div className="mt-7 space-y-6">{groups.map((group) => <fieldset key={group.id}><legend className="flex w-full justify-between text-sm font-bold"><span>{group.name}</span><span className="font-normal text-slate-400">{group.min_select > 0 ? `Required · choose ${group.min_select}-${group.max_select}` : `Choose up to ${group.max_select}`}</span></legend><div className="mt-3 space-y-2">{group.options.filter((option) => option.is_available).map((option) => { const active = (selections[group.id] ?? []).includes(option.id); return <label key={option.id} className={`flex cursor-pointer items-center justify-between rounded-2xl border p-4 transition ${active ? 'border-emerald-500 bg-emerald-50' : 'border-slate-200 hover:border-emerald-300'}`}><span className="flex items-center gap-3"><input type={group.max_select === 1 ? 'radio' : 'checkbox'} name={group.id} checked={active} onChange={() => toggle(group.id, option.id, group.max_select)} className="accent-emerald-600" />{option.name}</span><span className="text-sm font-semibold text-slate-500">{option.price_delta ? `+${formatETB(option.price_delta)}` : 'Included'}</span></label>; })}</div>{errors[group.id] && <p className="mt-2 text-sm font-medium text-red-600">{errors[group.id]}</p>}</fieldset>)}</div><div className="mt-8 flex items-center justify-between border-t border-slate-100 pt-5"><div><p className="text-xs text-slate-400">Total</p><p className="text-2xl font-black">{formatETB(total)}</p></div><Button disabled={Object.keys(errors).length > 0} onClick={onClose}>Add to cart</Button></div></div></div>;
}
