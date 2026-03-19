const IMAGE_EXTENSION_RE = /\.(avif|bmp|gif|heic|heif|jpe?g|png|svg|webp)$/i;

interface FileLike {
  name: string;
  type: string;
}

interface ClipboardFileLikeItem<T extends FileLike = FileLike> {
  kind: string;
  type: string;
  getAsFile(): T | null;
}

export function hasFileItems<T extends FileLike>(items: Iterable<ClipboardFileLikeItem<T>>) {
  for (const item of items) {
    if (item.kind === "file") {
      return true;
    }
  }

  return false;
}

export function isImageLikeFile(file: FileLike) {
  if (file.type?.startsWith("image/")) {
    return true;
  }

  return IMAGE_EXTENSION_RE.test(file.name || "");
}

export function filterImageFiles<T extends Pick<File, "name" | "type">>(files: T[]) {
  return files.filter(isImageLikeFile);
}

export function extractImageFilesFromItems<T extends FileLike>(
  items: Iterable<ClipboardFileLikeItem<T>>
) {
  const files: T[] = [];

  for (const item of items) {
    if (item.kind !== "file" || !item.type.startsWith("image/")) {
      continue;
    }

    const file = item.getAsFile();
    if (file) {
      files.push(file);
    }
  }

  return files;
}
