export interface ToolDiffSection {
  label: string;
  path: string;
  before: string;
  after: string;
}

type JSONRecord = Record<string, unknown>;

const EDIT_KEYS = ["old_string", "old_str", "search"] as const;
const REPLACEMENT_KEYS = ["new_string", "new_str", "replacement", "replace"] as const;
const WRITE_KEYS = ["file_text", "content", "text"] as const;
const PATH_KEYS = ["file_path", "path"] as const;

export function extractToolDiff(name: string, input: JSONRecord | null): ToolDiffSection[] {
  if (!input) {
    return [];
  }

  const path = pickString(input, PATH_KEYS);
  if (!path || !looksLikeEditTool(name)) {
    return [];
  }

  const multiEditSections = extractMultiEditSections(input, path);
  if (multiEditSections.length > 0) {
    return multiEditSections;
  }

  const before = pickString(input, EDIT_KEYS);
  const after = pickString(input, REPLACEMENT_KEYS);
  if (before !== undefined || after !== undefined) {
    return [
      {
        label: "Edit",
        path,
        before: before || "",
        after: after || "",
      },
    ];
  }

  const content = pickString(input, WRITE_KEYS);
  if (content !== undefined) {
    return [
      {
        label: "Write",
        path,
        before: "",
        after: content,
      },
    ];
  }

  return [];
}

function extractMultiEditSections(parsed: JSONRecord, path: string): ToolDiffSection[] {
  const edits = parsed.edits;
  if (!Array.isArray(edits)) {
    return [];
  }

  return edits.flatMap((edit, index) => {
    if (!isRecord(edit)) {
      return [];
    }

    const before = pickString(edit, EDIT_KEYS);
    const after = pickString(edit, REPLACEMENT_KEYS);
    if (before === undefined && after === undefined) {
      return [];
    }

    return [
      {
        label: `Edit ${index + 1}`,
        path,
        before: before || "",
        after: after || "",
      },
    ];
  });
}

function looksLikeEditTool(name: string) {
  const normalized = name.trim().toLowerCase();
  return (
    normalized.includes("edit") ||
    normalized.includes("write") ||
    normalized.includes("patch") ||
    normalized.includes("replace")
  );
}

function pickString(input: JSONRecord, keys: readonly string[]): string | undefined {
  for (const key of keys) {
    const value = input[key];
    if (typeof value === "string") {
      return value;
    }
  }
  return undefined;
}

function isRecord(value: unknown): value is JSONRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
