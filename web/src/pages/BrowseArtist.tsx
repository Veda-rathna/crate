import { useState, useMemo } from 'react';
import { useParams, Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import FilterBar from '../components/FilterBar';
import MetadataPills from '../components/MetadataPills';
import LanguageSelector from '../components/LanguageSelector';
import { IconCheck, IconDisc, IconLibrary } from '../components/Icons';

export default function BrowseArtist() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const [watchNewReleases, setWatchNewReleases] = useState(false);
  const [languages, setLanguages] = useState<string[]>([]);
  const [localWatchedAlbums, setLocalWatchedAlbums] = useState<Set<string>>(new Set());
  const [trackFilter, setTrackFilter] = useState('');
  const [debouncedFilter, setDebouncedFilter] = useState('');

  const providerOverride = searchParams.get('provider') || undefined;

  const { data: artist, isLoading } = useQuery({
    queryKey: ['browse-artist', id, providerOverride],
    queryFn: () => api.browseArtist(id!, providerOverride),
    enabled: !!id,
  });

  const watchArtist = useMutation({
    mutationFn: () =>
      api.watchArtist(id!, {
        watch_new_releases: watchNewReleases,
        languages,
        provider: providerOverride,
      }),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['status'] });
      toast(`Added ${created.name} — syncing discography in background`, 'success');
      navigate(`/artist/${created.id}`);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const watchAlbum = useMutation({
    mutationFn: (albumID: string) =>
      api.watchAlbum(albumID, {
        artist_provider_id: artist!.id,
        artist_name: artist!.name,
        artist_image_url: artist!.image_url,
        languages,
        provider: providerOverride,
      }),
    onSuccess: (_data, albumID) => {
      setLocalWatchedAlbums((prev) => new Set(prev).add(albumID));
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast('Album added to library', 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const { data: trackSearchResults } = useQuery({
    queryKey: ['browse-artist-tracks', id, debouncedFilter, providerOverride],
    queryFn: () => api.searchBrowseArtistTracks(id!, debouncedFilter, providerOverride),
    enabled: !!id && debouncedFilter.length >= 2,
  });

  const matchingAlbumIds = useMemo(() => {
    if (!trackSearchResults?.tracks || !debouncedFilter) return null;
    const ids = new Set<string>();
    for (const t of trackSearchResults.tracks) ids.add(t.album_id);
    return ids;
  }, [trackSearchResults, debouncedFilter]);

  const filteredAlbums = useMemo(() => {
    if (!artist?.albums) return [];
    if (!debouncedFilter) return artist.albums;
    const q = debouncedFilter.toLowerCase();
    return artist.albums.filter((album) => {
      if (album.title.toLowerCase().includes(q)) return true;
      return matchingAlbumIds?.has(album.id) ?? false;
    });
  }, [artist, debouncedFilter, matchingAlbumIds]);

  if (isLoading) {
    return (
      <div className="space-y-4 animate-pulse">
        <div className="flex items-center gap-4">
          <div className="w-16 h-16 md:w-20 md:h-20 rounded-2xl bg-zinc-800 shrink-0" />
          <div className="flex-1 space-y-2">
            <div className="h-6 bg-zinc-800 rounded w-44" />
            <div className="h-3.5 bg-zinc-800/70 rounded w-28" />
          </div>
        </div>
        <div className="h-28 bg-zinc-900/70 rounded-2xl" />
        <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="h-16 bg-zinc-900/60 rounded-xl" />
          ))}
        </div>
      </div>
    );
  }

  if (!artist) {
    return (
      <div className="rounded-2xl bg-zinc-900/50 border border-white/[0.06] py-12 text-center text-zinc-400 text-sm">
        Artist not found
      </div>
    );
  }

  return (
    <div className="space-y-5">
      {/* Artist Hero */}
      <div className="rounded-2xl bg-zinc-900/70 border border-white/[0.07] p-4 md:p-5">
        <div className="flex items-center gap-4">
          <div className="w-16 h-16 md:w-20 md:h-20 rounded-2xl bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.08]">
            {artist.image_url ? (
              <img src={artist.image_url} alt={artist.name} className="w-full h-full object-cover" />
            ) : (
              <div className="w-full h-full flex items-center justify-center text-zinc-600">
                <IconLibrary className="w-7 h-7" />
              </div>
            )}
          </div>
          <div className="flex-1 min-w-0">
            <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100 truncate">
              {artist.name}
            </h2>
            <p className="text-xs text-zinc-400 mt-0.5">
              {artist.albums?.length || 0} releases available
            </p>
            <div className="mt-2">
              <MetadataPills metadata={artist.metadata} />
            </div>
          </div>
        </div>
      </div>

      {/* Watch Configuration & Primary CTA */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-3 items-start">
        <div className="lg:col-span-7">
          <LanguageSelector selected={languages} onChange={setLanguages} />
        </div>

        <div className="lg:col-span-5 space-y-2.5">
          <label className="flex items-center justify-between gap-3 bg-zinc-900/70 border border-white/[0.06] rounded-xl px-3.5 py-3 cursor-pointer">
            <div>
              <p className="text-sm font-medium text-zinc-100">Watch for new releases</p>
              <p className="text-[11px] text-zinc-400 mt-0.5">
                Auto-add future albums detected by scheduler
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={watchNewReleases}
              onClick={() => setWatchNewReleases(!watchNewReleases)}
              className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                watchNewReleases ? 'bg-emerald-500' : 'bg-zinc-700'
              }`}
            >
              <span
                className={`inline-block h-5 w-5 rounded-full bg-white shadow transform transition-transform mt-0.5 ${
                  watchNewReleases ? 'translate-x-[22px]' : 'translate-x-0.5'
                }`}
              />
            </button>
          </label>

          <button
            type="button"
            onClick={() => watchArtist.mutate()}
            disabled={watchArtist.isPending}
            className={`w-full py-3 rounded-xl font-semibold text-sm transition-all disabled:opacity-50 ${
              artist.artist_watched || watchArtist.isSuccess
                ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30'
                : 'bg-amber-400 hover:bg-amber-300 text-zinc-950 active:scale-[0.98]'
            }`}
          >
            {watchArtist.isPending
              ? 'Adding & Starting Sync…'
              : artist.artist_watched || watchArtist.isSuccess
                ? '✓ Watching Full Discography'
                : `Watch All Albums (${artist.albums?.length || 0})`}
          </button>
        </div>
      </div>

      {/* Discography */}
      {artist.albums && artist.albums.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
              Discography ({filteredAlbums.length})
            </p>
          </div>

          <FilterBar
            value={trackFilter}
            onChange={(v) => {
              setTrackFilter(v);
              setDebouncedFilter(v);
            }}
            debounceMs={400}
            placeholder="Filter by song or album title..."
            className="mb-0"
          />

          {debouncedFilter && filteredAlbums.length === 0 && (
            <p className="text-zinc-500 text-sm text-center py-6">
              No matching albums or tracks
            </p>
          )}

          <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
            {filteredAlbums.map((album) => {
              const serverWatched = artist.watched_album_ids?.includes(album.id);
              const isWatched = serverWatched || localWatchedAlbums.has(album.id);
              const isAdding = watchAlbum.isPending && watchAlbum.variables === album.id;

              return (
                <div
                  key={album.id}
                  className="flex items-center gap-3 bg-zinc-900/65 hover:bg-zinc-900 border border-white/[0.06] rounded-xl p-2.5 transition-colors"
                >
                  <Link
                    to={`/browse/album/${album.id}?artist_id=${artist.id}&artist_name=${encodeURIComponent(
                      artist.name
                    )}&artist_image_url=${encodeURIComponent(artist.image_url || '')}${
                      providerOverride ? `&provider=${providerOverride}` : ''
                    }`}
                    className="flex items-center gap-3 flex-1 min-w-0"
                  >
                    <div className="w-12 h-12 rounded-lg bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.06]">
                      {album.cover_url ? (
                        <img
                          src={album.cover_url}
                          alt=""
                          className="w-full h-full object-cover"
                          onError={(e) => ((e.target as HTMLImageElement).style.display = 'none')}
                        />
                      ) : (
                        <div className="w-full h-full flex items-center justify-center text-zinc-600">
                          <IconDisc className="w-5 h-5" />
                        </div>
                      )}
                    </div>
                    <div className="flex-1 min-w-0">
                      <p className="font-medium text-sm text-zinc-100 truncate">{album.title}</p>
                      <div className="flex items-center gap-1.5 mt-0.5">
                        {album.year > 0 && (
                          <span className="text-xs text-zinc-400 tabular-nums">{album.year}</span>
                        )}
                        {album.record_type && album.record_type !== 'album' && (
                          <span className="text-[10px] font-medium uppercase text-zinc-400 bg-zinc-800 border border-white/[0.05] px-1.5 py-px rounded">
                            {album.record_type}
                          </span>
                        )}
                      </div>
                    </div>
                  </Link>

                  {isWatched ? (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg bg-emerald-500/15 text-emerald-300 border border-emerald-500/25 text-xs font-medium shrink-0">
                      <IconCheck className="w-3.5 h-3.5" />
                      Watched
                    </span>
                  ) : (
                    <button
                      type="button"
                      disabled={isAdding}
                      onClick={(e) => {
                        e.stopPropagation();
                        watchAlbum.mutate(album.id);
                      }}
                      className="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors shrink-0 bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-zinc-200 disabled:opacity-50"
                    >
                      {isAdding ? 'Adding…' : 'Watch'}
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
