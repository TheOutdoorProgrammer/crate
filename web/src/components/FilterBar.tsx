import { useState, useEffect, useRef } from 'react';

export default function FilterBar({
  value,
  onChange,
  placeholder = 'Filter...',
  debounceMs = 0,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  debounceMs?: number;
}) {
  const [local, setLocal] = useState(value);
  const timerRef = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    setLocal(value);
  }, [value]);

  const handleChange = (v: string) => {
    setLocal(v);
    if (debounceMs > 0) {
      clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => onChange(v), debounceMs);
    } else {
      onChange(v);
    }
  };

  useEffect(() => () => clearTimeout(timerRef.current), []);

  return (
    <div className="relative mb-3">
      <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-zinc-500 pointer-events-none" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" />
      </svg>
      <input
        type="text"
        value={local}
        onChange={(e) => handleChange(e.target.value)}
        placeholder={placeholder}
        className="w-full bg-zinc-800/60 rounded-lg pl-8 pr-8 py-2 text-sm placeholder-zinc-600 outline-none focus:ring-2 focus:ring-zinc-600"
      />
      {local && (
        <button
          onClick={() => handleChange('')}
          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-zinc-500 active:text-zinc-300"
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M18 6 6 18" /><path d="m6 6 12 12" />
          </svg>
        </button>
      )}
    </div>
  );
}
