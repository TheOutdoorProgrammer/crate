import { useState, useRef, useEffect, useCallback } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { api } from '../api/client';
import { formatFans } from '../lib/format';
import type { ArtistSearchResult, ProviderInfo } from '../types/index';

const PAGE_SIZE = 25;

export default function Search() {
  const [query, setQuery] = useState('');
  const [submitted, setSubmitted] = useState('');
  const [provider, setProvider] = useState<string>('');
  const [showProviderMenu, setShowProviderMenu] = useState(false);
  const [extraArtists, setExtraArtists] = useState<ArtistSearchResult[]>([]);
  const [offset, setOffset] = useState(PAGE_SIZE);
  const [loadingMore, setLoadingMore] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: api.listProviders,
  });

  const { data: settings } = useQuery({
    queryKey: ['settings'],
    queryFn: api.getSettings,
  });

  const defaultProvider = settings?.provider_primary || 'musicbrainz';
  const activeProvider = provider || defaultProvider;

  const activeProviderInfo = providers?.find((p: ProviderInfo) => p.name === activeProvider);
  const providerLabel = activeProviderInfo?.display_name || activeProvider;

  const { data: searchData, isLoading } = useQuery({
    queryKey: ['search', submitted, activeProvider],
    queryFn: () => api.search(submitted, activeProvider, PAGE_SIZE, 0),
    enabled: !!submitted,
  });

  const artists = [...(searchData?.artists || []), ...extraArtists];
  const total = searchData?.total ?? 0;

  const loadMore = useCallback(async () => {
    if (loadingMore || offset >= total) return;
    setLoadingMore(true);
    try {
      const data = await api.search(submitted, activeProvider, PAGE_SIZE, offset);
      setExtraArtists((prev) => [...prev, ...(data.artists || [])]);
      setOffset((prev) => prev + PAGE_SIZE);
    } finally {
      setLoadingMore(false);
    }
  }, [submitted, activeProvider, offset, total, loadingMore]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setExtraArtists([]);
    setOffset(PAGE_SIZE);
    setSubmitted(query.trim());
  };

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setShowProviderMenu(false);
      }
    }
    if (showProviderMenu) document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showProviderMenu]);

  return (
    <div>
      <form onSubmit={handleSubmit} className="mb-4">
        <div className="flex gap-2">
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search for an artist..."
            className="flex-1 bg-zinc-800 rounded-lg px-4 py-2.5 text-sm placeholder-zinc-500 outline-none focus:ring-2 focus:ring-zinc-600"
            autoFocus
          />
          <div className="relative" ref={menuRef}>
            <div className="flex">
              <button
                type="submit"
                disabled={!query.trim()}
                className="px-4 py-2.5 bg-white text-zinc-900 rounded-l-lg font-medium text-sm active:scale-[0.97] transition-transform disabled:opacity-40"
              >
                Search
              </button>
              <button
                type="button"
                onClick={() => setShowProviderMenu(!showProviderMenu)}
                className="px-1.5 py-2.5 bg-white text-zinc-900 rounded-r-lg border-l border-zinc-200 active:bg-zinc-100 transition-colors"
              >
                <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <path d="m6 9 6 6 6-6" />
                </svg>
              </button>
            </div>

            {showProviderMenu && providers && (
              <div className="absolute right-0 top-full mt-1 bg-zinc-800 rounded-lg border border-zinc-700 shadow-xl z-50 min-w-[160px] py-1 animate-fade-in">
                {providers.filter((p: ProviderInfo) => p.healthy).map((p: ProviderInfo) => (
                  <button
                    key={p.name}
                    type="button"
                    onClick={() => {
                      setProvider(p.name === defaultProvider ? '' : p.name);
                      setShowProviderMenu(false);
                      if (submitted) setSubmitted(query.trim() || submitted);
                    }}
                    className={`w-full text-left px-3 py-2 text-sm transition-colors ${
                      activeProvider === p.name
                        ? 'text-white bg-zinc-700'
                        : 'text-zinc-300 active:bg-zinc-700'
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span>{p.display_name}</span>
                      {activeProvider === p.name && (
                        <svg className="w-3.5 h-3.5 text-green-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                          <polyline points="20 6 9 17 4 12" />
                        </svg>
                      )}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
        {provider && (
          <p className="text-[11px] text-zinc-500 mt-1.5">
            Searching via <span className="text-zinc-400">{providerLabel}</span>
          </p>
        )}
      </form>

      {isLoading && (
        <div className="space-y-1">
          {[...Array(5)].map((_, i) => (
            <div key={i} className="flex items-center gap-3 bg-zinc-800/40 rounded-lg p-2.5 animate-pulse">
              <div className="w-11 h-11 rounded-full bg-zinc-700" />
              <div className="flex-1 space-y-2">
                <div className="h-3.5 bg-zinc-700 rounded w-32" />
                <div className="h-2.5 bg-zinc-800 rounded w-20" />
              </div>
            </div>
          ))}
        </div>
      )}

      {artists.length > 0 && (
        <div className="space-y-1">
          {artists.map((artist, i) => (
            <Link
              key={`${artist.id}-${i}`}
              to={`/browse/artist/${artist.id}${provider ? `?provider=${provider}` : ''}`}
              className="flex items-center gap-3 bg-zinc-800/40 rounded-lg p-2.5 active:bg-zinc-800 transition-colors"
            >
              <div className="w-11 h-11 rounded-full bg-zinc-700 overflow-hidden shrink-0">
                {artist.image_url ? (
                  <img src={artist.image_url} alt="" className="w-full h-full object-cover" />
                ) : (
                  <div className="w-full h-full flex items-center justify-center text-zinc-500 text-sm">
                    <svg className="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M9 18V5l12-2v13" /><circle cx="6" cy="18" r="3" /><circle cx="18" cy="16" r="3" /></svg>
                  </div>
                )}
              </div>
              <div className="flex-1 min-w-0">
                <p className="font-medium text-sm truncate">{artist.name}</p>
                <p className="text-xs text-zinc-500">
                  {artist.album_count > 0 && `${artist.album_count} albums`}
                  {artist.metadata?.fans && ` · ${formatFans(Number(artist.metadata.fans))} fans`}
                  {artist.metadata?.country && ` · ${artist.metadata.country}`}
                </p>
              </div>
              <svg className="w-4 h-4 text-zinc-600 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m9 18 6-6-6-6" /></svg>
            </Link>
          ))}
          {offset < total && (
            <button
              onClick={loadMore}
              disabled={loadingMore}
              className="w-full py-2.5 text-sm text-zinc-400 bg-zinc-800/40 rounded-lg active:bg-zinc-800 transition-colors disabled:opacity-50 mt-2"
            >
              {loadingMore ? 'Loading...' : `Load More (${artists.length} of ${total})`}
            </button>
          )}
        </div>
      )}

      {submitted && !isLoading && artists.length === 0 && (
        <div className="text-center py-12 text-zinc-500 text-sm">No artists found</div>
      )}

      {!submitted && !isLoading && (
        <div className="text-center py-16">
          <svg className="w-12 h-12 mx-auto text-zinc-700 mb-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" />
          </svg>
          <p className="text-zinc-500">Find artists to add to your crate</p>
          <p className="text-zinc-600 text-sm mt-1">Search by name above</p>
        </div>
      )}
    </div>
  );
}
