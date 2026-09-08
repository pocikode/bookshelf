import { act, renderHook } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import { useLibraryPageHistoryReset } from './useLibraryPageHistoryReset';

describe('useLibraryPageHistoryReset', () => {
  test('resets transient state when the library is restored from browser history', () => {
    const onRestore = vi.fn();
    renderHook(() => useLibraryPageHistoryReset(onRestore));

    act(() => {
      window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }));
    });

    expect(onRestore).toHaveBeenCalledOnce();
  });

  test('does not reset state during a normal page show', () => {
    const onRestore = vi.fn();
    renderHook(() => useLibraryPageHistoryReset(onRestore));

    act(() => {
      window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: false }));
    });

    expect(onRestore).not.toHaveBeenCalled();
  });
});
