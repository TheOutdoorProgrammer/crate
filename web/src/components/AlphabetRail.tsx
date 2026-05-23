import { useCallback, useRef, useState } from 'react';

const LETTERS = '#ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('');

interface AlphabetRailProps {
  activeLetters: Set<string>;
}

export default function AlphabetRail({ activeLetters }: AlphabetRailProps) {
  const railRef = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);
  const [hoveredLetter, setHoveredLetter] = useState<string | null>(null);

  const scrollToLetter = useCallback((letter: string) => {
    const el = document.getElementById(`section-${letter}`);
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  }, []);

  const getLetterFromY = useCallback((clientY: number) => {
    if (!railRef.current) return null;
    const rect = railRef.current.getBoundingClientRect();
    const y = clientY - rect.top;
    const idx = Math.floor((y / rect.height) * LETTERS.length);
    return LETTERS[Math.max(0, Math.min(idx, LETTERS.length - 1))];
  }, []);

  const handlePointerDown = useCallback((e: React.PointerEvent) => {
    setDragging(true);
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
    const letter = getLetterFromY(e.clientY);
    if (letter && activeLetters.has(letter)) {
      setHoveredLetter(letter);
      scrollToLetter(letter);
    }
  }, [activeLetters, getLetterFromY, scrollToLetter]);

  const handlePointerMove = useCallback((e: React.PointerEvent) => {
    if (!dragging) return;
    const letter = getLetterFromY(e.clientY);
    if (letter && activeLetters.has(letter) && letter !== hoveredLetter) {
      setHoveredLetter(letter);
      scrollToLetter(letter);
    }
  }, [dragging, activeLetters, getLetterFromY, hoveredLetter, scrollToLetter]);

  const handlePointerUp = useCallback(() => {
    setDragging(false);
    setHoveredLetter(null);
  }, []);

  if (activeLetters.size <= 1) return null;

  return (
    <>
      <div
        ref={railRef}
        className="fixed right-1 top-1/2 -translate-y-1/2 z-40 flex flex-col items-center select-none touch-none md:right-[max(0.25rem,calc(50vw-39rem))]"
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerUp}
      >
        {LETTERS.map((letter) => {
          const isActive = activeLetters.has(letter);
          const isHovered = hoveredLetter === letter;
          return (
            <button
              key={letter}
              onClick={() => isActive && scrollToLetter(letter)}
              className={`w-5 h-[14px] flex items-center justify-center text-[9px] font-medium leading-none rounded-sm transition-colors ${
                isHovered
                  ? 'text-white bg-zinc-700'
                  : isActive
                    ? 'text-zinc-300'
                    : 'text-zinc-700/50'
              }`}
              disabled={!isActive}
              tabIndex={-1}
            >
              {letter}
            </button>
          );
        })}
      </div>

      {dragging && hoveredLetter && (
        <div className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 bg-zinc-800 rounded-xl w-16 h-16 flex items-center justify-center pointer-events-none">
          <span className="text-3xl font-bold text-white">{hoveredLetter}</span>
        </div>
      )}
    </>
  );
}
