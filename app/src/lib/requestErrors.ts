export function isUnauthorizedError(error: unknown) {
  return error instanceof Error && /unauthorized/i.test(error.message);
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
