import type { Message } from "../api/types";

export function mergeIncomingMessage(messages: Message[], incoming: Message): Message[] {
  const normalizedIncoming = stripOptimisticFlag(incoming);
  const optimisticIndex = findOptimisticMatchIndex(messages, normalizedIncoming);

  if (optimisticIndex >= 0) {
    return [
      ...messages.slice(0, optimisticIndex),
      normalizedIncoming,
      ...messages.slice(optimisticIndex + 1),
    ];
  }

  return [...messages, normalizedIncoming];
}

export function removeOptimisticMessage(messages: Message[], optimisticId: string): Message[] {
  return messages.filter(
    (message) => !(message.optimistic && message.optimistic_id === optimisticId)
  );
}

function findOptimisticMatchIndex(messages: Message[], incoming: Message): number {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    if (canReplaceOptimisticMessage(messages[index], incoming)) {
      return index;
    }
  }
  return -1;
}

function canReplaceOptimisticMessage(
  current: Message | undefined,
  incoming: Message
): boolean {
  if (!current?.optimistic || current.role !== "user" || incoming.role !== "user") {
    return false;
  }

  return (
    current.kind === incoming.kind &&
    current.content === incoming.content &&
    commandsMatch(current.command, incoming.command) &&
    attachmentsMatch(current.attachments, incoming.attachments)
  );
}

function commandsMatch(current?: Message["command"], incoming?: Message["command"]): boolean {
  if (!current && !incoming) {
    return true;
  }
  if (!current || !incoming) {
    return false;
  }
  return current.command === incoming.command;
}

function attachmentsMatch(
  current: Message["attachments"] = [],
  incoming: Message["attachments"] = []
): boolean {
  if (current.length !== incoming.length) {
    return false;
  }

  return current.every((attachment, index) => {
    const other = incoming[index];
    return (
      attachment.name === other.name &&
      attachment.content_type === other.content_type &&
      attachment.size === other.size
    );
  });
}

function stripOptimisticFlag(message: Message): Message {
  const { optimistic: _optimistic, optimistic_id: _optimisticId, ...rest } = message;
  return rest;
}
