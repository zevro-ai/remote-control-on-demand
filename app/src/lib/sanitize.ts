import { AnsiUp } from "ansi_up";
import DOMPurify from "dompurify";

export function ansiToSafeHtml(raw: string): string {
  const ansi = new AnsiUp();
  ansi.use_classes = true;
  const html = ansi.ansi_to_html(raw);
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: ["span"],
    ALLOWED_ATTR: ["class"],
  });
}
