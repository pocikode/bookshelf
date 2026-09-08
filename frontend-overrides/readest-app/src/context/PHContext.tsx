'use client';

import type { ReactNode } from 'react';

// Keep the provider boundary for upstream consumers, but do not initialize
// PostHog. This prevents the analytics SDK from sending any network requests.
export const CSPostHogProvider = ({ children }: { children: ReactNode }) => {
  return children;
};
