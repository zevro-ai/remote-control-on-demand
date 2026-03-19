export const MAX_REPEAT_COUNT = 25;

export function normalizeRepeatCount(value: number | string | undefined | null): number {
  const parsed =
    typeof value === "number"
      ? value
      : typeof value === "string"
        ? Number.parseInt(value, 10)
        : Number.NaN;

  if (!Number.isFinite(parsed) || parsed < 1) {
    return 1;
  }

  return Math.min(MAX_REPEAT_COUNT, Math.floor(parsed));
}

export async function repeatSequentially(
  count: number | string | undefined,
  action: (iteration: number, total: number) => Promise<void> | void
) {
  const total = normalizeRepeatCount(count);

  for (let iteration = 1; iteration <= total; iteration += 1) {
    await action(iteration, total);
  }
}
