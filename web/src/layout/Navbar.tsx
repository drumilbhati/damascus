import React from 'react';
import { useTheme } from '../context/useTheme';
import { useHealth } from '../api/hooks';
import { Sun, Moon, Menu } from 'lucide-react';

interface NavbarProps {
  onToggleSidebar?: () => void;
}

export const Navbar: React.FC<NavbarProps> = ({ onToggleSidebar }) => {
  const { resolvedTheme, toggleTheme } = useTheme();
  const { data: health, error } = useHealth();
  const isHealthy = health?.status === 'UP' && !error;

  return (
    <header
      className="sticky top-0 z-30 flex h-12 w-full items-center justify-between px-4"
      style={{ backgroundColor: '#161b22', borderBottom: '1px solid #30363d' }}
    >
      <div className="flex items-center gap-3">
        {onToggleSidebar && (
          <button
            onClick={onToggleSidebar}
            className="flex h-7 w-7 items-center justify-center rounded text-[#8b949e] hover:text-[#e6edf3] md:hidden"
            style={{ backgroundColor: 'transparent' }}
            aria-label="Toggle Navigation"
          >
            <Menu className="h-4 w-4" />
          </button>
        )}
        <div className="flex items-center gap-2">
          {/* Simple text logo - no gradient */}
          <span className="font-mono text-sm font-bold tracking-widest" style={{ color: '#f0883e' }}>
            DAMASCUS
          </span>
          <span
            className="rounded px-1.5 py-0.5 font-mono text-[10px]"
            style={{ backgroundColor: '#21262d', color: '#8b949e', border: '1px solid #30363d' }}
          >
            v1.0
          </span>
        </div>
        <span className="hidden text-xs sm:block" style={{ color: '#6e7681' }}>
          Chaos &amp; Capacity Engine
        </span>
      </div>

      <div className="flex items-center gap-2">
        {/* Health pill */}
        <div
          className="flex items-center gap-1.5 rounded px-2.5 py-1 text-xs"
          style={{ backgroundColor: '#21262d', border: '1px solid #30363d' }}
        >
          <span
            className="h-1.5 w-1.5 rounded-full"
            style={{ backgroundColor: isHealthy ? '#3fb950' : '#f85149' }}
          />
          <span style={{ color: '#8b949e' }}>control-plane</span>
          <span
            className="font-semibold"
            style={{ color: isHealthy ? '#3fb950' : '#f85149' }}
          >
            {isHealthy ? 'UP' : error ? 'DOWN' : health?.status || 'DEGRADED'}
          </span>
        </div>

        {/* GitHub link */}
        <a
          href="https://github.com/drumilbhati/damascus"
          target="_blank"
          rel="noreferrer"
          className="hidden h-7 w-7 items-center justify-center rounded sm:flex"
          style={{ color: '#8b949e', border: '1px solid #30363d' }}
          aria-label="GitHub"
        >
          <svg className="h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
            <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z" />
          </svg>
        </a>

        {/* Theme toggle */}
        <button
          onClick={toggleTheme}
          className="flex h-7 w-7 items-center justify-center rounded"
          style={{ color: '#8b949e', border: '1px solid #30363d' }}
          title={`Switch to ${resolvedTheme === 'dark' ? 'light' : 'dark'} mode`}
          aria-label="Toggle Theme"
        >
          {resolvedTheme === 'dark' ? (
            <Sun className="h-3.5 w-3.5" style={{ color: '#d29922' }} />
          ) : (
            <Moon className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
    </header>
  );
};
