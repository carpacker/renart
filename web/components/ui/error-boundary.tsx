"use client";

import { Component, type ReactNode } from "react";

type ErrorBoundaryProps = {
  children: ReactNode;
  /** Rendered instead of the children after a render error is caught. */
  fallback?: ReactNode;
  /**
   * When this value changes, the boundary clears a caught error and retries
   * rendering its children. Pass something that changes when the underlying
   * input is edited (e.g. asset content) so the UI recovers on the next valid
   * state instead of staying stuck on the fallback.
   */
  resetKey?: unknown;
  /** Optional hook for logging; the error is otherwise swallowed on purpose. */
  onError?: (error: unknown) => void;
};

type ErrorBoundaryState = { hasError: boolean };

/**
 * Minimal render-error boundary. Ephemeral errors — e.g. a half-typed YAML
 * asset that momentarily parses to invalid metadata — should degrade the
 * affected panel to a fallback, not crash the whole app. Recovers automatically
 * when `resetKey` changes.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  componentDidCatch(error: unknown) {
    this.props.onError?.(error);
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps) {
    if (this.state.hasError && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ hasError: false });
    }
  }

  render() {
    if (this.state.hasError) {
      return this.props.fallback ?? null;
    }
    return this.props.children;
  }
}
