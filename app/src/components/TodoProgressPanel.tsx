import type { TodoProgressState } from "../lib/todoProgress";

interface Props {
  progress: TodoProgressState;
}

export function TodoProgressPanel({ progress }: Props) {
  return (
    <section className="todo-progress">
      <div className="todo-progress__header">
        <div>
          <div className="session-window__title">Todo Progress</div>
          <p>{progress.completedCount}/{progress.totalCount} items completed</p>
        </div>
        <div className="todo-progress__summary">
          {progress.completedCount}/{progress.totalCount}
        </div>
      </div>

      <div className="todo-progress__list">
        {progress.items.map((item) => (
          <div
            key={item.text}
            className={`todo-progress__item ${item.completed ? "is-done" : ""} ${item.active ? "is-active" : ""}`}
          >
            <span className="todo-progress__status" aria-hidden="true">
              {item.completed ? "\u2713" : item.active ? "\u23F3" : "\u25CB"}
            </span>
            <span className="todo-progress__text">{item.text}</span>
          </div>
        ))}
      </div>
    </section>
  );
}
