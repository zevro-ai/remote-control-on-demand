import {
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type DragEvent,
  type KeyboardEvent,
  type ClipboardEvent,
} from "react";
import type { DraftAttachment } from "../api/types";
import {
  extractImageFilesFromItems,
  filterImageFiles,
  hasFileItems,
} from "../lib/imageAttachments";
import { MAX_REPEAT_COUNT, normalizeRepeatCount, repeatSequentially } from "../lib/repeatAction";
import { toLoopActionErrorMessage } from "../lib/requestErrors";

type ComposerMode = "prompt" | "bash";

interface Props {
  onSendPrompt: (message: string, attachments: DraftAttachment[]) => void | Promise<void>;
  onSendCommand?: (command: string) => void | Promise<void>;
  disabled?: boolean;
  promptPlaceholder?: string;
  commandPlaceholder?: string;
  supportsImages?: boolean;
  supportsBash?: boolean;
}

export function MessageInput({
  onSendPrompt,
  onSendCommand,
  disabled,
  promptPlaceholder = "Send a message...",
  commandPlaceholder = "Run a bash command...",
  supportsImages = false,
  supportsBash = false,
}: Props) {
  const [value, setValue] = useState("");
  const [attachments, setAttachments] = useState<DraftAttachment[]>([]);
  const [mode, setMode] = useState<ComposerMode>("prompt");
  const [repeatCount, setRepeatCount] = useState(1);
  const [loopIteration, setLoopIteration] = useState(0);
  const [loopTotal, setLoopTotal] = useState(1);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isDragActive, setIsDragActive] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const attachmentsRef = useRef<DraftAttachment[]>([]);
  const inFlightAttachmentsRef = useRef<DraftAttachment[]>([]);
  const dragDepthRef = useRef(0);
  const mountedRef = useRef(true);

  useEffect(() => {
    attachmentsRef.current = attachments;
  }, [attachments]);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      attachmentsRef.current.forEach((attachment) => URL.revokeObjectURL(attachment.preview_url));
      inFlightAttachmentsRef.current.forEach((attachment) => URL.revokeObjectURL(attachment.preview_url));
    };
  }, []);

  const clearAttachments = () => {
    attachmentsRef.current.forEach((attachment) => URL.revokeObjectURL(attachment.preview_url));
    attachmentsRef.current = [];
    setAttachments([]);
    resetFileInput();
  };

  const resetFileInput = () => {
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  const hideAttachments = () => {
    attachmentsRef.current = [];
    setAttachments([]);
    resetFileInput();
  };

  const restoreAttachments = (nextAttachments: DraftAttachment[]) => {
    attachmentsRef.current = nextAttachments;
    setAttachments(nextAttachments);
  };

  const revokeAttachments = (nextAttachments: DraftAttachment[]) => {
    nextAttachments.forEach((attachment) => URL.revokeObjectURL(attachment.preview_url));
  };

  const appendFiles = (files: File[]) => {
    const imageFiles = filterImageFiles(files);
    if (imageFiles.length === 0) {
      return;
    }

    setSubmitError(null);
    setAttachments((current) => [
      ...current,
      ...imageFiles.map((file) => ({
        file,
        name: file.name,
        content_type: file.type || "application/octet-stream",
        size: file.size,
        preview_url: URL.createObjectURL(file),
      })),
    ]);
  };

  const handleSubmit = async () => {
    const message = value.trim();
    const sendCommand = onSendCommand;
    if (disabled || isSubmitting) return;
    if (mode === "bash" && (!message || !sendCommand)) return;
    if (mode === "prompt" && !message && attachments.length === 0) return;

    const total = normalizeRepeatCount(repeatCount);
    let activeIteration = 0;
    const submittedMode = mode;
    const submittedMessage = message;
    const submittedAttachments = attachments;
    inFlightAttachmentsRef.current = submittedMode === "prompt" ? submittedAttachments : [];

    setSubmitError(null);
    setIsSubmitting(true);
    setLoopIteration(0);
    setLoopTotal(total);

    try {
      if (submittedMode === "bash") {
        setValue("");
        await repeatSequentially(total, async (iteration) => {
          activeIteration = iteration;
          setLoopIteration(iteration);
          await sendCommand!(submittedMessage);
        });
        return;
      }

      setValue("");
      hideAttachments();
      await repeatSequentially(total, async (iteration) => {
        activeIteration = iteration;
        setLoopIteration(iteration);
        await onSendPrompt(submittedMessage, submittedAttachments);
      });
      revokeAttachments(submittedAttachments);
      inFlightAttachmentsRef.current = [];
    } catch (error) {
      if (submittedMode === "prompt") {
        if (mountedRef.current) {
          setValue(submittedMessage);
          restoreAttachments(submittedAttachments);
        } else {
          revokeAttachments(submittedAttachments);
        }
        inFlightAttachmentsRef.current = [];
      }
      if (mountedRef.current) {
        if (submittedMode === "bash") {
          setValue(submittedMessage);
        }
        setSubmitError(
          toLoopActionErrorMessage(
            error,
            submittedMode === "bash" ? "Run" : "Send",
            activeIteration || 1,
            total
          )
        );
      }
    } finally {
      if (mountedRef.current) {
        setIsSubmitting(false);
        setLoopIteration(0);
      }
    }
  };

  const handleFiles = (event: ChangeEvent<HTMLInputElement>) => {
    setSubmitError(null);
    const files = Array.from(event.target.files || []);
    appendFiles(files);
    event.target.value = "";
  };

  const removeAttachment = (index: number) => {
    setSubmitError(null);
    setAttachments((current) => {
      const next = [...current];
      const [removed] = next.splice(index, 1);
      if (removed) {
        URL.revokeObjectURL(removed.preview_url);
      }
      return next;
    });
  };

  const switchMode = (nextMode: ComposerMode) => {
    if (nextMode === mode) return;
    if (nextMode === "bash") {
      clearAttachments();
    }
    setSubmitError(null);
    setMode(nextMode);
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void handleSubmit();
    }
  };

  const canAcceptImages = supportsImages && !disabled && !isSubmitting;

  const resetDragState = () => {
    dragDepthRef.current = 0;
    setIsDragActive(false);
  };

  useEffect(() => {
    if (!canAcceptImages) {
      resetDragState();
    }
  }, [canAcceptImages]);

  const handleDragEnter = (event: DragEvent<HTMLDivElement>) => {
    if (!canAcceptImages) {
      return;
    }

    if (!hasFileItems(Array.from(event.dataTransfer.items || []))) {
      return;
    }

    event.preventDefault();
    dragDepthRef.current += 1;
    setIsDragActive(true);
  };

  const handleDragOver = (event: DragEvent<HTMLDivElement>) => {
    if (!canAcceptImages) {
      return;
    }

    if (!hasFileItems(Array.from(event.dataTransfer.items || []))) {
      return;
    }

    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
    setIsDragActive(true);
  };

  const handleDragLeave = (event: DragEvent<HTMLDivElement>) => {
    if (!canAcceptImages || !isDragActive) {
      return;
    }

    event.preventDefault();
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
    if (dragDepthRef.current === 0) {
      setIsDragActive(false);
    }
  };

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    if (!canAcceptImages) {
      return;
    }

    event.preventDefault();
    resetDragState();

    const files = filterImageFiles(Array.from(event.dataTransfer.files || []));
    if (files.length === 0) {
      return;
    }

    if (mode === "bash") {
      switchMode("prompt");
    }
    appendFiles(files);
  };

  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    if (!canAcceptImages) {
      return;
    }

    const files = extractImageFilesFromItems(Array.from(event.clipboardData.items || []));
    if (files.length === 0) {
      return;
    }

    event.preventDefault();
    if (mode === "bash") {
      switchMode("prompt");
    }
    appendFiles(files);
  };

  return (
    <div
      className={`message-composer ${isDragActive ? "is-dragging" : ""} ${isSubmitting ? "is-submitting" : ""}`}
      aria-busy={isSubmitting}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {supportsBash && (
        <div className="message-composer__mode-switch" role="tablist" aria-label="Composer mode">
          <button
            type="button"
            className={mode === "prompt" ? "is-active" : ""}
            onClick={() => switchMode("prompt")}
            disabled={disabled || isSubmitting}
          >
            Prompt
          </button>
          <button
            type="button"
            className={mode === "bash" ? "is-active" : ""}
            onClick={() => switchMode("bash")}
            disabled={disabled || isSubmitting}
          >
            Bash
          </button>
        </div>
      )}

      {supportsImages && isDragActive && (
        <div className="message-composer__drop-hint">
          Drop image files here to attach them to the prompt
        </div>
      )}

      {supportsImages && mode === "prompt" && (
        <div className="message-composer__attachments">
          {attachments.map((attachment, index) => (
            <div key={`${attachment.name}-${index}`} className="message-attachment-chip">
              <img src={attachment.preview_url} alt={attachment.name} />
              <div className="message-attachment-chip__meta">
                <strong>{attachment.name}</strong>
                <span>{Math.max(1, Math.round(attachment.size / 1024))} KB</span>
              </div>
              <button type="button" onClick={() => removeAttachment(index)} disabled={disabled || isSubmitting}>
                x
              </button>
            </div>
          ))}
        </div>
      )}

      {submitError && (
        <p className="message-composer__error" role="alert">
          {submitError}
        </p>
      )}

      <textarea
        value={value}
        onChange={(e) => {
          setValue(e.target.value);
          if (submitError) {
            setSubmitError(null);
          }
        }}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        disabled={disabled || isSubmitting}
        placeholder={mode === "bash" ? commandPlaceholder : promptPlaceholder}
        rows={2}
        className={`message-composer__input ${mode === "bash" ? "is-command" : ""}`}
      />
      <label className="message-composer__repeat">
        <span>Loop</span>
        <input
          type="number"
          min={1}
          max={MAX_REPEAT_COUNT}
          step={1}
          value={repeatCount}
          onChange={(event) => setRepeatCount(normalizeRepeatCount(event.target.value))}
          disabled={disabled || isSubmitting}
        />
      </label>
      {supportsImages && mode === "prompt" && (
        <>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            multiple
            onChange={handleFiles}
            className="message-composer__file-input"
          />
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            disabled={disabled || isSubmitting}
            className="message-composer__attach"
          >
            Add image
          </button>
        </>
      )}
      <button
        onClick={() => void handleSubmit()}
        disabled={
          disabled ||
          isSubmitting ||
          (mode === "bash"
            ? !value.trim()
            : !value.trim() && attachments.length === 0)
        }
        className="message-composer__send"
      >
        {isSubmitting && loopTotal > 1
          ? `${mode === "bash" ? "Running" : "Sending"} ${loopIteration}/${loopTotal}`
          : mode === "bash"
            ? repeatCount > 1
              ? `Run x${repeatCount}`
              : "Run"
            : repeatCount > 1
              ? `Send x${repeatCount}`
              : "Send"}
      </button>
    </div>
  );
}
