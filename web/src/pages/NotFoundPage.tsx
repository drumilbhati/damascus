import React from 'react';
import { Link } from 'react-router-dom';
import { AlertCircle, Home } from 'lucide-react';

export const NotFoundPage: React.FC = () => {
  return (
    <div className="flex h-96 flex-col items-center justify-center space-y-4 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-400">
        <AlertCircle className="h-8 w-8" />
      </div>
      <div>
        <h1 className="text-xl font-bold text-slate-900 dark:text-white">Page Not Found</h1>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
          The requested route does not exist in the Damascus console.
        </p>
      </div>
      <Link
        to="/"
        className="inline-flex items-center gap-2 rounded-xl bg-indigo-600 px-4 py-2 text-sm font-semibold text-white shadow-sm transition hover:bg-indigo-500"
      >
        <Home className="h-4 w-4" />
        Return to Dashboard
      </Link>
    </div>
  );
};
