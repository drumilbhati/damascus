import React from 'react';
import { RouterProvider } from 'react-router-dom';
import { SWRConfig } from 'swr';
import { router } from './router';
import { ThemeProvider } from './context/ThemeContext';
import { defaultFetcher } from './api/client';

export const App: React.FC = () => {
  return (
    <ThemeProvider>
      <SWRConfig
        value={{
          fetcher: defaultFetcher,
          revalidateOnFocus: true,
          shouldRetryOnError: true,
          errorRetryCount: 3,
        }}
      >
        <RouterProvider router={router} />
      </SWRConfig>
    </ThemeProvider>
  );
};

export default App;
