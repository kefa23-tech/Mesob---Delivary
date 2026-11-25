'use client';

import { FormEvent, useState } from 'react';
import { useRouter } from 'next/navigation';
import { apiClient } from '@/infrastructure/api/client';

export default function LoginPage() {
  const router=useRouter();const [email,setEmail]=useState('');const [password,setPassword]=useState('');const [error,setError]=useState('');const [loading,setLoading]=useState(false);
  async function submit(event:FormEvent<HTMLFormElement>){
    event.preventDefault();setError('');setLoading(true);
    try{
      const result=await apiClient.POST('/auth/login',{body:{email,password}});
      const status=result.response.status;
      const errorCode=result.error&&typeof result.error==='object'&&'code' in result.error?result.error.code:undefined;
      if(status>=500)throw new Error('Authentication service is temporarily unavailable. Please try again shortly.');
      if(status===429)throw new Error('Too many sign-in attempts. Wait a minute and try again.');
      if(status===403)throw new Error('This console is restricted to administrator accounts.');
      if(status===401||errorCode==='invalid_credentials'||errorCode==='unauthorized')throw new Error('Email or password was not accepted.');
      if(result.error||!result.data)throw new Error('Could not complete sign-in. Please try again.');
      if(result.data.user.role!=='admin')throw new Error('This console is restricted to administrator accounts.');
      localStorage.setItem('mesob_access_token',result.data.access_token);localStorage.setItem('mesob_refresh_token',result.data.refresh_token);localStorage.setItem('mesob_user',JSON.stringify(result.data.user));
      document.cookie=`mesob_role=admin; path=/; max-age=2592000; SameSite=Lax`;router.replace('/en/admin');
    }catch(reason){
      setError(reason instanceof TypeError?'Authentication service is temporarily unavailable. Please try again shortly.':reason instanceof Error?reason.message:'Authentication service is temporarily unavailable. Please try again shortly.');
    }finally{setLoading(false);}
  }
  return <main className="grid min-h-screen place-items-center bg-[#102d27] p-5"><div className="w-full max-w-md rounded-3xl bg-white p-8 shadow-2xl sm:p-10"><div className="grid h-12 w-12 place-items-center rounded-2xl bg-emerald-100 font-bold text-emerald-900">M</div><p className="mt-8 text-xs font-semibold uppercase tracking-[.2em] text-emerald-800">Mesob operations</p><h1 className="mt-2 text-3xl font-bold tracking-tight text-slate-900">Administrator sign in</h1><p className="mt-2 text-sm text-slate-500">Use your platform administrator account to continue.</p><form onSubmit={submit} className="mt-8 space-y-5"><label className="block text-sm font-medium text-slate-700">Email<input type="email" autoComplete="username" required value={email} onChange={(event)=>setEmail(event.target.value)} className="field" placeholder="admin@company.com"/></label><label className="block text-sm font-medium text-slate-700">Password<input type="password" autoComplete="current-password" required value={password} onChange={(event)=>setPassword(event.target.value)} className="field"/></label>{error&&<p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}<button disabled={loading} className="w-full rounded-xl bg-emerald-800 px-4 py-3 font-semibold text-white hover:bg-emerald-900 disabled:opacity-50">{loading?'Signing in…':'Sign in securely'}</button></form><p className="mt-6 text-xs leading-5 text-slate-400">Access is verified against your current platform role each time you open an operations endpoint.</p></div></main>;
}
