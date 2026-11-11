import { useState } from 'react';
import { useParams, useSearchParams, useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import { useToast } from '../components/Toast';
import MetadataPills from '../components/MetadataPills';
import { formatDuration, formatTotalDuration } from '../lib/format';
import { IconCheck, IconDisc } from '../components/Icons';
import type { BrowseTrackResult } from '../types/index';

export default function BrowseAlbum() {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const [localWatchedTracks, setLocalWatchedTracks] = useState<Set<string>>(new Set());

  const artistID = searchParams.get('artist_id') || '';
  const artistName = searchParams.get('artist_name') || '';
  const artistImageURL = searchParams.get('artist_image_url') || '';
  const providerOverride = searchParams.get('provider') || undefined;

  const { data: album, isLoading } = useQuery({
    queryKey: ['browse-album', id, providerOverride],
    queryFn: () => api.browseAlbum(id!, providerOverride),
    enabled: !!id,
  });

  const watchAlbum = useMutation({
    mutationFn: () =>
      api.watchAlbum(id!, {
        artist_provider_id: artistID,
        artist_name: artistName || album?.artist_name || '',
        artist_image_url: artistImageURL,
        provider: providerOverride,
      }),
    onSuccess: (created) => {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast(`Added "${created.title}" to library`, 'success');
      navigate(`/album/${created.id}`);
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  const watchTrack = useMutation({
    mutationFn: (track: BrowseTrackResult) =>
      api.watchTrack(track.id, {
        artist_provider_id: artistID,
        artist_name: artistName || album?.artist_name || '',
        artist_image_url: artistImageURL,
        album_provider_id: id,
        album_title: album!.title,
        album_cover_url: album!.cover_url,
        album_year: album!.year || null,
        title: track.title,
        track_number: track.track_number,
        disc_number: track.disc_number,
        duration_ms: track.duration_ms,
        provider: providerOverride,
      }),
    onSuccess: (_data, track) => {
      setLocalWatchedTracks((prev) => new Set(prev).add(track.id));
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      toast(`Watching "${track.title}"`, 'success');
    },
    onError: (err: Error) => toast(err.message, 'error'),
  });

  if (isLoading) {
    return (
      <div className="space-y-4 animate-pulse">
        <div className="flex items-center gap-4">
          <div className="w-20 h-20 rounded-2xl bg-zinc-800 shrink-0" />
          <div className="flex-1 space-y-2">
            <div className="h-6 bg-zinc-800 rounded w-44" />
            <div className="h-3.5 bg-zinc-800/70 rounded w-32" />
          </div>
        </div>
        <div className="h-11 bg-zinc-800 rounded-xl" />
        <div className="space-y-1">
          {[...Array(8)].map((_, i) => (
            <div key={i} className="h-12 bg-zinc-900/60 rounded-xl" />
          ))}
        </div>
      </div>
    );
  }

  if (!album) {
    return (
      <div className="rounded-2xl bg-zinc-900/50 border border-white/[0.06] py-12 text-center text-zinc-400 text-sm">
        Album not found
      </div>
    );
  }

  const totalDuration = album.tracks?.reduce((sum, t) => sum + t.duration_ms, 0) ?? 0;

  return (
    <div className="space-y-5">
      {/* Album Hero */}
      <div className="rounded-2xl bg-zinc-900/70 border border-white/[0.07] p-4 md:p-5">
        <div className="flex flex-col sm:flex-row sm:items-center gap-4">
          <div className="flex items-center gap-4 flex-1 min-w-0">
            <div className="w-20 h-20 md:w-24 md:h-24 rounded-xl bg-zinc-800 overflow-hidden shrink-0 border border-white/[0.08]">
              {album.cover_url ? (
                <img src={album.cover_url} alt={album.title} className="w-full h-full object-cover" />
              ) : (
                <div className="w-full h-full flex items-center justify-center text-zinc-600">
                  <IconDisc className="w-8 h-8" />
                </div>
              )}
            </div>
            <div className="flex-1 min-w-0">
              <h2 className="text-xl md:text-2xl font-bold tracking-tight text-zinc-100 truncate">
                {album.title}
              </h2>
              <p className="text-xs text-zinc-400 mt-1">
                {album.artist_name || artistName}
                {album.year > 0 && ` · ${album.year}`} · {album.tracks?.length || 0} tracks
                {totalDuration > 0 && ` · ${formatTotalDuration(totalDuration)}`}
              </p>
              {album.metadata && (
                <div className="mt-2">
                  <MetadataPills metadata={album.metadata} />
                </div>
              )}
            </div>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            {album.library_id && (
              <Link
                to={`/album/${album.library_id}`}
                className="px-4 py-2.5 rounded-xl bg-zinc-800 hover:bg-zinc-700 border border-white/[0.08] text-xs font-medium text-zinc-200 transition-colors"
              >
                Open in Library
              </Link>
            )}
            <button
              type="button"
              onClick={() => watchAlbum.mutate()}
              disabled={watchAlbum.isPending || watchAlbum.isSuccess || album.album_watched}
              className={`flex-1 sm:flex-initial px-5 py-2.5 rounded-xl font-semibold text-sm transition-all disabled:opacity-60 ${
                album.album_watched || watchAlbum.isSuccess
                  ? 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30'
                  : 'bg-amber-400 hover:bg-amber-300 text-zinc-950 active:scale-[0.98]'
              }`}
            >
              {album.album_watched || watchAlbum.isSuccess
                ? '✓ Watching Album'
                : watchAlbum.isPending
                  ? 'Adding…'
                  : 'Watch Entire Album'}
            </button>
          </div>
        </div>
      </div>

      {/* Tracks */}
      {album.tracks && album.tracks.length > 0 && (
        <div className="space-y-2">
          <p className="text-[11px] font-semibold text-zinc-400 uppercase tracking-wider">
            Tracks ({album.tracks.length})
          </p>
          <div className="rounded-2xl bg-zinc-900/60 border border-white/[0.06] overflow-hidden divide-y divide-white/[0.05]">
            {album.tracks.map((track) => {
              const serverWatched = album.watched_track_ids?.includes(track.id);
              const isWatched = serverWatched || localWatchedTracks.has(track.id);
              const isAdding =
                watchTrack.isPending && watchTrack.variables?.id === track.id;

              return (
                <div
                  key={track.id}
                  className="flex items-center gap-3 px-3.5 py-2.5 hover:bg-white/[0.02] transition-colors"
                >
                  <span className="text-xs text-zinc-500 w-6 text-right shrink-0 tabular-nums">
                    {track.track_number}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-zinc-100 truncate">{track.title}</p>
                    <p className="text-[11px] text-zinc-500 tabular-nums">
                      {formatDuration(track.duration_ms)}
                    </p>
                  </div>
                  {isWatched ? (
                    <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg bg-emerald-500/15 text-emerald-300 border border-emerald-500/25 text-xs font-medium shrink-0">
                      <IconCheck className="w-3.5 h-3.5" />
                      Watched
                    </span>
                  ) : (
                    <button
                      type="button"
                      disabled={isAdding}
                      onClick={() => watchTrack.mutate(track)}
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
