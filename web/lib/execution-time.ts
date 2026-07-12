export type ExecutionTimeWindow = {
  start: string;
  end: string;
};

export type ExecutionTimeOption = ExecutionTimeWindow & {
  value: string;
  label: string;
  isDefault: boolean;
};

export function getExecutionTimeOptions(
  schedule: string | undefined,
  now: Date = new Date(),
  count = 8,
): ExecutionTimeOption[] {
  const boundaries = getRecentScheduleBoundaries(schedule, now, count + 1);
  const options: ExecutionTimeOption[] = [];
  for (let i = boundaries.length - 1; i > 0 && options.length < count; i -= 1) {
    const start = boundaries[i - 1];
    const end = boundaries[i];
    options.push({
      start: start.toISOString(),
      end: end.toISOString(),
      value: start.toISOString(),
      label: formatExecutionTimeLabel(start, end),
      isDefault: options.length === 0,
    });
  }
  return options;
}

export function findExecutionTimeOption(options: ExecutionTimeOption[], value: string | undefined) {
  if (!value) {
    return options[0] ?? null;
  }
  return options.find((option) => option.value === value) ?? options[0] ?? null;
}

function getRecentScheduleBoundaries(
  schedule: string | undefined,
  now: Date,
  count: number,
): Date[] {
  const normalized = normalizeSchedule(schedule);
  if (normalized === "@hourly") {
    const end = floorHour(now);
    return Array.from({ length: count }, (_, index) => addHours(end, index - count + 1));
  }
  if (normalized === "@daily") {
    const end = floorDay(now);
    return Array.from({ length: count }, (_, index) => addDays(end, index - count + 1));
  }

  const parsed = parseStandardCron(normalized);
  if (!parsed) {
    return getRecentScheduleBoundaries("@daily", now, count);
  }

  const boundaries: Date[] = [];
  const cursor = new Date(now.getTime());
  cursor.setUTCSeconds(0, 0);
  const earliest = addDays(now, -366);
  for (let i = 0; i < 366 * 24 * 60 && cursor >= earliest && boundaries.length < count; i += 1) {
    if (cursor <= now && cronMatches(parsed, cursor)) {
      boundaries.unshift(new Date(cursor.getTime()));
    }
    cursor.setUTCMinutes(cursor.getUTCMinutes() - 1);
  }

  return boundaries.length >= count
    ? boundaries
    : getRecentScheduleBoundaries("@daily", now, count);
}

function normalizeSchedule(schedule: string | undefined) {
  const trimmed = (schedule ?? "").trim().toLowerCase();
  if (!trimmed || trimmed === "daily") {
    return "@daily";
  }
  if (trimmed === "hourly") {
    return "@hourly";
  }
  return trimmed;
}

function parseStandardCron(schedule: string) {
  const parts = schedule.trim().split(/\s+/);
  if (parts.length !== 5) {
    return null;
  }
  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;
  return {
    minute: parseCronField(minute, 0, 59),
    hour: parseCronField(hour, 0, 23),
    dayOfMonth: parseCronField(dayOfMonth, 1, 31),
    month: parseCronField(month, 1, 12),
    dayOfWeek: parseCronField(dayOfWeek, 0, 7),
  };
}

function parseCronField(value: string, min: number, max: number) {
  const allowed = new Set<number>();
  for (const part of value.split(",")) {
    const [rangePart, stepPart] = part.split("/");
    const step = stepPart ? Number(stepPart) : 1;
    if (!Number.isInteger(step) || step <= 0) {
      return null;
    }
    const [startRaw, endRaw] =
      rangePart === "*" ? [String(min), String(max)] : rangePart.split("-");
    const start = Number(startRaw);
    const end = Number(endRaw ?? startRaw);
    if (
      !Number.isInteger(start) ||
      !Number.isInteger(end) ||
      start < min ||
      end > max ||
      start > end
    ) {
      return null;
    }
    for (let current = start; current <= end; current += step) {
      allowed.add(current);
    }
  }
  return allowed;
}

function cronMatches(parsed: NonNullable<ReturnType<typeof parseStandardCron>>, date: Date) {
  if (!parsed.minute || !parsed.hour || !parsed.dayOfMonth || !parsed.month || !parsed.dayOfWeek) {
    return false;
  }
  const weekday = date.getUTCDay();
  const weekdayMatches =
    parsed.dayOfWeek.has(weekday) || (weekday === 0 && parsed.dayOfWeek.has(7));
  return (
    parsed.minute.has(date.getUTCMinutes()) &&
    parsed.hour.has(date.getUTCHours()) &&
    parsed.dayOfMonth.has(date.getUTCDate()) &&
    parsed.month.has(date.getUTCMonth() + 1) &&
    weekdayMatches
  );
}

function formatExecutionTimeLabel(start: Date, end: Date) {
  const dateFormatter = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
  const timeFormatter = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
  if (
    start.getUTCHours() === 0 &&
    start.getUTCMinutes() === 0 &&
    end.getUTCHours() === 0 &&
    end.getUTCMinutes() === 0
  ) {
    return dateFormatter.format(start);
  }
  return `${dateFormatter.format(start)} ${timeFormatter.format(start)}-${timeFormatter.format(end)}`;
}

function floorHour(date: Date) {
  return new Date(
    Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate(), date.getUTCHours()),
  );
}

function floorDay(date: Date) {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
}

function addHours(date: Date, hours: number) {
  return new Date(date.getTime() + hours * 60 * 60 * 1000);
}

function addDays(date: Date, days: number) {
  return new Date(date.getTime() + days * 24 * 60 * 60 * 1000);
}
