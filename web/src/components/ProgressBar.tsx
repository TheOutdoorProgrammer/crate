export default function ProgressBar({ owned, total }: { owned: number; total: number }) {
  if (total === 0) return null;
  const pct = Math.round((owned / total) * 100);

  return (
    <div className="flex items-center gap-2.5">
      <div className="flex-1 h-1.5 bg-zinc-800 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-300 ${
            pct === 100 ? 'bg-green-500' : 'bg-blue-500'
          }`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span className="text-[11px] text-zinc-500 tabular-nums shrink-0">
        {owned}/{total}
      </span>
    </div>
  );
}
