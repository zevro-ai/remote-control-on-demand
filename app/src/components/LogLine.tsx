function getLineColor(line: string): string {
  const trimmed = line.trimStart();
  if (trimmed.startsWith("$")) return "text-accent-green";
  if (trimmed.startsWith("#")) return "text-text-muted";
  if (trimmed.startsWith(">")) return "text-accent-cyan";
  return "text-text-secondary";
}

export function LogLine({ line }: { line: string }) {
  return (
    <div className={`font-mono text-xs leading-5 ${getLineColor(line)} break-all`}>
      {line}
    </div>
  );
}
