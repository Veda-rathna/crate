import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useCallback,
  type ReactNode,
} from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../api/client';
import type { ActivityLog, BackgroundTask, ImportState, ProviderInfo, SystemStatus } from '../types/index';

interface SystemContextValue {
  status?: SystemStatus;
  providers: ProviderInfo[];
  recentActivity: ActivityLog[];
  activeTasks: BackgroundTask[];
  importStatus?: ImportState;
  hasRunningWork: boolean;
  activeDownloadsCount: number;
  pendingDownloadsCount: number;
  failedDownloadsCount: number;
  attentionCount: number;
  headline: string;
  processDrawerOpen: boolean;
  setProcessDrawerOpen: (open: boolean) => void;
  openProcessDrawer: () => void;
  closeProcessDrawer: () => void;
}

const SystemContext = createContext<SystemContextValue>({
  providers: [],
  recentActivity: [],
  activeTasks: [],
  hasRunningWork: false,
  activeDownloadsCount: 0,
  pendingDownloadsCount: 0,
  failedDownloadsCount: 0,
  attentionCount: 0,
  headline: 'Connecting…',
  processDrawerOpen: false,
  setProcessDrawerOpen: () => {},
  openProcessDrawer: () => {},
  closeProcessDrawer: () => {},
});

export function useSystemProcess() {
  return useContext(SystemContext);
}

export function SystemProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [processDrawerOpen, setProcessDrawerOpen] = useState(false);
  const [fastPoll, setFastPoll] = useState(false);

  const { data: status } = useQuery({
    queryKey: ['status'],
    queryFn: api.getStatus,
    refetchInterval: fastPoll ? 2500 : 12_000,
  });

  const { data: providers = [] } = useQuery({
    queryKey: ['providers'],
    queryFn: api.listProviders,
    refetchInterval: 30_000,
  });

  const activeTasks = useMemo(() => status?.active_tasks ?? [], [status?.active_tasks]);
  const importStatus = status?.import_status;
  const activeDownloadsCount =
    (status?.active_downloads ?? 0) + (status?.organizing_downloads ?? 0);
  const pendingDownloadsCount = status?.pending_downloads ?? 0;
  const failedDownloadsCount = status?.failed_downloads ?? 0;

  const isImporting = importStatus?.status === 'running';
  const hasRunningWork =
    activeDownloadsCount > 0 ||
    pendingDownloadsCount > 0 ||
    activeTasks.length > 0 ||
    isImporting;

  useEffect(() => {
    setFastPoll(hasRunningWork);
  }, [hasRunningWork]);

  // Automatically refresh Library/Artist queries when background syncs,
  // reconciliations, or imports transition from running -> finished.
  const prevTaskCount = useRef(0);
  const prevImportRunning = useRef(false);
  useEffect(() => {
    if (prevTaskCount.current > 0 && activeTasks.length === 0) {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['artist'] });
      queryClient.invalidateQueries({ queryKey: ['activity'] });
    }
    prevTaskCount.current = activeTasks.length;
  }, [activeTasks.length, queryClient]);

  useEffect(() => {
    if (prevImportRunning.current && importStatus?.status === 'done') {
      queryClient.invalidateQueries({ queryKey: ['artists'] });
      queryClient.invalidateQueries({ queryKey: ['activity'] });
    }
    prevImportRunning.current = isImporting;
  }, [isImporting, importStatus?.status, queryClient]);

  const { data: activityResp } = useQuery({
    queryKey: ['activity-preview'],
    queryFn: () => api.listActivity(12, 0),
    refetchInterval: processDrawerOpen || hasRunningWork ? 4000 : 30_000,
  });

  const recentActivity = useMemo(() => activityResp?.items ?? [], [activityResp?.items]);

  const attentionCount =
    failedDownloadsCount +
    (status?.unlinked_artists ?? 0) +
    (status?.orphaned_artists ?? 0) +
    (status?.unhealthy_providers ?? 0);

  const headline = useMemo(() => {
    if (!status) return 'Connecting…';
    if (isImporting) {
      const progress =
        importStatus && importStatus.total > 0
          ? `${importStatus.processed}/${importStatus.total}`
          : `${importStatus?.processed ?? 0}`;
      return `${importStatus?.dry_run ? 'Scanning' : 'Importing'} library (${progress})`;
    }
    if (activeTasks.length > 0) {
      const first = activeTasks[0];
      if (first.total > 0) {
        return `${first.title} (${first.current}/${first.total})`;
      }
      return first.title;
    }
    if ((status.organizing_downloads ?? 0) > 0 && status.active_downloads === 0) {
      return `Organizing ${status.organizing_downloads} track${status.organizing_downloads! > 1 ? 's' : ''}`;
    }
    if (activeDownloadsCount > 0) {
      const parts = [`${activeDownloadsCount} active`];
      if (pendingDownloadsCount > 0) parts.push(`${pendingDownloadsCount} queued`);
      return parts.join(' · ');
    }
    if (pendingDownloadsCount > 0) {
      return `${pendingDownloadsCount} track${pendingDownloadsCount > 1 ? 's' : ''} queued`;
    }
    if (attentionCount > 0) {
      if (failedDownloadsCount > 0) {
        return `${failedDownloadsCount} download${failedDownloadsCount > 1 ? 's' : ''} failed`;
      }
      if ((status.unhealthy_providers ?? 0) > 0) {
        return 'Provider offline';
      }
      if ((status.unlinked_artists ?? 0) > 0) {
        return `${status.unlinked_artists} artist${status.unlinked_artists! > 1 ? 's' : ''} unlinked`;
      }
    }
    return 'All systems idle';
  }, [
    status,
    isImporting,
    importStatus,
    activeTasks,
    activeDownloadsCount,
    pendingDownloadsCount,
    attentionCount,
    failedDownloadsCount,
  ]);

  const openProcessDrawer = useCallback(() => setProcessDrawerOpen(true), []);
  const closeProcessDrawer = useCallback(() => setProcessDrawerOpen(false), []);

  return (
    <SystemContext.Provider
      value={{
        status,
        providers,
        recentActivity,
        activeTasks,
        importStatus,
        hasRunningWork,
        activeDownloadsCount,
        pendingDownloadsCount,
        failedDownloadsCount,
        attentionCount,
        headline,
        processDrawerOpen,
        setProcessDrawerOpen,
        openProcessDrawer,
        closeProcessDrawer,
      }}
    >
      {children}
    </SystemContext.Provider>
  );
}

