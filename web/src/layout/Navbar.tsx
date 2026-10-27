import React from 'react';
import { useTheme } from '../context/useTheme';
import { useHealth } from '../api/hooks';
import { Sun, Moon, Activity, ShieldCheck, ShieldAlert, Menu, ExternalLink } from 'lucide-react';

interface NavbarProps {
  onToggleSidebar?: () => void;
}

export const Navbar: React.FC<NavbarProps> = ({ onToggleSidebar }) => {
  const { resolvedTheme, toggleTheme } = useTheme();
  const { data: health, error } = useHealth();

  const isHealthy = health?.status === 'UP' && !error;

  return (
    <header className="sticky top-0 z-30 flex h-16 w-full items-center justify-between border-b border-slate-200 bg-white/90 px-4 backdrop-blur dark:border-slate-800 dark:bg-slate-900/90 sm:px-6">
      <div className="flex items-center gap-3">
        {onToggleSidebar && (
          <button
            onClick={onToggleSidebar}
            className="flex h-9 w-9 items-center justify-center rounded-lg text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100 md:hidden"
            aria-label="Toggle Navigation Sidebar"
          >
            <Menu className="h-5 w-5" />
          </button>
        )}
        <div className="flex items-center gap-2">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-gradient-to-br from-indigo-500 to-purple-600 text-white font-bold shadow-md shadow-indigo-500/20">
            <Activity className="h-5 w-5" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-extrabold tracking-wider text-slate-900 dark:text-white">
                DAMASCUS
              </span>
              <span className="hidden rounded-full bg-indigo-50 px-2 py-0.5 text-xs font-semibold text-indigo-600 dark:bg-indigo-950/60 dark:text-indigo-400 sm:inline-block">
                Chaos Mesh
              </span>
            </div>
            <p className="hidden text-[10px] text-slate-400 dark:text-slate-500 sm:block">
              Dynamic Capacity & Criticality Stress Engine
            </p>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-3">
        {/* Cluster / Control-Plane Health Indicator */}
        <div className="flex items-center gap-2 rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs font-medium dark:border-slate-800 dark:bg-slate-800/60">
          {isHealthy ? (
            <>
              <span className="relative flex h-2 w-2">
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75"></span>
                <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500"></span>
              </span>
              <span className="hidden text-slate-700 dark:text-slate-300 sm:inline">
                Control Plane:
              </span>
              <span className="font-semibold text-emerald-600 dark:text-emerald-400">
                Healthy
              </span>
              <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
            </>
          ) : (
            <>
              <span className="h-2 w-2 rounded-full bg-amber-500"></span>
              <span className="hidden text-slate-700 dark:text-slate-300 sm:inline">
                Control Plane:
              </span>
              <span className="font-semibold text-amber-600 dark:text-amber-400">
                {error ? 'Unreachable' : health?.status || 'Degraded'}
              </span>
              <ShieldAlert className="h-3.5 w-3.5 text-amber-500" />
            </>
          )}
        </div>

        {/* Theme Toggle Button */}
        <button
          onClick={toggleTheme}
          className="flex h-9 w-9 items-center justify-center rounded-lg border border-slate-200 text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900 dark:border-slate-800 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100"
          title={`Switch to ${resolvedTheme === 'dark' ? 'light' : 'dark'} mode`}
          aria-label="Toggle Theme"
        >
          {resolvedTheme === 'dark' ? (
            <Sun className="h-4 w-4 text-amber-400" />
          ) : (
            <Moon className="h-4 w-4 text-slate-600" />
          )}
        </button>

        {/* GitHub / External Link */}
        <a
          href="https://github.com/drumilbhati/damascus"
          target="_blank"
          rel="noreferrer"
          className="hidden h-9 w-9 items-center justify-center rounded-lg border border-slate-200 text-slate-600 transition-colors hover:bg-slate-100 hover:text-slate-900 dark:border-slate-800 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-slate-100 sm:flex"
          aria-label="View on GitHub"
          title="GitHub Repository"
        >
          <ExternalLink className="h-4 w-4" />
        </a>
      </div>
    </header>
  );
};
