export function isUnauthorizedError(error: unknown) {
  if (!(error instanceof Error)) {
    return false;
  }
  const status = (error as Error & { status?: number }).status;
  return status === 401 || status === 403 || /unauthorized/i.test(error.message);
}

export function hasErrorStatus(error: unknown, status: number) {
  return error instanceof Error && (error as Error & { status?: number }).status === status;
}

export function toRequestErrorMessage(
  error: unknown,
  fallback = "Request failed."
) {
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  return fallback;
}

export function toLoopActionErrorMessage(
  error: unknown,
  actionLabel: string,
  iteration: number,
  total: number
) {
  const base = toRequestErrorMessage(error, `${actionLabel} failed.`);
  if (total > 1) {
    return `${base} Loop stopped at ${iteration}/${total}.`;
  }
  return base;
}
