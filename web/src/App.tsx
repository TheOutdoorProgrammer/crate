import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import ErrorBoundary from './components/ErrorBoundary';
import Layout from './components/Layout';
import { ToastProvider } from './components/Toast';
import Library from './pages/Library';
import Search from './pages/Search';
import BrowseArtist from './pages/BrowseArtist';
import BrowseAlbum from './pages/BrowseAlbum';
import ArtistDetail from './pages/ArtistDetail';
import AlbumDetail from './pages/AlbumDetail';
import Downloads from './pages/Downloads';
import Settings from './pages/Settings';
import BlockedSources from './pages/BlockedSources';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});

export default function App() {
  return (
    <ErrorBoundary>
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<Library />} />
            <Route path="/search" element={<Search />} />
            <Route path="/browse/artist/:id" element={<BrowseArtist />} />
            <Route path="/browse/album/:id" element={<BrowseAlbum />} />
            <Route path="/artist/:id" element={<ArtistDetail />} />
            <Route path="/album/:id" element={<AlbumDetail />} />
            <Route path="/downloads" element={<Downloads />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="/settings/blocked" element={<BlockedSources />} />
          </Route>
        </Routes>
      </BrowserRouter>
      </ToastProvider>
    </QueryClientProvider>
    </ErrorBoundary>
  );
}
