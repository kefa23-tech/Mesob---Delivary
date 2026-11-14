export function formatETB(minorUnits: number, locale = 'en-ET'): string {
  return new Intl.NumberFormat(locale, { style: 'currency', currency: 'ETB', currencyDisplay: 'symbol' }).format(minorUnits / 100);
}
