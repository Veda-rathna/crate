import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useSystemProcess } from '../context/SystemContext';
import ProcessDrawer from './ProcessDrawer';
import {
  IconActivity,
  IconBack,
  IconDownload,
  IconLibrary,
  IconSearch,
  IconSettings,
} from './Icons';

const navItems = [
  { to: '/', label: 'Library', Icon: IconLibrary },
  { to: '/search', label: 'Search', Icon: IconSearch },
  { to: '/downloads', label: 'Downloads', Icon: IconDownload },
  { to: '/settings', label: 'Settings', Icon: IconSettings },
];

function getPageTitle(pathname: string): string {
  if (pathname === '/') return 'Library';
  if (pathname.startsWith('/search')) return 'Discover';
  if (pathname.startsWith('/downloads')) return 'Downloads';
  if (pathname.startsWith('/settings/blocked')) return 'Blocked Sources';
  if (pathname.startsWith('/settings')) return 'Settings';
  if (pathname.startsWith('/browse/artist')) return 'Artist Preview';
  if (pathname.startsWith('/browse/album')) return 'Album Preview';
  if (pathname.startsWith('/artist/')) return 'Artist';
  if (pathname.startsWith('/album/')) return 'Album';
  return 'Crate';
}

export default function Layout() {
  const location = useLocation();
  const navigate = useNavigate();
  const {
    status,
    providers,
    hasRunningWork,
    activeDownloadsCount,
    pendingDownloadsCount,
    failedDownloadsCount,
    attentionCount,
    headline,
    openProcessDrawer,
    activeTasks,
    importStatus,
  } = useSystemProcess();

  const isSubPage =
    location.pathname.startsWith('/browse/') ||
    location.pathname.startsWith('/artist/') ||
    location.pathname.startsWith('/album/') ||
    location.pathname.startsWith('/settings/blocked');

  const queueBadgeCount = activeDownloadsCount + pendingDownloadsCount;
  const pageTitle = getPageTitle(location.pathname);
  const unhealthyCount = providers.filter((p) => !p.healthy).length;

  return (
    <div className="min-h-dvh bg-zinc-950 text-zinc-100 flex flex-col">
      {/* Mobile sticky top app bar */}
      <header className="md:hidden sticky top-0 z-40 bg-zinc-950/85 backdrop-blur-md border-b border-white/[0.06] safe-area-top">
        <div className="h-14 px-4 flex items-center justify-between gap-2">
          <div className="flex items-center gap-2.5 min-w-0">
            {isSubPage ? (
              <button
                type="button"
                onClick={() => (window.history.length > 1 ? navigate(-1) : navigate('/'))}
                className="inline-flex items-center gap-1 -ml-1.5 px-2 py-1.5 rounded-lg text-sm font-medium text-zinc-300 active:bg-zinc-800 transition-colors"
              >
                <IconBack className="w-4 h-4" />
                <span>Back</span>
              </button>
            ) : (
              <div className="flex items-center gap-2.5 min-w-0">
                <img src="/cratelogo.png" alt="Crate" className="w-7 h-7 rounded-md shrink-0" />
                <span className="font-bold text-base tracking-tight truncate">{pageTitle}</span>
              </div>
            )}
          </div>

          <button
            type="button"
            onClick={openProcessDrawer}
            className={`inline-flex items-center gap-2 px-2.5 py-1.5 rounded-full border text-[11px] font-medium max-w-[58%] transition-colors active:scale-95 ${
              hasRunningWork
                ? 'bg-sky-500/15 border-sky-500/30 text-sky-200'
                : attentionCount > 0
                  ? 'bg-amber-500/15 border-amber-500/30 text-amber-200'
                  : 'bg-zinc-900 border-white/[0.08] text-zinc-400'
            }`}
            aria-label="Open system process monitor"
          >
            <span
              className={`w-2 h-2 rounded-full shrink-0 ${
                hasRunningWork
                  ? 'bg-sky-400 animate-pulse'
                  : attentionCount > 0
                    ? 'bg-amber-400'
                    : 'bg-emerald-400/80'
              }`}
            />
            <span className="truncate">{headline}</span>
          </button>
        </div>
      </header>

      {/* Desktop persistent sidebar */}
      <aside className="hidden md:flex fixed left-0 top-0 h-full w-64 bg-zinc-900/60 backdrop-blur-xl border-r border-white/[0.07] flex-col z-40">
        <div className="px-5 pt-6 pb-5 flex items-center gap-3">
          <img src="/cratelogo.png" alt="Crate" className="w-10 h-10 rounded-xl shrink-0 shadow-md" />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="text-lg font-bold tracking-tight leading-none">Crate</h1>
              {status?.version && (
                <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-400 border border-white/[0.06] truncate max-w-[80px]">
                  {status.version}
                </span>
              )}
            </div>
            <p className="text-[11px] text-zinc-500 mt-1 tracking-wider uppercase">Dig deeper</p>
          </div>
        </div>

        <nav className="flex-1 flex flex-col gap-1 px-3">
          {navItems.map(({ to, label, Icon }) => {
            const isDownloads = to === '/downloads';
            const isLibrary = to === '/';
            const isSettings = to === '/settings';

            return (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  `group flex items-center justify-between gap-3 px-3.5 py-2.5 rounded-xl text-sm font-medium transition-all ${
                    isActive
                      ? 'bg-white/[0.08] text-white border border-white/[0.08] shadow-sm'
                      : 'text-zinc-400 hover:text-zinc-100 hover:bg-white/[0.04] border border-transparent'
                  }`
                }
              >
                <span className="flex items-center gap-3">
                  <Icon className="w-[18px] h-[18px] shrink-0" />
                  <span>{label}</span>
                </span>

                {isLibrary && status && (
                  <span className="text-[11px] tabular-nums text-zinc-500">
                    {status.artists_count}
                  </span>
                )}

                {isDownloads && (queueBadgeCount > 0 || failedDownloadsCount > 0) && (
                  <span
                    className={`px-1.5 py-0.5 rounded-md text-[10px] font-semibold tabular-nums ${
                      activeDownloadsCount > 0
                        ? 'bg-sky-500/20 text-sky-300 border border-sky-500/30'
                        : pendingDownloadsCount > 0
                          ? 'bg-amber-500/20 text-amber-300 border border-amber-500/30'
                          : 'bg-rose-500/20 text-rose-300 border border-rose-500/30'
                    }`}
                  >
                    {queueBadgeCount > 0 ? queueBadgeCount : `${failedDownloadsCount}!`}
                  </span>
                )}

                {isSettings && unhealthyCount > 0 && (
                  <span
                    className="w-2 h-2 rounded-full bg-rose-400 animate-pulse"
                    title="Provider offline"
                  />
                )}
              </NavLink>
            );
          })}
        </nav>

        {/* Live System Status Card in Sidebar */}
        <div className="p-3 border-t border-white/[0.06]">
          <button
            type="button"
            onClick={openProcessDrawer}
            className={`w-full text-left rounded-xl p-3 border transition-all ${
              hasRunningWork
                ? 'bg-sky-950/30 border-sky-500/25 hover:border-sky-500/40'
                : attentionCount > 0
                  ? 'bg-amber-950/20 border-amber-500/25 hover:border-amber-500/40'
                  : 'bg-zinc-900/80 border-white/[0.06] hover:border-white/[0.12]'
            }`}
          >
            <div className="flex items-center justify-between gap-2 mb-1.5">
              <span className="inline-flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-zinc-300">
                <span
                  className={`w-2 h-2 rounded-full ${
                    hasRunningWork
                      ? 'bg-sky-400 animate-pulse'
                      : attentionCount > 0
                        ? 'bg-amber-400'
                        : 'bg-emerald-400'
                  }`}
                />
                System Monitor
              </span>
              <IconActivity className="w-3.5 h-3.5 text-zinc-500" />
            </div>

            <p className="text-xs text-zinc-200 font-medium truncate">{headline}</p>

            {(activeTasks.length > 0 || importStatus?.status === 'running') && (
              <div className="mt-2 h-1 bg-zinc-800 rounded-full overflow-hidden">
                <div className="h-full bg-sky-400 rounded-full w-2/3 animate-pulse" />
              </div>
            )}

            {providers.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1 mt-2.5 pt-2 border-t border-white/[0.06]">
                {providers.map((p) => (
                  <span
                    key={p.name}
                    className="inline-flex items-center gap-1 text-[10px] text-zinc-400"
                    title={`${p.display_name}: ${p.healthy ? 'Online' : 'Offline'}`}
                  >
                    <span
                      className={`w-1.5 h-1.5 rounded-full ${
                        p.healthy ? 'bg-emerald-400' : 'bg-rose-400'
                      }`}
                    />
                    {p.display_name}
                  </span>
                ))}
              </div>
            )}
          </button>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 pb-[calc(4.75rem+env(safe-area-inset-bottom,0px))] md:pb-10 md:pl-64">
        <div className="max-w-2xl mx-auto px-4 pt-4 pb-6 md:max-w-6xl md:px-8 md:py-7">
          {isSubPage && (
            <button
              type="button"
              onClick={() => (window.history.length > 1 ? navigate(-1) : navigate('/'))}
              className="hidden md:inline-flex items-center gap-1.5 text-xs font-medium text-zinc-400 hover:text-zinc-100 mb-4 px-2.5 py-1.5 -ml-2 rounded-lg hover:bg-white/[0.05] transition-colors"
            >
              <IconBack className="w-3.5 h-3.5" />
              Back
            </button>
          )}
          <Outlet />
        </div>
      </main>

      {/* Mobile bottom nav */}
      <nav className="md:hidden fixed bottom-0 left-0 right-0 bg-zinc-950/90 backdrop-blur-lg border-t border-white/[0.08] safe-area-bottom z-40">
        <div className="grid grid-cols-4 px-2 py-1.5">
          {navItems.map(({ to, label, Icon }) => {
            const isDownloads = to === '/downloads';
            const isSettings = to === '/settings';

            return (
              <NavLink
                key={to}
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  `relative flex flex-col items-center justify-center gap-0.5 py-1.5 rounded-xl text-[11px] font-medium transition-colors ${
                    isActive
                      ? 'text-amber-300 bg-amber-500/10'
                      : 'text-zinc-500 active:text-zinc-200'
                  }`
                }
              >
                <div className="relative">
                  <Icon className="w-5 h-5" />
                  {isDownloads && (queueBadgeCount > 0 || failedDownloadsCount > 0) && (
                    <span
                      className={`absolute -top-1.5 -right-2.5 min-w-[16px] h-4 px-1 rounded-full text-[9px] font-bold flex items-center justify-center tabular-nums ${
                        activeDownloadsCount > 0
                          ? 'bg-sky-500 text-zinc-950'
                          : pendingDownloadsCount > 0
                            ? 'bg-amber-400 text-zinc-950'
                            : 'bg-rose-500 text-white'
                      }`}
                    >
                      {queueBadgeCount > 0 ? queueBadgeCount : failedDownloadsCount}
                    </span>
                  )}
                  {isSettings && unhealthyCount > 0 && (
                    <span className="absolute -top-0.5 -right-1 w-2 h-2 rounded-full bg-rose-500" />
                  )}
                </div>
                <span>{label}</span>
              </NavLink>
            );
          })}
        </div>
      </nav>

      <ProcessDrawer />
    </div>
  );
}
