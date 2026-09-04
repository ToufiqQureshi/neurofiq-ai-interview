import { useState } from 'react';
import { Outlet, Link, useLocation } from 'react-router-dom';
import { LayoutDashboard, FolderKanban, FileText, Menu, X, FileUser, FilePenLine, Map, LogOut, Target, Briefcase } from 'lucide-react';
import { useAuth } from '../context/AuthContext';

function Avatar({ avatarUrl, name, className }: { avatarUrl?: string; name: string; className?: string }) {
  return avatarUrl ? (
    <img src={avatarUrl} alt={name} className={`${className} rounded-full object-cover`} />
  ) : (
    <div className={`${className} bg-accent text-white rounded-full flex items-center justify-center font-mono font-semibold text-xs`}>
      {name.slice(0, 2).toUpperCase()}
    </div>
  );
}

type NavItem = {
  name: string;
  icon: React.ComponentType<{ className?: string }>;
  href?: string;
  soon?: boolean;
};

const navGroups: { label: string; items: NavItem[] }[] = [
  {
    label: 'Practice',
    items: [
      { name: 'Dashboard', href: '/dashboard', icon: LayoutDashboard },
      { name: 'Repositories', href: '/repositories', icon: FolderKanban },
      { name: 'Reports', href: '/reports', icon: FileText },
    ],
  },
  {
    label: 'Optimize',
    items: [
      { name: 'Job Radar', icon: Target, href: '/radar' },
      { name: 'LinkedIn Optimizer', icon: FileUser, soon: true },
      { name: 'CV Optimizer', icon: FilePenLine, soon: true },
    ],
  },
  {
    label: 'Discover',
    items: [
      { name: 'Find Jobs', icon: Briefcase, href: '/jobs' },
      { name: 'Job Map', icon: Map, href: '/directory' },
    ],
  },
];

export function DashboardLayout() {
  const [isSidebarOpen, setSidebarOpen] = useState(false);
  const location = useLocation();
  const { user, logout } = useAuth();

  const displayName = user?.github_username || 'Candidate';
  const displayEmail = user?.email || 'No email on file';
  const groups = navGroups;

  return (
    <div className="min-h-screen bg-paper flex font-sans text-ink">
      {/* Mobile Sidebar Toggle */}
      <header className="lg:hidden fixed top-0 w-full z-50 bg-surface border-b border-line flex justify-between items-center h-16 px-4">
        <div className="flex items-center gap-2">
          <span className="w-7 h-7 rounded-md bg-ink text-white flex items-center justify-center text-sm font-display font-extrabold flex-shrink-0">N</span>
          <span className="font-display font-extrabold text-lg text-ink tracking-tight">NeuroFIQ</span>
        </div>
        <button
          className="p-2 text-ink-soft rounded-full hover:bg-paper"
          onClick={() => setSidebarOpen(!isSidebarOpen)}
        >
          {isSidebarOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
        </button>
      </header>

      {/* Dimmed backdrop behind the mobile sidebar; tap to close */}
      {isSidebarOpen && (
        <div
          className="fixed inset-0 bg-black/40 z-30 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside className={`fixed inset-y-0 left-0 bg-surface border-r border-line w-64 pt-16 lg:pt-0 transform transition-transform duration-200 ease-in-out z-40 lg:translate-x-0 ${isSidebarOpen ? 'translate-x-0' : '-translate-x-full'} flex flex-col`}>
        {/* Branding already shown in the mobile top bar above — only repeat it here on desktop, where that bar is hidden. */}
        <div className="hidden lg:flex items-center gap-2 px-6 h-16 border-b border-line lg:border-transparent">
          <span className="w-7 h-7 rounded-md bg-ink text-white flex items-center justify-center text-sm font-display font-extrabold flex-shrink-0">N</span>
          <h1 className="font-display font-extrabold text-ink text-lg leading-tight">NeuroFIQ</h1>
        </div>

        <div className="px-4 py-6 flex-1 overflow-y-auto space-y-6">
          {groups.map((group) => (
            <div key={group.label}>
              <div className="mb-2 px-3 text-[10px] font-mono font-semibold text-ink-faint uppercase tracking-wider">
                {group.label}
              </div>
              <nav className="space-y-0.5">
                {group.items.map((item) => {
                  const isActive = !!item.href && location.pathname.startsWith(item.href);
                  if (item.soon) {
                    return (
                      <div
                        key={item.name}
                        title={`${item.name} — coming soon`}
                        className="flex items-center justify-between px-3 py-2 rounded-lg text-sm text-ink-faint cursor-default"
                      >
                        <div className="flex items-center gap-3">
                          <item.icon className="w-4 h-4 text-ink-faint" />
                          {item.name}
                        </div>
                        <span className="text-[9px] font-mono uppercase tracking-wide text-ink-faint bg-soon-soft px-1.5 py-0.5 rounded-full">Soon</span>
                      </div>
                    );
                  }
                  return (
                    <Link
                      key={item.name}
                      to={item.href!}
                      className={`flex items-center justify-between px-3 py-2 rounded-lg transition-colors text-sm ${
                        isActive
                          ? 'bg-ink text-white font-medium'
                          : 'text-ink-soft hover:bg-paper hover:text-ink'
                      }`}
                      onClick={() => setSidebarOpen(false)}
                    >
                      <div className="flex items-center gap-3">
                        <item.icon className={`w-4 h-4 ${isActive ? 'text-accent' : 'text-ink-faint'}`} />
                        {item.name}
                      </div>
                    </Link>
                  );
                })}
              </nav>
            </div>
          ))}
        </div>

        <div className="p-4 border-t border-line space-y-1">
          <div className="flex items-center gap-3 px-3 py-2 rounded-lg">
            <Avatar avatarUrl={user?.avatar_url} name={displayName} className="w-8 h-8 flex-shrink-0" />
            <div className="flex-1 min-w-0">
              <p className="font-semibold text-xs text-ink truncate">{displayName}</p>
              <p className="text-[10px] text-ink-faint truncate">{displayEmail}</p>
            </div>
          </div>
          <button
            onClick={logout}
            className="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm text-ink-soft hover:bg-paper hover:text-ink transition-colors"
          >
            <LogOut className="w-4 h-4 text-ink-faint" />
            Sign out
          </button>
        </div>
      </aside>

      {/* Main Content */}
      <main className="flex-1 lg:pl-64 flex flex-col min-h-screen">
        {/* Top Navbar */}
        <header className="hidden lg:flex items-center justify-between h-16 px-8 bg-surface border-b border-line sticky top-0 z-30">
          {/* A disabled search box sat here, directly above the Job Map's own
              working one. Two search fields where the first is dead reads as
              a broken page, not as a feature on the way — and the same went
              for a Help button that looked clickable and did nothing. Both
              are gone until they do something. */}
          <div className="flex-1" />
          <div className="flex items-center gap-2 ml-4">
             <Link to="/repositories" className="text-sm font-semibold px-4 py-2 rounded-full bg-ink hover:bg-black text-white transition-colors">Start Interview</Link>
          </div>
        </header>

        <div className="flex-1 overflow-x-hidden pt-16 lg:pt-0">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
