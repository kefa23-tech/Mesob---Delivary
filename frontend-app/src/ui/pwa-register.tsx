'use client';

import { useEffect } from 'react';

export function PWARegister() {
  useEffect(() => {
    if ('serviceWorker' in navigator && (window.location.protocol === 'https:' || window.location.hostname === 'localhost')) {
      void navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => undefined);
    }
  }, []);
  return null;
}
