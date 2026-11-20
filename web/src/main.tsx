import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Toaster } from 'sonner';
import { onUnauthorized } from './api/client';
import { createAppRouter } from './app/router';
import { TooltipProvider } from './components/ui/tooltip';
import { LocaleProvider } from './i18n';
import { useTheme } from './lib/theme';
import './styles/globals.css';

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 15_000, refetchOnWindowFocus: false } },
});

const router = createAppRouter(queryClient);

onUnauthorized((code) => {
  if (code === 'password_change_required') {
    void router.navigate({ to: '/password' });
    return;
  }
  queryClient.clear();
  void router.navigate({ to: '/login' });
});

function ThemedToaster() {
  const { theme } = useTheme();
  return <Toaster position="bottom-right" theme={theme} richColors closeButton />;
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <LocaleProvider>
        <TooltipProvider>
          <RouterProvider router={router} />
          <ThemedToaster />
        </TooltipProvider>
      </LocaleProvider>
    </QueryClientProvider>
  </StrictMode>,
);
