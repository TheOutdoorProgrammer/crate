export default function ProviderBadge({ provider }: { provider: string }) {
  return (
    <span className="inline-flex items-center gap-1 text-[10px] font-medium text-zinc-500 bg-zinc-800 px-1.5 py-0.5 rounded">
      <svg className="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
      </svg>
      {provider}
    </span>
  );
}
