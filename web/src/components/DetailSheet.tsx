import { useEffect } from 'react';

interface DetailSheetProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
}

export default function DetailSheet({ open, onClose, title, children }: DetailSheetProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[90] flex items-end justify-center"
      onClick={onClose}
    >
      <div className="absolute inset-0 bg-black/60" />
      <div
        className="relative w-full max-w-lg bg-zinc-900 rounded-t-2xl animate-slide-up max-h-[70vh] flex flex-col pb-[env(safe-area-inset-bottom,0px)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 pt-4 pb-3 border-b border-zinc-800">
          <h3 className="text-sm font-semibold truncate pr-4">{title}</h3>
          <button
            onClick={onClose}
            className="text-zinc-500 active:text-white transition-colors shrink-0"
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>
        <div className="overflow-y-auto px-4 py-3">
          {children}
        </div>
      </div>
    </div>
  );
}

export function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between items-start gap-3 py-1.5">
      <span className="text-[11px] text-zinc-500 uppercase tracking-wider shrink-0">{label}</span>
      <span className="text-sm text-right min-w-0 break-words">{children}</span>
    </div>
  );
}
