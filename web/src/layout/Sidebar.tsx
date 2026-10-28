import React from 'react';
import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  FlaskConical,
  Network,
  FileSpreadsheet,
  X,
} from 'lucide-react';
import { useExperiments } from '../api/hooks';

interface SidebarProps {
  isOpen: boolean;
  onClose: () => void;
}

export const Sidebar: React.FC<SidebarProps> = ({ isOpen, onClose }) => {
  const { data: experiments } = useExperiments();
  const runningCount = experiments?.filter((e) => e.state === 'RUNNING').length || 0;

  const sections = [
    {
      label: 'Overview',
      items: [
        { to: '/', label: 'Dashboard', icon: LayoutDashboard, badge: null },
      ],
    },
    {
      label: 'Chaos Engineering',
      items: [
        { to: '/experiments', label: 'Experiments', icon: FlaskConical, badge: runningCount > 0 ? runningCount : null },
      ],
    },
    {
      label: 'Observability',
      items: [
        { to: '/dependencies', label: 'Dependency Graph', icon: Network, badge: null },
        { to: '/reports', label: 'Analysis Reports', icon: FileSpreadsheet, badge: null },
      ],
    },
  ];

  return (
    <>
      {isOpen && (
        <div
          onClick={onClose}
          className="fixed inset-0 z-40 md:hidden"
          style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}
          aria-hidden="true"
        />
      )}

      <aside
        className={`fixed top-12 bottom-0 left-0 z-40 flex w-[220px] flex-col md:static md:translate-x-0 ${
          isOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
        style={{
          backgroundColor: '#161b22',
          borderRight: '1px solid #30363d',
          transition: 'transform 200ms ease',
        }}
      >
        {/* Mobile close */}
        <div className="flex items-center justify-between px-3 py-2 md:hidden" style={{ borderBottom: '1px solid #30363d' }}>
          <span className="font-mono text-xs" style={{ color: '#6e7681' }}>NAVIGATION</span>
          <button
            onClick={onClose}
            className="flex h-6 w-6 items-center justify-center rounded"
            style={{ color: '#8b949e' }}
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <nav className="flex-1 overflow-y-auto py-4">
          {sections.map((section) => (
            <div key={section.label} className="mb-4">
              <div className="px-3 pb-1">
                <span className="text-[10px] font-semibold uppercase tracking-widest" style={{ color: '#6e7681' }}>
                  {section.label}
                </span>
              </div>
              {section.items.map((item) => {
                const Icon = item.icon;
                return (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.to === '/'}
                    onClick={onClose}
                    className={({ isActive }) =>
                      `flex items-center justify-between py-1.5 pr-3 text-xs transition-colors ${
                        isActive ? 'border-l-2' : 'border-l-2 border-transparent'
                      }`
                    }
                    style={({ isActive }) => ({
                      paddingLeft: '10px',
                      borderLeftColor: isActive ? '#f0883e' : 'transparent',
                      color: isActive ? '#f0883e' : '#8b949e',
                      backgroundColor: isActive ? 'rgba(240,136,62,0.08)' : 'transparent',
                    })}
                  >
                    <div className="flex items-center gap-2.5">
                      <Icon className="h-3.5 w-3.5 shrink-0" />
                      <span className="font-medium">{item.label}</span>
                    </div>
                    {item.badge != null && (
                      <span
                        className="rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold"
                        style={{ backgroundColor: 'rgba(63,185,80,0.15)', color: '#3fb950' }}
                      >
                        {item.badge}
                      </span>
                    )}
                  </NavLink>
                );
              })}
            </div>
          ))}
        </nav>

        {/* Bottom info */}
        <div className="px-3 py-3" style={{ borderTop: '1px solid #30363d' }}>
          <div className="flex items-center justify-between">
            <span className="font-mono text-[10px]" style={{ color: '#6e7681' }}>damascus-engine</span>
            <span
              className="rounded px-1 py-0.5 font-mono text-[10px]"
              style={{ backgroundColor: '#21262d', color: '#6e7681', border: '1px solid #30363d' }}
            >
              v1.0.0
            </span>
          </div>
        </div>
      </aside>
    </>
  );
};
