import { createBrowserRouter, Navigate } from 'react-router-dom';
import { AppLayout } from '../layout/AppLayout';
import { DashboardPage } from '../pages/DashboardPage';
import { ExperimentsPage } from '../pages/ExperimentsPage';
import { ExperimentDetailPage } from '../pages/ExperimentDetailPage';
import { DependenciesPage } from '../pages/DependenciesPage';
import { ReportsPage } from '../pages/ReportsPage';
import { NotFoundPage } from '../pages/NotFoundPage';

export const router = createBrowserRouter([
  {
    path: '/',
    element: <AppLayout />,
    children: [
      {
        index: true,
        element: <DashboardPage />,
      },
      {
        path: 'dashboard',
        element: <Navigate to="/" replace />,
      },
      {
        path: 'experiments',
        element: <ExperimentsPage />,
      },
      {
        path: 'experiments/:id',
        element: <ExperimentDetailPage />,
      },
      {
        path: 'dependencies',
        element: <DependenciesPage />,
      },
      {
        path: 'reports',
        element: <ReportsPage />,
      },
      {
        path: '*',
        element: <NotFoundPage />,
      },
    ],
  },
]);
